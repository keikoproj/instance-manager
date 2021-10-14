/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	CRDStrategyName          = "crd"
	DefaultUpgradeNamespace  = "default"
	OwnershipAnnotationKey   = "app.kubernetes.io/managed-by"
	ScopeAnnotationKey       = "instancemgr.keikoproj.io/upgrade-scope"
	OwnershipAnnotationValue = "instance-manager"
)

func ProcessCRDStrategy(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup, configName string) (bool, error) {

	var (
		status                      = instanceGroup.GetStatus()
		strategy                    = instanceGroup.GetUpgradeStrategy().GetCRDType()
		spec                        = instanceGroup.GetEKSSpec()
		instanceGroupNamespacedName = instanceGroup.NamespacedName()
		asgName                     = status.GetActiveScalingGroupName()
		statusPath                  = strategy.GetStatusJSONPath()
		successString               = strategy.GetStatusSuccessString()
		failureString               = strategy.GetStatusFailureString()
		crdName                     = strategy.GetCRDName()
		policy                      = strategy.GetConcurrencyPolicy()
	)

	renderParams := struct {
		InstanceGroup *v1alpha1.InstanceGroup
	}{
		InstanceGroup: instanceGroup,
	}

	templatedCustomResource, err := RenderCustomResource(strategy.GetSpec(), renderParams)
	if err != nil {
		return false, errors.Wrap(err, "failed to render custom resource templating")
	}

	customResource, err := ParseCustomResourceYaml(templatedCustomResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to parse custom resource yaml")

	}

	AddAnnotation(customResource, OwnershipAnnotationKey, OwnershipAnnotationValue)
	AddAnnotation(customResource, ScopeAnnotationKey, asgName)
	GVR := GetGVR(customResource, crdName)

	var launchID string
	if spec.IsLaunchConfiguration() {
		launchID = common.GetLastElementBy(configName, "-")
	} else if spec.IsLaunchTemplate() {
		templateID := common.GetLastElementBy(configName, "-")
		version := status.GetLatestTemplateVersion()
		if common.StringEmpty(version) {
			version = "0"
		}
		launchID = strings.Join([]string{templateID, version}, "-")
	}
	NormalizeName(customResource, launchID)

	crdFullName := CRDFullName(GVR.Resource, GVR.Group)
	if !CRDExists(kube, crdFullName) {
		return false, errors.Errorf("custom resource definition '%v' is missing, could not upgrade", crdFullName)
	}

	var (
		customResourceName      = customResource.GetName()
		customResourceNamespace = customResource.GetNamespace()
	)
	status.SetStrategyResourceName(customResourceName)
	status.SetStrategyResourceNamespace(customResourceNamespace)

	_, activeResources, err := GetResources(kube, instanceGroup, customResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to discover active custom resources")
	}

	if len(activeResources) > 0 {

		switch strings.ToLower(policy) {

		case v1alpha1.ForbidConcurrencyPolicy:
			// if any active resource exist for the ASG, it must first complete
			log.Info("custom resource/s still active, will requeue", "instancegroup", instanceGroupNamespacedName)
			return false, nil

		case v1alpha1.ReplaceConcurrencyPolicy:
			var isRunning bool
			for _, resource := range activeResources {
				resourceName := resource.GetName()
				resourceNamespace := resource.GetNamespace()
				// if active resource exist with same launch id, it's not replaceable
				if strings.HasSuffix(resourceName, launchID) {
					isRunning = true
					continue
				}

				// if active resource exist with a different launch id, it is replaceable
				log.Info("active custom resource/s exists, will replace", "instancegroup", instanceGroupNamespacedName)
				err = kube.Resource(GVR).Namespace(resourceNamespace).Delete(context.Background(), resourceName, metav1.DeleteOptions{})
				if err != nil {
					if !kerr.IsNotFound(err) {
						return false, errors.Wrap(err, "failed to delete custom resource")
					}
				}
			}
			if isRunning {
				// finally, if an active resource is still running, requeue until it's done
				return false, nil
			}

		case v1alpha1.AllowConcurrencyPolicy:
			log.Info("concurrency set to allow, will submit new resource", "instancegroup", instanceGroupNamespacedName)
		}

	}

	// create new resource if not exist
	_, err = kube.Resource(GVR).Namespace(customResourceNamespace).Create(context.Background(), customResource, metav1.CreateOptions{})
	if err != nil {
		if !kerr.IsAlreadyExists(err) {
			return false, errors.Wrap(err, "failed to submit custom resource")
		}
	} else {
		log.Info("submitted custom resource", "instancegroup", instanceGroupNamespacedName)
		status.SetStrategyRetryCount(0)
	}

	// get created resource
	customResource, err = kube.Resource(GVR).Namespace(customResourceNamespace).Get(context.Background(), customResourceName, metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		log.Info("custom resource did not propagate, will requeue", "instancegroup", instanceGroupNamespacedName)
		return false, nil
	}

	// check if resource completed / failed
	resourceStatus, err := GetUnstructuredPath(customResource, statusPath)
	if err != nil {
		return false, err
	}

	log.Info("watching custom resource status", "instancegroup", instanceGroupNamespacedName, "resource", customResourceName, "status", resourceStatus)

	if strings.EqualFold(resourceStatus, successString) {
		log.Info("custom resource succeeded", "instancegroup", instanceGroupNamespacedName, "resource", customResourceName, "status", resourceStatus)
		status.SetStrategyRetryCount(0)
		return true, nil
	}

	if strings.EqualFold(resourceStatus, failureString) {
		log.Info("custom resource failed", "instancegroup", instanceGroupNamespacedName, "resource", customResourceName, "status", resourceStatus)
		maxRetries := *strategy.MaxRetries
		if maxRetries > status.GetStrategyRetryCount() {

			if maxRetries == -1 {
				// if maxRetries is set to -1, retry forever
				status.SetStrategyRetryCount(-1)
			} else {
				// otherwise increment retry counter
				status.IncrementStrategyRetryCount()
			}

			log.Info("max retries not met, will resubmit", "instancegroup", instanceGroupNamespacedName, "maxRetries", maxRetries, "retryNumber", status.StrategyRetryCount)
			err = kube.Resource(GVR).Namespace(customResourceNamespace).Delete(context.Background(), customResourceName, metav1.DeleteOptions{})
			if err != nil {
				if !kerr.IsNotFound(err) {
					return false, errors.Wrap(err, "failed to delete custom resource")
				}
			}
			return false, nil
		}

		return false, errors.Errorf("custom resource failed to converge, %v status is %v", statusPath, resourceStatus)
	}

	log.Info("custom resource still converging", "instancegroup", instanceGroupNamespacedName, "resource", customResourceName, "status", resourceStatus)
	return false, nil
}

func NormalizeName(customResource *unstructured.Unstructured, id string) {
	var (
		name              string
		resourceName      = customResource.GetName()
		resourceNamespace = customResource.GetNamespace()
		generatedName     = customResource.GetGenerateName()
	)

	// Add missing id suffix if missing
	if !common.StringEmpty(resourceName) && !strings.HasSuffix(resourceName, id) {
		name = fmt.Sprintf("%v-%v", resourceName, id)
		customResource.SetName(name)
	}

	// If generatedName provided use instead
	if !common.StringEmpty(generatedName) {
		name = fmt.Sprintf("%v-%v", generatedName, id)
		customResource.SetName(name)
	}

	// Shorten long name
	if len(name) > 63 {
		name = fmt.Sprintf("instancemgr-%v", id)
		customResource.SetName(name)
	}

	// Use default namespace if not provided
	if common.StringEmpty(resourceNamespace) {
		customResource.SetNamespace(DefaultUpgradeNamespace)
	}
}

func ResourceGVR(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup) (schema.GroupVersionResource, error) {
	var (
		strategy = instanceGroup.GetUpgradeStrategy().GetCRDType()
	)

	renderParams := struct {
		InstanceGroup *v1alpha1.InstanceGroup
	}{
		InstanceGroup: instanceGroup,
	}

	templatedCustomResource, err := RenderCustomResource(strategy.GetSpec(), renderParams)
	if err != nil {
		return schema.GroupVersionResource{}, errors.Wrap(err, "failed to render custom resource templating")
	}

	customResource, err := ParseCustomResourceYaml(templatedCustomResource)
	if err != nil {
		return schema.GroupVersionResource{}, errors.Wrap(err, "failed to parse custom resource yaml")
	}

	return GetGVR(customResource, strategy.GetCRDName()), nil
}

func IsResourceActive(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup) bool {
	var (
		strategy = instanceGroup.GetUpgradeStrategy().GetCRDType()
		status   = instanceGroup.GetStatus()
	)

	if strategy == nil {
		return false
	}

	var (
		statusJSONPath  = strategy.GetStatusJSONPath()
		completedStatus = strategy.GetStatusSuccessString()
		errorStatus     = strategy.GetStatusFailureString()
	)

	name := status.GetStrategyResourceName()
	namespace := status.GetStrategyResourceNamespace()

	if common.StringEmpty(name) || common.StringEmpty(namespace) {
		return false
	}

	gvr, err := ResourceGVR(kube, instanceGroup)
	if err != nil {
		log.Error(err, "failed to get resource gvr")
		return false
	}

	r, err := kube.Resource(gvr).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if kerr.IsNotFound(err) {
			return false
		}
		log.Error(err, "failed to get upgrade resource")
		return false
	}

	if IsPathValue(*r, statusJSONPath, completedStatus) || IsPathValue(*r, statusJSONPath, errorStatus) {
		return false
	}

	return true
}

func GetResources(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup, resource *unstructured.Unstructured) ([]unstructured.Unstructured, []unstructured.Unstructured, error) {
	var (
		status            = instanceGroup.GetStatus()
		strategy          = instanceGroup.GetUpgradeStrategy().GetCRDType()
		statusJSONPath    = strategy.GetStatusJSONPath()
		completedStatus   = strategy.GetStatusSuccessString()
		errorStatus       = strategy.GetStatusFailureString()
		activeResources   = make([]unstructured.Unstructured, 0)
		inactiveResources = make([]unstructured.Unstructured, 0)
		GVR               = GetGVR(resource, strategy.GetCRDName())
		resourceNamespace = resource.GetNamespace()
	)

	r, err := kube.Resource(GVR).Namespace(resourceNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return inactiveResources, activeResources, err
	}

	for _, ru := range r.Items {

		annotations := ru.GetAnnotations()

		if HasAnnotationWithValue(annotations, OwnershipAnnotationKey, OwnershipAnnotationValue) && HasAnnotationWithValue(annotations, ScopeAnnotationKey, status.GetActiveScalingGroupName()) {
			if IsPathValue(ru, statusJSONPath, completedStatus) || IsPathValue(ru, statusJSONPath, errorStatus) {
				// if resource is not completed and not failed, it must be still active
				inactiveResources = append(inactiveResources, ru)
			} else {
				activeResources = append(activeResources, ru)
			}
		}

	}

	return inactiveResources, activeResources, nil
}
