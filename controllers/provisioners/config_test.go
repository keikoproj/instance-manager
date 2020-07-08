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

package provisioners

import (
	"testing"

	"github.com/ghodss/yaml"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func MockConfigMap(data map[string]string) *corev1.ConfigMap {
	if data == nil {
		return &corev1.ConfigMap{}
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-manager",
			Namespace: "kube-system",
		},
		Data: data,
	}
}

func MockConfigData(keysAndValues ...string) map[string]string {
	d := map[string]string{}
	for i := 0; i < len(keysAndValues); i = i + 2 {
		d[keysAndValues[i]] = keysAndValues[i+1]
	}
	return d
}

func TestSetConfigurationDefaults(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	crVolume := v1alpha1.NodeVolume{
		Name: "/dev/crvolume",
		Type: "gp2",
		Size: 60,
	}

	crTags := map[string]string{
		"key":   "cr-tag-key",
		"value": "cr-tag-value",
	}

	crSecurityGroups := []string{"sg-123456789012"}

	instanceGroup := &v1alpha1.InstanceGroup{
		Spec: v1alpha1.InstanceGroupSpec{
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				Type:    "crd",
				CRDType: &v1alpha1.CRDUpdateStrategy{},
			},
			EKSSpec: &v1alpha1.EKSSpec{
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					Taints: []corev1.Taint{
						{
							Key:    "cr-taint-key",
							Value:  "cr-taint-value",
							Effect: "NoSchedule",
						},
					},
					Labels:             map[string]string{},
					Volumes:            []v1alpha1.NodeVolume{crVolume},
					Tags:               []map[string]string{crTags},
					NodeSecurityGroups: crSecurityGroups,
				},
			},
		},
	}

	mockBoundaries := `
restricted:
- spec.eks.configuration.taints
- spec.eks.configuration.labels
shared:
- spec.eks.configuration.volumes
- spec.eks.configuration.tags
- spec.eks.configuration.securityGroups
- spec.eks.configuration.instanceType
- spec.strategy`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
      volumes:
      - size: 50
        type: gp2
        name: /dev/xvda
      tags:
      - key: tag-key
        value: tag-value`

	defaultLabels := map[string]string{"label-key": "label-value"}
	defaultTags := map[string]string{"key": "tag-key", "value": "tag-value"}
	defaultVolume := v1alpha1.NodeVolume{
		Name: "/dev/xvda",
		Type: "gp2",
		Size: 50,
	}

	defaultTaints := []corev1.Taint{
		{
			Key:    "taint-key",
			Value:  "taint-value",
			Effect: "NoSchedule",
		},
	}

	defaultStrategy := v1alpha1.AwsUpgradeStrategy{
		Type:    "rollingUpdate",
		CRDType: &v1alpha1.CRDUpdateStrategy{},
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.String,
				StrVal: "30%",
			},
		},
	}

	defaultConfig, err := UnmarshalConfiguration(MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults)))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	instanceGroup, err = SetConfigurationDefaults(instanceGroup, defaultConfig)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// Restricted values get 1st priority
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal(defaultTaints))
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(defaultLabels))
	// Shared values get merged
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.Volumes).To(gomega.ConsistOf(defaultVolume, crVolume))
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.Tags).To(gomega.ConsistOf(defaultTags, crTags))
	g.Expect(instanceGroup.Spec.AwsUpgradeStrategy).To(gomega.Equal(defaultStrategy))
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal(crSecurityGroups))
	g.Expect(instanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.large"))
}

func TestUnmarshalConfiguration(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	defaultConfig, err := UnmarshalConfiguration(nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(defaultConfig).To(gomega.Equal(&DefaultConfiguration{}))

	mockBoundaries := `
restricted:
- spec.eks.configuration.taints
- spec.eks.configuration.labels
shared:
- spec.eks.configuration.volumes
- spec.eks.configuration.tags`

	mockDefaults := `
spec:
  eks:
    configuration:
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
      volumes:
      - size: 30
        type: gp2
        name: /dev/xvda
      tags:
      - key: tag-key
        value: tag-value`

	expectedDefaults := map[string]interface{}{}
	err = yaml.Unmarshal([]byte(mockDefaults), &expectedDefaults)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defaultConfig, err = UnmarshalConfiguration(MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults)))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(defaultConfig.Boundaries.Restricted).To(gomega.ConsistOf("spec.eks.configuration.taints", "spec.eks.configuration.labels"))
	g.Expect(defaultConfig.Boundaries.Shared).To(gomega.ConsistOf("spec.eks.configuration.volumes", "spec.eks.configuration.tags"))
	g.Expect(defaultConfig.Defaults).To(gomega.Equal(expectedDefaults))
}
