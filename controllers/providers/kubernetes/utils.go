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
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/keikoproj/instance-manager/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

func IsDesiredNodesReady(kube kubernetes.Interface, instanceIds []string, desiredCount int) (bool, error) {
	readyDesiredInstances := make([]string, 0)
	nodes, err := kube.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	// if count of instances in scaling group does not match desired, need to requeue
	if len(instanceIds) != desiredCount {
		return false, nil
	}

	for _, id := range instanceIds {
		for _, node := range nodes.Items {
			if IsNodeReady(node) && common.GetLastElementBy(node.Spec.ProviderID, "/") == id {
				readyDesiredInstances = append(readyDesiredInstances, id)
			}

		}
	}

	// if discovered nodes match provided instance ids, condition is ready
	if common.StringSliceEquals(readyDesiredInstances, instanceIds) {
		log.Infof("desired instances %s are ready", strings.Join(readyDesiredInstances, ","))
		return true, nil
	}

	return false, nil
}

func IsNodeReady(n corev1.Node) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func AddAnnotation(u *unstructured.Unstructured, key, value string) {
	annotations := u.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	u.SetAnnotations(annotations)
}

func HasAnnotation(u *unstructured.Unstructured, key, value string) bool {
	annotations := u.GetAnnotations()
	if val, ok := annotations[key]; ok {
		if val == value {
			return true
		}
	}
	return false
}

func GetUnstructuredPath(u *unstructured.Unstructured, jsonPath string) (string, error) {
	splitFunction := func(c rune) bool {
		return c == '.'
	}
	statusPath := strings.FieldsFunc(jsonPath, splitFunction)

	value, _, err := unstructured.NestedString(u.UnstructuredContent(), statusPath...)
	if err != nil {
		return "", err
	}
	return value, nil
}

func GetGVR(customResource *unstructured.Unstructured, CRDName string) schema.GroupVersionResource {
	GVK := customResource.GroupVersionKind()

	var resourceName string
	if strings.HasSuffix(CRDName, GVK.Group) {
		s := strings.Split(CRDName, ".")
		resourceName = s[0]
	} else {
		resourceName = CRDName
	}

	return schema.GroupVersionResource{
		Group:    GVK.Group,
		Version:  GVK.Version,
		Resource: resourceName,
	}
}
