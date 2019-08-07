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

	"github.com/orkaproj/instance-manager/api/v1alpha1"
	"github.com/orkaproj/instance-manager/controllers/common"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (ctx *EksCfInstanceGroupContext) processCRDStrategy() error {
	var customResourceKind string
	var customResourceName string
	var customResourceNamespace string
	var instanceGroup = ctx.GetInstanceGroup()
	var spec = &instanceGroup.Spec
	var status = &instanceGroup.Status
	var strategyConfiguration = spec.AwsUpgradeStrategy.GetCRDType()
	var state = ctx.GetDiscoveredState()
	var statusJSONPath = strategyConfiguration.GetStatusJSONPath()
	var completedStatus = strategyConfiguration.GetStatusSuccessString()
	var errorStatus = strategyConfiguration.GetStatusFailureString()
	var customResourceSpec = strategyConfiguration.GetSpec()
	var customResourceDefinitionName = strategyConfiguration.GetCRDName()
	var selfGroup = state.GetSelfGroup()

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

	customResource, err := common.ParseCustomResourceYaml(templatedCustomResource)
	if err != nil {
		log.Errorf("failed to parse: %v", templatedCustomResource)
		return err
	}

	API := customResource.GetAPIVersion()
	s := strings.Split(API, "/")
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

	customResourceDefinitionFullName := strings.Join([]string{customResourceKind, customResourceDefinitionGroup}, ".")

	if !common.CRDExists(ctx.KubernetesClient.KubeDynamic, customResourceDefinitionFullName) {
		err := fmt.Errorf("custom resource definition '%v' is missing, could not upgrade", customResourceDefinitionFullName)
		return err
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

	splitFunction := func(c rune) bool {
		return c == '.'
	}
	statusPath := strings.FieldsFunc(JSONPath, splitFunction)

	value, ok, err := unstructured.NestedString(customResource.UnstructuredContent(), statusPath...)
	if ok {
		switch strings.ToLower(value) {
		case strings.ToLower(successString):
			return true, nil
		case strings.ToLower(errorString):
			err := fmt.Errorf("custom resource failed to converge, %v status is %v", JSONPath, value)
			return false, err
		default:
			return false, nil
		}
	} else {
		log.Warnf("could not find %v in custom resource", JSONPath)
		return false, nil
	}
}
