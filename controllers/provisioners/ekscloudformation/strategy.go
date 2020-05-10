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

package ekscloudformation

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	OwnershipAnnotationKey   = "app.kubernetes.io/managed-by"
	ScopeAnnotationKey       = "instancemgr.keikoproj.io/upgrade-scope"
	OwnershipAnnotationValue = "instance-manager"
	DefaultConcurrencyPolicy = "forbid"
)

func (ctx *EksCfInstanceGroupContext) discoverCreatedResources(s schema.GroupVersionResource, namespace, currentResourceName string) error {
	var (
		instanceGroup         = ctx.GetInstanceGroup()
		spec                  = &instanceGroup.Spec
		strategyConfiguration = spec.AwsUpgradeStrategy.GetCRDType()
		state                 = ctx.GetDiscoveredState()
		statusJSONPath        = strategyConfiguration.GetStatusJSONPath()
		completedStatus       = strategyConfiguration.GetStatusSuccessString()
		errorStatus           = strategyConfiguration.GetStatusFailureString()
		selfGroup             = state.GetSelfGroup()
		scopeAnnotationValue  = selfGroup.GetScalingGroupName()
	)

	resources, err := ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, resource := range resources.Items {
		if hasAnnotation(&resource, OwnershipAnnotationKey, OwnershipAnnotationValue) && hasAnnotation(&resource, ScopeAnnotationKey, scopeAnnotationValue) {
			state.AddOwnedResources(resource)
		}
	}

	for _, resource := range state.GetOwnedResources() {
		if resource.GetName() == currentResourceName {
			break
		}
		val, err := getUnstructuredPath(resource, statusJSONPath)
		if err != nil {
			return err
		}
		if val != completedStatus && val != errorStatus {
			// if resource is not completed and not failed, it must be still active
			log.Infof("found active owned resource in scope: %v", resource.GetName())
			state.AddActiveOwnedResources(resource)
		}
	}
	return nil
}

func (ctx *EksCfInstanceGroupContext) setRollingStrategyConfigurationDefaults() {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)

	if strings.ToLower(instanceGroup.Spec.AwsUpgradeStrategy.Type) != "rollingupdate" {
		return
	}

	strategyConfiguration := instanceGroup.Spec.AwsUpgradeStrategy.RollingUpdateType
	maxBatchSize := strategyConfiguration.GetMaxBatchSize()
	minInService := strategyConfiguration.GetMinInstancesInService()
	minSuccessfulPercent := strategyConfiguration.GetMinSuccessfulInstancesPercent()
	pauseTime := strategyConfiguration.GetPauseTime()

	if maxBatchSize == 0 {
		strategyConfiguration.SetMaxBatchSize(1)
	}
	if minInService == 0 {
		strategyConfiguration.SetMinInstancesInService(1)
	}
	if minSuccessfulPercent == 0 {
		strategyConfiguration.SetMinSuccessfulInstancesPercent(100)
	}
	if pauseTime == "" {
		strategyConfiguration.SetPauseTime("PT5M")
	}
	ctx.reloadCloudformationConfiguration()
}

func (ctx *EksCfInstanceGroupContext) processCRDStrategy() error {

	var (
		customResourceKind           string
		customResourceName           string
		customResourceNamespace      string
		instanceGroup                = ctx.GetInstanceGroup()
		spec                         = &instanceGroup.Spec
		status                       = &instanceGroup.Status
		strategyConfiguration        = spec.AwsUpgradeStrategy.GetCRDType()
		state                        = ctx.GetDiscoveredState()
		statusJSONPath               = strategyConfiguration.GetStatusJSONPath()
		completedStatus              = strategyConfiguration.GetStatusSuccessString()
		errorStatus                  = strategyConfiguration.GetStatusFailureString()
		customResourceSpec           = strategyConfiguration.GetSpec()
		customResourceDefinitionName = strategyConfiguration.GetCRDName()
		concurrencyPolicy            = strategyConfiguration.GetConcurrencyPolicy()
		selfGroup                    = state.GetSelfGroup()
		scopeAnnotationValue         = selfGroup.GetScalingGroupName()
	)

	if concurrencyPolicy == "" {
		concurrencyPolicy = DefaultConcurrencyPolicy
		strategyConfiguration.SetConcurrencyPolicy(concurrencyPolicy)
	}

	if !common.ContainsString([]string{"forbid", "allow"}, strings.ToLower(concurrencyPolicy)) {
		err := fmt.Errorf("invalid concurrencyPolicy provided: %v", concurrencyPolicy)
		return err
	}

	if customResourceSpec == "" {
		err := fmt.Errorf("custom resource spec not provided")
		return err
	}

	if customResourceDefinitionName == "" {
		err := fmt.Errorf("custom resource definition name not provided")
		return err
	}

	templatedCustomResource, err := ctx.renderCustomResource(customResourceSpec)
	if err != nil {
		log.Errorf("failed to render: %v", customResourceSpec)
		return err
	}

	customResource, err := kubeprovider.ParseCustomResourceYaml(templatedCustomResource)
	if err != nil {
		log.Errorf("failed to parse: %v", templatedCustomResource)
		return err
	}

	addAnnotation(customResource, OwnershipAnnotationKey, OwnershipAnnotationValue)
	addAnnotation(customResource, ScopeAnnotationKey, scopeAnnotationValue)

	API := customResource.GetAPIVersion()
	s := strings.Split(API, "/")
	if len(s) != 2 {
		err = fmt.Errorf("malformed apiVersion provided: %v, expecting 'some.api.com/v1'", API)
		return err
	}
	customResourceDefinitionGroup, customResourceDefinitionVersion := s[0], s[1]

	if strings.HasSuffix(customResourceDefinitionName, customResourceDefinitionGroup) {
		s := strings.Split(customResourceDefinitionName, ".")
		customResourceKind = s[0]
	} else {
		customResourceKind = customResourceDefinitionName
	}

	launchConfigName := strings.ToLower(fmt.Sprintf("%v", selfGroup.GetLaunchConfigName()))
	s = strings.Split(launchConfigName, "-")
	launchConfigID := s[len(s)-1]

	if providedName := customResource.GetName(); providedName != "" {
		if strings.HasSuffix(providedName, launchConfigID) {
			customResourceName = providedName
		} else {
			customResourceName = fmt.Sprintf("%v-%v", providedName, launchConfigID)
		}
	} else if generatedName := customResource.GetGenerateName(); generatedName != "" {
		customResourceName = fmt.Sprintf("%v%v", generatedName, launchConfigID)
	}

	if len(customResourceName) > 63 {
		customResourceName = fmt.Sprintf("instancemgr-%v", launchConfigID)
	}

	customResourceNamespace = customResource.GetNamespace()
	if customResourceNamespace == "" {
		customResourceNamespace = "default"
	}

	status.SetStrategyResourceName(customResourceName)

	customResource.SetName(customResourceName)
	customResource.SetNamespace(customResourceNamespace)

	customResourceDefinitionSchema := schema.GroupVersionResource{
		Group:    customResourceDefinitionGroup,
		Version:  customResourceDefinitionVersion,
		Resource: customResourceKind,
	}

	err = ctx.discoverCreatedResources(customResourceDefinitionSchema, customResourceNamespace, customResourceName)
	if err != nil {
		log.Errorf("failed to discover active custom resources: %v", err)
		return err
	}

	customResourceDefinitionFullName := strings.Join([]string{customResourceKind, customResourceDefinitionGroup}, ".")

	if !kubeprovider.CRDExists(ctx.KubernetesClient.KubeDynamic, customResourceDefinitionFullName) {
		err := fmt.Errorf("custom resource definition '%v' is missing, could not upgrade", customResourceDefinitionFullName)
		return err
	}

	if len(state.GetActiveOwnedResources()) != 0 {
		if strings.ToLower(concurrencyPolicy) == "forbid" {
			log.Infoln("custom resource/s still active, will wait for finite-state per concurrencyPolicy = Forbid")
			instanceGroup.SetState(v1alpha1.ReconcileModifying)
			return nil
		}
	}

	log.Infoln("submitting custom resource")
	customResource, err = ctx.submitCustomResource(customResourceDefinitionSchema, customResource)
	if err != nil {
		log.Errorln("failed to submit upgrade CR")
		return err
	}

	log.Infof("waiting for custom resource %v/%v success status", customResourceNamespace, customResourceName)
	log.Infof("custom resource status path: %v", statusJSONPath)

	ok, err := ctx.checkCustomResourceField(statusJSONPath, completedStatus, errorStatus, customResourceDefinitionSchema, customResource)
	if err != nil {
		log.Infof("custom resource waiter failed: %v", err)
		return err
	}
	if ok {
		log.Infof("custom resource %v/%v completed successfully", customResourceNamespace, customResourceName)
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	} else {
		log.Infof("custom resource %v/%v still converging", customResourceNamespace, customResourceName)
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
	}
	return nil
}

func (ctx *EksCfInstanceGroupContext) renderCustomResource(rawTemplate string) (string, error) {
	var renderBuffer bytes.Buffer
	template, err := template.New("CustomResource").Parse(rawTemplate)
	if err != nil {
		return "", err
	}
	err = template.Execute(&renderBuffer, ctx)
	if err != nil {
		return "", err
	}
	return renderBuffer.String(), nil
}

func (ctx *EksCfInstanceGroupContext) submitCustomResource(s schema.GroupVersionResource, customResource *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	customResourceName := customResource.GetName()
	customResourceNamespace := customResource.GetNamespace()
	createdResource, err := ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(customResourceNamespace).Get(customResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		createdResource, err = ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(customResourceNamespace).Create(customResource, metav1.CreateOptions{})
		if err != nil {
			return createdResource, err
		}
		log.Infof("submitted custom resource %v/%v", customResourceNamespace, customResourceName)

		ok := ctx.waitForResourceExist(s, customResourceNamespace, customResourceName)
		if !ok {
			err = fmt.Errorf("failed to create custom resource %v/%v", customResourceNamespace, customResourceName)
			return createdResource, err
		}
	}
	return createdResource, nil
}

func (ctx *EksCfInstanceGroupContext) waitForResourceExist(s schema.GroupVersionResource, namespace string, name string) bool {
	var timeout = 30
	var counter = 0
	for {
		_, err := ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(namespace).Get(name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			time.Sleep(1 * time.Second)
			counter++
		} else {
			return true
		}
		if counter == timeout {
			break
		}
	}
	return false
}

func (ctx *EksCfInstanceGroupContext) checkCustomResourceField(JSONPath string, successString string, errorString string, s schema.GroupVersionResource, customResource *unstructured.Unstructured) (bool, error) {
	customResourceName := customResource.GetName()
	customResourceNamespace := customResource.GetNamespace()
	customResource, err := ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(customResourceNamespace).Get(customResourceName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	value, err := getUnstructuredPath(customResource, JSONPath)
	if err != nil {
		return false, err
	}

	if value == "" {
		log.Warnf("could not find %v in custom resource", JSONPath)
		return false, nil
	}

	switch strings.ToLower(value) {
	case strings.ToLower(successString):
		return true, nil
	case strings.ToLower(errorString):
		err := fmt.Errorf("custom resource failed to converge, %v status is %v", JSONPath, value)
		return false, err
	default:
		return false, nil
	}
}
