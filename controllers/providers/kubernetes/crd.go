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
	"fmt"
	"strings"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const (
	CRDStrategyName          = "crd"
	OwnershipAnnotationKey   = "app.kubernetes.io/managed-by"
	ScopeAnnotationKey       = "instancemgr.keikoproj.io/upgrade-scope"
	OwnershipAnnotationValue = "instance-manager"
)

func ProcessCRDStrategy(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup) (bool, error) {

	var (
		status   = instanceGroup.GetStatus()
		strategy = instanceGroup.GetUpgradeStrategy().GetCRDType()
		asgName  = status.GetActiveScalingGroupName()
		lcName   = status.GetActiveLaunchConfigurationName()
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

	launchID := common.GetLastElementBy(lcName, "-")
	NormalizeName(customResource, launchID)
	crdFullName := strings.Join([]string{GVR.Resource, GVR.Group}, ".")
	if !CRDExists(kube, crdFullName) {
		return false, errors.Errorf("custom resource definition '%v' is missing, could not upgrade", crdFullName)
	}
	status.SetStrategyResourceName(customResource.GetName())

	activeResources, err := GetActiveResources(kube, instanceGroup, customResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to discover active custom resources")
	}

	if len(activeResources) > 0 && strings.EqualFold(strategy.GetConcurrencyPolicy(), v1alpha1.ForbidConcurrencyPolicy) {
		log.Info("custom resource/s still active, will requeue", "instancegroup", instanceGroup.GetName())
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
		return false, nil
	}

	var isRunning bool
	if len(activeResources) > 0 && strings.EqualFold(strategy.GetConcurrencyPolicy(), v1alpha1.ReplaceConcurrencyPolicy) {
		for _, resource := range activeResources {
			if strings.HasSuffix(resource.GetName(), launchID) {
				isRunning = true
				continue
			}
			log.Info("active custom resource/s exists, will replace", "instancegroup", instanceGroup.GetName())
			err = kube.Resource(GVR).Namespace(resource.GetNamespace()).Delete(resource.GetName(), &metav1.DeleteOptions{})
			if err != nil {
				return false, errors.Wrap(err, "failed to delete custom resource")
			}
		}
		if isRunning {
			return false, nil
		}
	}

	err = SubmitCustomResource(kube, customResource, strategy.GetCRDName())
	if err != nil {
		return false, errors.Wrap(err, "failed to submit custom resource")
	}
	log.Info("submitted custom resource", "instancegroup", instanceGroup.GetName())

	customResource, err = kube.Resource(GVR).Namespace(customResource.GetNamespace()).Get(customResource.GetName(), metav1.GetOptions{})
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

func GetActiveResources(kube dynamic.Interface, instanceGroup *v1alpha1.InstanceGroup, resource *unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	var (
		status          = instanceGroup.GetStatus()
		strategy        = instanceGroup.GetUpgradeStrategy().GetCRDType()
		statusJSONPath  = strategy.GetStatusJSONPath()
		completedStatus = strategy.GetStatusSuccessString()
		errorStatus     = strategy.GetStatusFailureString()
		activeResources = make([]*unstructured.Unstructured, 0)
		GVR             = GetGVR(resource, strategy.GetCRDName())
	)

	resources, err := kube.Resource(GVR).Namespace(resource.GetNamespace()).List(metav1.ListOptions{})
	if err != nil {
		return activeResources, err
	}

	for _, r := range resources.Items {

		if !HasAnnotation(&r, OwnershipAnnotationKey, OwnershipAnnotationValue) || !HasAnnotation(&r, ScopeAnnotationKey, status.GetActiveScalingGroupName()) {
			// skip resources not owned by controller
			continue
		}

		val, err := GetUnstructuredPath(&r, statusJSONPath)
		if err != nil {
			return activeResources, err
		}

		if val != completedStatus && val != errorStatus {
			// if resource is not completed and not failed, it must be still active
			activeResources = append(activeResources, &r)
		}
	}

	return activeResources, nil
}

func SubmitCustomResource(kube dynamic.Interface, customResource *unstructured.Unstructured, CRDName string) error {
	var (
		customResourceNamespace = customResource.GetNamespace()
		GVR                     = GetGVR(customResource, CRDName)
	)

	_, err := kube.Resource(GVR).Namespace(customResourceNamespace).Create(customResource, metav1.CreateOptions{})
	if !kerr.IsAlreadyExists(err) {
		return err
	}
	return nil
}
