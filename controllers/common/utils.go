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

package common

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/user"
	"strings"
	"time"

	yaml "github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var log = logrus.New()

type KubernetesClientSet struct {
	Kubernetes  kubernetes.Interface
	KubeDynamic dynamic.Interface
}

func init() {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

// ContainsString returns true if a given slice 'slice' contains string 's', otherwise return false
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// RemoveString removes a string 's' from slice 'slice'
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// ConcatonateList joins lists to strings delimited with `delimiter`
func ConcatonateList(list []string, delimiter string) string {
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(list)), delimiter), "[]")
}

func ReadFile(path string) ([]byte, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("failed to read file %v", path)
		return nil, err
	}
	return f, nil
}

func GetKubernetesClient() (kubernetes.Interface, error) {
	var config *rest.Config
	config, err := GetKubernetesConfig()
	if err != nil {
		log.Errorf("failed to create kubernetes client config")
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("failed to create kubernetes client")
		return nil, err
	}
	return client, nil
}

func GetKubernetesDynamicClient() (dynamic.Interface, error) {
	var config *rest.Config
	config, err := GetKubernetesConfig()
	if err != nil {
		log.Errorf("failed to create kubernetes client config")
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Errorf("failed to create kubernetes dynamic client")
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
			log.Errorf("failed to get local kubernetes auth")
			return nil, err
		}
		log.Warnf("using local kubernetes config")
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
		log.Errorf("failed to get local kubernetes auth")
		return nil, err
	}
	return config, nil
}

func CRDExists(kubeClient dynamic.Interface, name string) bool {
	CRDSchema := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1beta1", Resource: "customresourcedefinitions"}
	_, err := kubeClient.Resource(CRDSchema).Get(name, metav1.GetOptions{})
	if err != nil {
		fmt.Println(err)
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
		fmt.Println(err)
		return &cr, err
	}
	return &cr, nil
}

func GetTimeString() string {
	n := time.Now().UTC()
	return fmt.Sprintf("%v%v%v%v%v", n.Year(), int(n.Month()), n.Day(), n.Hour(), n.Minute())
}
