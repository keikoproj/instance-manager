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

var workflowSchema = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "workflows",
}

func WaitForWorkflowCreation(k dynamic.Interface, workflowNamespace string, workflowName string) bool {
	// poll every 20 seconds
	var pollingInterval = time.Second * 10
	// timeout after 24 occurrences = 240 seconds = 4 minutes
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		_, err := k.Resource(workflowSchema).Namespace(workflowNamespace).Get(workflowName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			log.Printf("could not find workflow")
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
	// poll every 20 seconds
	var pollingInterval = time.Second * 10
	// timeout after 24 occurrences = 240 seconds = 4 minutes
	var timeoutCounter = 24
	var pollingCounter = 0

	for {
		workflow, err := k.Resource(workflowSchema).Namespace(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			log.Printf("could not get workflow: %v", err)
			return false
		}
		status, ok, _ := unstructured.NestedString(workflow.UnstructuredContent(), "status", "phase")
		if ok {
			if strings.ToLower(status) == "succeeded" {
				return true
			} else if strings.ToLower(status) == "failed" {

			}

		} else {
			log.Printf("could not get workflow status")
			return false
		}

		time.Sleep(pollingInterval)
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}
