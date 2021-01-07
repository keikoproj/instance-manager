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
	OwnershipAnnotationKey   = "app.kubernetes.io/managed-by"
	ScopeAnnotationKey       = "instancemgr.keikoproj.io/upgrade-scope"
	OwnershipAnnotationValue = "instance-manager"
)

func ProcessCRDStrategy(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup, configName string) (bool, error) {

	var (
		status   = instanceGroup.GetStatus()
		strategy = instanceGroup.GetUpgradeStrategy().GetCRDType()
		spec     = instanceGroup.GetEKSSpec()
		asgName  = status.GetActiveScalingGroupName()
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
	GVR := GetGVR(customResource, strategy.GetCRDName())

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
	crdFullName := strings.Join([]string{GVR.Resource, GVR.Group}, ".")
	if !CRDExists(kube, crdFullName) {
		return false, errors.Errorf("custom resource definition '%v' is missing, could not upgrade", crdFullName)
	}
	status.SetStrategyResourceName(customResource.GetName())
	status.SetStrategyResourceNamespace(customResource.GetNamespace())

	policy := strategy.GetConcurrencyPolicy()

	inactiveResources, activeResources, err := GetResources(kube, instanceGroup, customResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to discover active custom resources")
	}

	if len(activeResources) > 0 {

		switch {
		case strings.EqualFold(policy, v1alpha1.ForbidConcurrencyPolicy):
			log.Info("custom resource/s still active, will requeue", "instancegroup", instanceGroup.GetName())
			instanceGroup.SetState(v1alpha1.ReconcileModifying)
			return false, nil
		case strings.EqualFold(policy, v1alpha1.ReplaceConcurrencyPolicy):
			var isRunning bool
			for _, resource := range activeResources {
				if strings.HasSuffix(resource.GetName(), launchID) {
					isRunning = true
					continue
				}
				log.Info("active custom resource/s exists, will replace", "instancegroup", instanceGroup.GetName())
				err = kube.Resource(GVR).Namespace(resource.GetNamespace()).Delete(context.Background(), resource.GetName(), metav1.DeleteOptions{})
				if err != nil {
					if !kerr.IsNotFound(err) {
						return false, errors.Wrap(err, "failed to delete custom resource")
					}
				}
			}
			if isRunning {
				return false, nil
			}
		case strings.EqualFold(policy, v1alpha1.AllowConcurrencyPolicy):
			log.Info("concurrency set to allow, will submit new resource", "instancegroup", instanceGroup.GetName())
		}

	}

	// delete inactive resources if there is a name conflict
	for _, resource := range inactiveResources {
		if strings.EqualFold(resource.GetName(), customResource.GetName()) && strings.EqualFold(resource.GetNamespace(), customResource.GetNamespace()) {
			log.Info("name conflict with inactive resource, will delete", "instancegroup", instanceGroup.GetName(), "resource", resource.GetName())
			err = kube.Resource(GVR).Namespace(resource.GetNamespace()).Delete(context.Background(), resource.GetName(), metav1.DeleteOptions{})
			if err != nil {
				if !kerr.IsNotFound(err) {
					return false, errors.Wrap(err, "failed to delete custom resource")
				}
			}
		}
	}

	err = SubmitCustomResource(kube, customResource, strategy.GetCRDName())
	if err != nil {
		return false, errors.Wrap(err, "failed to submit custom resource")
	}
	log.Info("submitted custom resource", "instancegroup", instanceGroup.GetName())

	customResource, err = kube.Resource(GVR).Namespace(customResource.GetNamespace()).Get(context.Background(), customResource.GetName(), metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		log.Info("custom resource did not propagate, will requeue", "instancegroup", instanceGroup.GetName())
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
		return false, nil
	}

	resourceStatus, err := GetUnstructuredPath(customResource, strategy.GetStatusJSONPath())
	if err != nil {
		return false, err
	}

	log.Info("watching custom resource status",
		"instancegroup", instanceGroup.GetName(),
		"resource", customResource.GetName(),
		"statuspath", strategy.GetStatusJSONPath(),
		"status", resourceStatus,
	)
	switch strings.ToLower(resourceStatus) {
	case strings.ToLower(strategy.GetStatusSuccessString()):
		log.Info("custom resource succeeded",
			"instancegroup", instanceGroup.GetName(),
			"resource", customResource.GetName(),
			"statuspath", strategy.GetStatusJSONPath(),
			"status", resourceStatus,
		)
		return true, nil
	case strings.ToLower(strategy.GetStatusFailureString()):
		return false, errors.Errorf("custom resource failed to converge, %v status is %v", strategy.GetStatusJSONPath(), resourceStatus)
	default:
		log.Info("custom resource still converging",
			"instancegroup", instanceGroup.GetName(),
			"resource", customResource.GetName(),
			"statuspath", strategy.GetStatusJSONPath(),
			"status", resourceStatus,
		)
		return false, nil
	}
}

func NormalizeName(customResource *unstructured.Unstructured, id string) {
	if providedName := customResource.GetName(); providedName != "" {
		if !strings.HasSuffix(providedName, id) {
			customResource.SetName(fmt.Sprintf("%v-%v", providedName, id))
		}
	}

	if generatedName := customResource.GetGenerateName(); generatedName != "" {
		customResource.SetName(fmt.Sprintf("%v-%v", generatedName, id))
	}

	if len(customResource.GetName()) > 63 {
		customResource.SetName(fmt.Sprintf("instancemgr-%v", id))
	}

	if customResource.GetNamespace() == "" {
		customResource.SetNamespace("default")
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

func GetResources(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup, resource *unstructured.Unstructured) ([]*unstructured.Unstructured, []*unstructured.Unstructured, error) {
	var (
		status            = instanceGroup.GetStatus()
		strategy          = instanceGroup.GetUpgradeStrategy().GetCRDType()
		statusJSONPath    = strategy.GetStatusJSONPath()
		completedStatus   = strategy.GetStatusSuccessString()
		errorStatus       = strategy.GetStatusFailureString()
		activeResources   = make([]*unstructured.Unstructured, 0)
		inactiveResources = make([]*unstructured.Unstructured, 0)
		GVR               = GetGVR(resource, strategy.GetCRDName())
	)

	r, err := kube.Resource(GVR).Namespace(resource.GetNamespace()).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return inactiveResources, activeResources, err
	}

	for _, r := range r.Items {

		if !HasAnnotation(&r, OwnershipAnnotationKey, OwnershipAnnotationValue) || !HasAnnotation(&r, ScopeAnnotationKey, status.GetActiveScalingGroupName()) {
			// skip resources not owned by controller
			continue
		}

		if IsPathValue(r, statusJSONPath, completedStatus) || IsPathValue(r, statusJSONPath, errorStatus) {
			// if resource is not completed and not failed, it must be still active
			inactiveResources = append(inactiveResources, &r)
		} else {
			activeResources = append(activeResources, &r)
		}

	}

	return inactiveResources, activeResources, nil
}

func SubmitCustomResource(kube dynamic.Interface, customResource *unstructured.Unstructured, CRDName string) error {
	var (
		customResourceNamespace = customResource.GetNamespace()
		GVR                     = GetGVR(customResource, CRDName)
	)

	_, err := kube.Resource(GVR).Namespace(customResourceNamespace).Create(context.Background(), customResource, metav1.CreateOptions{})
	if !kerr.IsAlreadyExists(err) {
		return err
	}
	return nil
}
