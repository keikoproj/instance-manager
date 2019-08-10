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

package testutil

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/orkaproj/instance-manager/controllers/common"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

type TemplateArguments struct {
	ClusterName        *string
	KeyPairName        *string
	VpcID              *string
	AmiID              *string
	NodeSecurityGroups []string
	Subnets            []string
}

var instanceGroupSchema = schema.GroupVersionResource{
	Group:    "instancemgr.orkaproj.io",
	Version:  "v1alpha1",
	Resource: "instancegroups",
}

func CreateUpdateInstanceGroup(kubeClient dynamic.Interface, relativePath string, args TemplateArguments) (*unstructured.Unstructured, error) {
	ig, err := parseInstanceGroupYaml(relativePath, args)
	if err != nil {
		return ig, err
	}

	name := ig.GetName()
	namespace := ig.GetNamespace()

	igObject, err := kubeClient.Resource(instanceGroupSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
	if err == nil {
		resourceVersion := igObject.GetResourceVersion()
		ig.SetResourceVersion(resourceVersion)
		ig, err = kubeClient.Resource(instanceGroupSchema).Namespace(namespace).Update(ig, metav1.UpdateOptions{})
		if err != nil {
			return ig, err
		}

	} else {
		_, err = kubeClient.Resource(instanceGroupSchema).Namespace(namespace).Create(ig, metav1.CreateOptions{})
		if err != nil {
			return ig, err
		}
	}
	return ig, nil
}

func DeleteInstanceGroup(kubeClient dynamic.Interface, relativePath string, args TemplateArguments) (*unstructured.Unstructured, error) {
	ig, err := parseInstanceGroupYaml(relativePath, args)
	if err != nil {
		return ig, err
	}
	name := ig.GetName()
	namespace := ig.GetNamespace()

	if err := kubeClient.Resource(instanceGroupSchema).Namespace(namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
		return ig, err
	}

	return ig, nil
}

func parseInstanceGroupYaml(relativePath string, args TemplateArguments) (*unstructured.Unstructured, error) {
	var renderBuffer bytes.Buffer
	var err error

	var ig *unstructured.Unstructured

	if _, err = PathToOSFile(relativePath); err != nil {
		return nil, err
	}

	fileData, err := common.ReadFile(relativePath)
	if err != nil {
		return nil, err
	}

	rawTemplate := string(fileData)

	template, err := template.New("InstanceGroup").Parse(rawTemplate)
	if err != nil {
		return nil, err
	}

	err = template.Execute(&renderBuffer, args)
	if err != nil {
		return nil, err
	}

	decoder := yaml.NewYAMLOrJSONDecoder(&renderBuffer, 100)
	for {
		var out unstructured.Unstructured
		err = decoder.Decode(&out)
		if err != nil {
			// this would indicate it's malformed YAML.
			break
		}

		if out.GetKind() == "InstanceGroup" {
			var marshaled []byte
			marshaled, err = out.MarshalJSON()
			json.Unmarshal(marshaled, &ig)
			break
		}
	}

	if err != io.EOF && err != nil {
		return nil, err
	}
	return ig, nil
}

func WaitForInstanceGroupReadiness(k dynamic.Interface, namespace string, name string) bool {
	// poll every 20 seconds
	var pollingInterval = time.Second * 10
	// timeout after 24 occurrences = 240 seconds = 4 minutes
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		out, _ := k.Resource(instanceGroupSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
		currentState, _, _ := unstructured.NestedString(out.UnstructuredContent(), "status", "currentState")
		log.Printf("InstanceGroup %v is in state: %v", name, currentState)
		if strings.ToLower(currentState) == "ready" {
			return true
		}
		time.Sleep(pollingInterval)
		log.Println("InstanceGroup not ready yet, retrying")
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func WaitForInstanceGroupDeletion(k dynamic.Interface, namespace string, name string) bool {
	// poll every 20 seconds
	var pollingInterval = time.Second * 10
	// timeout after 24 occurrences = 240 seconds = 4 minutes
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		_, err := k.Resource(instanceGroupSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true
		}
		time.Sleep(pollingInterval)
		log.Println("InstanceGroup still exists, retrying")
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func WaitForInstanceGroupString(k dynamic.Interface, namespace string, name string, path ...string) (string, error) {
	// poll every 20 seconds
	var pollingInterval = time.Second * 10
	// timeout after 24 occurrences = 240 seconds = 4 minutes
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		out, _ := k.Resource(instanceGroupSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
		val, ok, _ := unstructured.NestedString(out.UnstructuredContent(), path...)
		if ok {
			return val, nil
		}
		time.Sleep(pollingInterval)
		log.Println("value not populated yet, retrying")
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	err := fmt.Errorf("could not get value of %v in allotted time", path)
	return "", err
}
