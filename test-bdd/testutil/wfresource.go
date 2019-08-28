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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var successString = "completed"
var errorString = "error"
var statusKey = "currentStatus"
var resourceName = "rollingupgrades"

var workflowSchema = schema.GroupVersionResource{
	Group:    "upgrademgr.keikoproj.io",
	Version:  "v1alpha1",
	Resource: resourceName,
}

func WaitForWorkflowCreation(k dynamic.Interface, workflowNamespace string, workflowName string) bool {
	var pollingInterval = time.Second * 10
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		_, err := k.Resource(workflowSchema).Namespace(workflowNamespace).Get(workflowName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			log.Printf("could not find %v", resourceName)
			time.Sleep(pollingInterval)
			pollingCounter++
		} else {
			return true
		}
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func WaitForWorkflowSuccess(k dynamic.Interface, namespace string, name string) bool {
	var pollingInterval = time.Second * 30
	var timeoutCounter = 48
	var pollingCounter = 0

	for {
		workflow, err := k.Resource(workflowSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			log.Printf("could not get %v: %v", resourceName, err)
			return false
		}
		status, ok, _ := unstructured.NestedString(workflow.UnstructuredContent(), "status", statusKey)
		if ok {
			if strings.ToLower(status) == successString {
				return true
			} else if strings.ToLower(status) == errorString {
				log.Printf("%v has failed", resourceName)
				return false
			}
		} else {
			log.Printf("could not get %v status", resourceName)
			return false
		}

		log.Printf("waiting for %v status to complete reconcile", resourceName)
		time.Sleep(pollingInterval)
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}
