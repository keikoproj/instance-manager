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
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
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
	DefaultConcurrencyPolicy = "forbid"
)

func init() {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

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

	templatedCustomResource, err := common.RenderCustomResource(strategy.GetSpec(), renderParams)
	if err != nil {
		return false, errors.Wrap(err, "failed to render custom resource templating")
	}

	customResource, err := common.ParseCustomResourceYaml(templatedCustomResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to parse custom resource yaml")

	}
	AddAnnotation(customResource, OwnershipAnnotationKey, OwnershipAnnotationValue)
	AddAnnotation(customResource, ScopeAnnotationKey, asgName)
	GVR := GetGVR(customResource, strategy.GetCRDName())

	NormalizeName(customResource, common.GetLastElementBy(lcName, "-"))
	status.SetStrategyResourceName(customResource.GetName())

	activeResources, err := GetActiveResources(kube, instanceGroup, customResource)
	if err != nil {
		return false, errors.Wrap(err, "failed to discover active custom resources")
	}

	crdFullName := strings.Join([]string{GVR.Resource, GVR.Group}, ".")

	if !common.CRDExists(kube, crdFullName) {
		return false, errors.Errorf("custom resource definition '%v' is missing, could not upgrade", crdFullName)
	}

	if len(activeResources) > 0 && strings.ToLower(strategy.GetConcurrencyPolicy()) == "forbid" {
		log.Infoln("custom resource/s still active, will requeue")
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
		return false, nil
	}

	log.Infoln("submitting custom resource")
	err = SubmitCustomResource(kube, customResource, strategy.GetCRDName())
	if err != nil {
		return false, errors.Wrap(err, "failed to submit custom resource")
	}

	customResource, err = kube.Resource(GVR).Namespace(customResource.GetNamespace()).Get(customResource.GetName(), metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		log.Infoln("custom resource did not propagate yet")
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
		return false, nil
	}

	log.Infof("waiting for custom resource %v/%v success status", customResource.GetNamespace(), customResource.GetName())
	log.Infof("custom resource status path: %v", strategy.GetStatusJSONPath())

	resourceStatus, err := GetUnstructuredPath(customResource, strategy.GetStatusJSONPath())
	if err != nil {
		return false, err
	}

	log.Infof("resource status: %v", resourceStatus)
	switch strings.ToLower(resourceStatus) {
	case strings.ToLower(strategy.GetStatusSuccessString()):
		log.Infof("custom resource %v/%v completed successfully", customResource.GetNamespace(), customResource.GetName())
		return true, nil
	case strings.ToLower(strategy.GetStatusFailureString()):
		return false, errors.Errorf("custom resource failed to converge, %v status is %v", strategy.GetStatusJSONPath(), resourceStatus)
	default:
		log.Infof("custom resource %v/%v still converging", customResource.GetNamespace(), customResource.GetName())
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
			log.Infof("found active owned resource in scope: %v", resource.GetName())
			activeResources = append(activeResources, &r)
		}
	}

	return activeResources, nil
}

func SubmitCustomResource(kube dynamic.Interface, customResource *unstructured.Unstructured, CRDName string) error {
	var (
		customResourceName      = customResource.GetName()
		customResourceNamespace = customResource.GetNamespace()
		GVR                     = GetGVR(customResource, CRDName)
	)

	_, err := kube.Resource(GVR).Namespace(customResourceNamespace).Create(customResource, metav1.CreateOptions{})
	if !kerr.IsAlreadyExists(err) {
		return err
	}

	log.Infof("submitted custom resource %v/%v (%v)", customResourceNamespace, customResourceName, GVR)
	return nil
}
