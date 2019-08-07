package ekscloudformation

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func updateConfigMap(k kubernetes.Interface, cmData *corev1.ConfigMap) error {
	_, err := k.CoreV1().ConfigMaps(cmData.Namespace).Update(cmData)
	if err != nil {
		return err
	}
	return nil
}

func getConfigMap(k kubernetes.Interface, namespace string, name string, options metav1.GetOptions) (*corev1.ConfigMap, error) {
	cm, err := k.CoreV1().ConfigMaps(namespace).Get(name, options)
	if err != nil {
		return nil, err
	}
	return cm, nil
}
