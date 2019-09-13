package ekscloudformation

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

func createConfigMap(k kubernetes.Interface, cmData *corev1.ConfigMap) error {
	_, err := k.CoreV1().ConfigMaps(cmData.Namespace).Create(cmData)
	if err != nil {
		return err
	}
	for {
		fieldSelector := fmt.Sprintf("metadata.name=%v", cmData.Name)
		configMaps, err := k.CoreV1().ConfigMaps(cmData.Namespace).List(metav1.ListOptions{FieldSelector: fieldSelector})
		if err != nil {
			return err
		}

		if len(configMaps.Items) == 0 {
			log.Infoln("waiting for configmap to finish creating")
			time.Sleep(2 * time.Second)
		} else {
			break
		}
	}
	return nil
}

func (ctx *EksCfInstanceGroupContext) isResourceDeleting(s schema.GroupVersionResource, namespace, name string) (bool, error) {
	obj, err := ctx.KubernetesClient.KubeDynamic.Resource(s).Namespace(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	deletionTimestamp := obj.GetDeletionTimestamp()
	if !deletionTimestamp.IsZero() {
		return true, nil
	}
	return false, nil
}

func getConfigMap(k kubernetes.Interface, namespace string, name string, options metav1.GetOptions) (*corev1.ConfigMap, error) {
	cm, err := k.CoreV1().ConfigMaps(namespace).Get(name, options)
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func addAnnotation(u *unstructured.Unstructured, key, value string) {
	annotations := u.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	u.SetAnnotations(annotations)
}

func hasAnnotation(u *unstructured.Unstructured, key, value string) bool {
	annotations := u.GetAnnotations()
	if val, ok := annotations[key]; ok {
		if val == value {
			return true
		}
	}
	return false
}

func getUnstructuredPath(u *unstructured.Unstructured, jsonPath string) (string, error) {
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

func (ctx *EksCfInstanceGroupContext) reloadCloudformationConfiguration() error {
	template, err := LoadCloudformationConfiguration(ctx.GetInstanceGroup(), ctx.TemplatePath)
	if err != nil {
		return err
	}

	ctx.AwsWorker.TemplateBody = template
	return nil
}

func LoadControllerConfiguration(ig *v1alpha1.InstanceGroup, configPath string) (EksCfDefaultConfiguration, error) {
	var defaultConfig EksCfDefaultConfiguration
	var specConfig = &ig.Spec.EKSCFSpec.EKSCFConfiguration

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Errorf("controller config file not found: %v", err)
		return defaultConfig, err
	}

	controllerConfig, err := common.ReadFile(configPath)
	if err != nil {
		return defaultConfig, err
	}

	err = yaml.Unmarshal(controllerConfig, &defaultConfig)
	if err != nil {
		return defaultConfig, err
	}

	if len(defaultConfig.DefaultSubnets) != 0 {
		specConfig.SetSubnets(defaultConfig.DefaultSubnets)
	}

	if defaultConfig.EksClusterName != "" {
		specConfig.SetClusterName(defaultConfig.EksClusterName)
	}

	return defaultConfig, nil
}

func LoadCloudformationConfiguration(ig *v1alpha1.InstanceGroup, path string) (string, error) {
	var renderBuffer bytes.Buffer

	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Errorf("controller cloudformation template file not found: %v", err)
		return "", err
	}

	rawTemplate, err := common.ReadFile(path)
	if err != nil {
		return "", err
	}

	template, err := template.New("InstanceGroup").Funcs(funcMap).Parse(string(rawTemplate))
	if err != nil {
		return "", err
	}

	err = template.Execute(&renderBuffer, ig)
	if err != nil {
		return "", err
	}

	return renderBuffer.String(), nil
}
