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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/user"
	"reflect"
	"strings"
	"sync"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	log = ctrl.Log.WithName("kubernetes-provider")
)

type KubernetesClientSet struct {
	Kubernetes  kubernetes.Interface
	KubeDynamic dynamic.Interface
}

type DrainManager struct {
	DrainErrors chan error
	DrainGroup  *sync.WaitGroup
}

func GetUnstructuredInstanceGroup(instanceGroup *v1alpha1.InstanceGroup) (*unstructured.Unstructured, error) {
	var obj = &unstructured.Unstructured{}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(instanceGroup)
	if err != nil {
		return obj, err
	}
	obj.Object = content
	return obj, nil
}

func IsDesiredNodesReady(nodes *corev1.NodeList, instanceIds []string, desiredCount int) (bool, error) {
	if len(instanceIds) != desiredCount {
		return false, nil
	}

	readyInstances := GetReadyNodeNamesByInstance(instanceIds, nodes)

	// if discovered nodes match provided instance ids, condition is ready
	if common.StringSliceEquals(readyInstances, instanceIds) {
		return true, nil
	}

	return false, nil
}

func IsMinNodesReady(nodes *corev1.NodeList, instanceIds []string, minCount int) (bool, error) {
	// if count of instances in scaling group is not over min, requeue
	if len(instanceIds) < minCount {
		return false, nil
	}

	readyInstances := GetReadyNodeNamesByInstance(instanceIds, nodes)

	// if discovered nodes match provided instance ids, condition is ready
	if common.StringSliceContains(readyInstances, instanceIds) {
		return true, nil
	}

	return false, nil
}

func GetReadyNodeNamesByInstance(instanceIds []string, nodes *corev1.NodeList) []string {
	readyInstances := make([]string, 0)
	for _, id := range instanceIds {
		for _, node := range nodes.Items {
			if IsNodeReady(node) && common.GetLastElementBy(node.Spec.ProviderID, "/") == id {
				readyInstances = append(readyInstances, id)
			}
		}
	}
	return readyInstances
}

func GetNodesByInstance(instanceIds []string, nodes *corev1.NodeList) *corev1.NodeList {
	nodeList := &corev1.NodeList{
		ListMeta: metav1.ListMeta{},
		Items:    []corev1.Node{},
	}

	for _, id := range instanceIds {
		for _, node := range nodes.Items {
			if common.GetLastElementBy(node.Spec.ProviderID, "/") == id {
				nodeList.Items = append(nodeList.Items, node)
			}
		}
	}
	return nodeList
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

func HasAnnotation(annotations map[string]string, key, value string) bool {
	if val, ok := annotations[key]; ok {
		if strings.EqualFold(val, value) {
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

func GetKubernetesClient() (kubernetes.Interface, error) {
	var config *rest.Config
	config, err := GetKubernetesConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func GetKubernetesDynamicClient() (dynamic.Interface, error) {
	var config *rest.Config
	config, err := GetKubernetesConfig()
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func GetKubernetesConfig() (*rest.Config, error) {
	var config *rest.Config
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = GetKubernetesLocalConfig()
		if err != nil {
			return nil, err
		}
		return config, nil
	}
	return config, nil
}

func RenderCustomResource(tpl string, params interface{}) (string, error) {
	var renderBuffer bytes.Buffer
	template, err := template.New("Template").Parse(tpl)
	if err != nil {
		return "", err
	}
	err = template.Execute(&renderBuffer, params)
	if err != nil {
		return "", err
	}
	return renderBuffer.String(), nil
}

func GetKubernetesLocalConfig() (*rest.Config, error) {
	var kubePath string
	if os.Getenv("KUBECONFIG") != "" {
		kubePath = os.Getenv("KUBECONFIG")
	} else {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		}
		kubePath = usr.HomeDir + "/.kube/config"
	}

	if kubePath == "" {
		err := fmt.Errorf("failed to get kubeconfig path")
		return nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubePath)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func CRDExists(kubeClient dynamic.Interface, name string) bool {
	CRDSchema := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1beta1", Resource: "customresourcedefinitions"}
	_, err := kubeClient.Resource(CRDSchema).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	return true
}

func ParseCustomResourceYaml(raw string) (*unstructured.Unstructured, error) {
	var err error
	cr := unstructured.Unstructured{}
	data := []byte(raw)
	err = yaml.Unmarshal(data, &cr.Object)
	if err != nil {
		return &cr, err
	}
	return &cr, nil
}

func ConfigmapHash(cm *corev1.ConfigMap) string {
	var buf strings.Builder

	if reflect.DeepEqual(*cm, corev1.ConfigMap{}) {
		return ""
	}

	cmStr := cm.String()
	buf.WriteString(cmStr[strings.Index(cm.String(), ",Data:")+1:])
	return common.StringMD5(buf.String())
}

func IsStorageError(err error) bool {
	if common.ContainsEqualFoldSubstring(err.Error(), "StorageError: invalid object") {
		return true
	}
	return false
}

func IsPathValue(resource unstructured.Unstructured, path, value string) bool {
	val, err := GetUnstructuredPath(&resource, path)
	if err != nil {
		log.Error(err, "failed to get unstructured path from resource", "path", path)
		return false
	}

	if strings.EqualFold(val, value) {
		return true
	}

	return false
}

func CRDFullName(resource, group string) string {
	return strings.Join([]string{resource, group}, ".")
}

type statusPatch struct {
	from v1alpha1.InstanceGroup
}

func (s *statusPatch) Type() types.PatchType {
	return types.MergePatchType
}

func (s *statusPatch) Data(obj runtime.Object) ([]byte, error) {
	origObj := s.from.DeepCopyObject()
	originalJSON, err := json.Marshal(origObj)
	if err != nil {
		return nil, err
	}

	modObj := obj.(*v1alpha1.InstanceGroup)
	modObj.Spec = v1alpha1.InstanceGroupSpec{}
	modifiedJSON, err := json.Marshal(modObj.DeepCopyObject())
	if err != nil {
		return nil, err
	}

	return jsonpatch.CreateMergePatch(originalJSON, modifiedJSON)
}

func MergePatch(obj v1alpha1.InstanceGroup) client.Patch {
	obj.Spec = v1alpha1.InstanceGroupSpec{}
	return &statusPatch{obj}
}
