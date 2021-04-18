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

func MockResource() *v1alpha1.InstanceGroup {
	return &v1alpha1.InstanceGroup{
		Spec: v1alpha1.InstanceGroupSpec{
			EKSSpec: &v1alpha1.EKSSpec{
				EKSConfiguration: &v1alpha1.EKSConfiguration{},
			},
		},
	}
}

func MockVolume(name, tp string, size int64) v1alpha1.NodeVolume {
	return v1alpha1.NodeVolume{
		Name: name,
		Type: tp,
		Size: size,
	}
}

func MockLabels(keysAndValues ...string) map[string]string {
	lbl := map[string]string{}
	for i := 0; i < len(keysAndValues); i = i + 2 {
		lbl[keysAndValues[i]] = keysAndValues[i+1]
	}
	return lbl
}

func MockTag(key, value string) map[string]string {
	tag := map[string]string{}
	tag["key"] = key
	tag["value"] = value
	return tag
}

func MockTaint(key, value, effect string) corev1.Taint {
	return corev1.Taint{
		Key:    key,
		Value:  value,
		Effect: corev1.TaintEffect(effect),
	}
}

func TestSetDefaultsRestricted(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    restricted:
    - spec.eks.configuration.keyPairName
    - spec.eks.configuration.taints
    - spec.eks.configuration.labels
    - spec.eks.configuration.securityGroups
    - spec.eks.configuration.instanceType
    - spec.strategy`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family=windows"
  defaults:
    spec:
      eks:
        configuration:
          image: ami-22222222222
`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.xlarge"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("other-label-key", "other-label-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("other-taint-key", "other-taint-value", "NoExecute")}
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{Type: "crd"}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// All restricted fields must be overwritten with default value even if the user sets them
	g.Expect(c.InstanceGroup.Spec.AwsUpgradeStrategy).To(gomega.Equal(v1alpha1.AwsUpgradeStrategy{
		Type: "rollingUpdate",
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
		},
	}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal([]string{"sg-123456789012"}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.large"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("label-key", "label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{MockTaint("taint-key", "taint-value", "NoSchedule")}))

	// Fields without defaults should stay as provided
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.EksClusterName).To(gomega.Equal("someCluster"))

	// Defaults without boundary should not be set
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal(""))

	// Fields with defaults are used when CR does not provide it
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.KeyPairName).To(gomega.Equal("TestKeyPair"))
}

func TestSetDefaultsWithRestrictedConditional(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    restricted:
    - spec.eks.configuration.keyPairName
    - spec.eks.configuration.taints
    - spec.eks.configuration.labels
    - spec.eks.configuration.securityGroups
    - spec.eks.configuration.instanceType
    - spec.eks.configuration.image
    - spec.strategy`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family=windows"
  defaults:
    spec:
      eks:
        configuration:
          image: ami-22222222222
- annotationSelector: "instancemgr.keikoproj.io/arch=arm64"
  defaults:
    spec:
      eks:
        configuration:
          image: ami-33333333333
`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	cr.Annotations = make(map[string]string)
	cr.Annotations["instancemgr.keikoproj.io/os-family"] = "windows"
	cr.Annotations["instancemgr.keikoproj.io/arch"] = "arm64"
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.xlarge"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("other-label-key", "other-label-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("other-taint-key", "other-taint-value", "NoExecute")}
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{Type: "crd"}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// All restricted fields must be overwritten with default value even if the user sets them
	g.Expect(c.InstanceGroup.Spec.AwsUpgradeStrategy).To(gomega.Equal(v1alpha1.AwsUpgradeStrategy{
		Type: "rollingUpdate",
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
		},
	}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal([]string{"sg-123456789012"}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.large"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("label-key", "label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{MockTaint("taint-key", "taint-value", "NoSchedule")}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal("ami-33333333333"))

	// Fields without defaults should stay as provided
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.EksClusterName).To(gomega.Equal("someCluster"))

	// Fields with defaults are used when CR does not provide it
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.KeyPairName).To(gomega.Equal("TestKeyPair"))
}

func TestSetDefaultsWithSharedConditionalReplace(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    shared:
      replace: 
      - spec.eks.configuration.tags`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family=windows"
  defaults:
    spec:
      eks:
        configuration:
          securityGroups:
          - sg-923456789012
          tags:
          - key: tag-A
            value: value-A
          - key: tag-B
            value: value-B`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	cr.Annotations = make(map[string]string)
	cr.Annotations["instancemgr.keikoproj.io/os-family"] = "windows"
	cr.Annotations["instancemgr.keikoproj.io/arch"] = "arm64"
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.Tags = []map[string]string{
		MockTag("tag-A", "value-D"),
		MockTag("tag-C", "value-C"),
	}
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{Type: "crd"}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	//Should not replace
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal([]string{"sg-000000000000"}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Tags).To(gomega.Equal([]map[string]string{
		MockTag("tag-A", "value-D"),
		MockTag("tag-C", "value-C"),
	}))
}

func TestSetDefaultsWithSharedConditionalMergeOverride(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    shared:
      mergeOverride: 
      - spec.eks.configuration.tags`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/arch in (arm64),instancemgr.keikoproj.io/os-family in (windows)"
  defaults:
    spec:
      eks:
        configuration:
          tags:
          - key: tag-A
            value: value-A
          - key: tag-B
            value: value-B`
	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	cr.Annotations = make(map[string]string)
	cr.Annotations["instancemgr.keikoproj.io/os-family"] = "windows"
	cr.Annotations["instancemgr.keikoproj.io/arch"] = "arm64"
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.Tags = []map[string]string{
		MockTag("tag-A", "value-D"),
		MockTag("tag-C", "value-C"),
	}
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{Type: "crd"}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	//Should not replace
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal([]string{"sg-000000000000"}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Tags).To(gomega.Equal([]map[string]string{
		MockTag("tag-A", "value-D"),
		MockTag("tag-B", "value-B"),
		MockTag("tag-C", "value-C"),
	}))
}

func TestSetDefaultsWithInvalidConditionalYAML(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    shared:
      mergeOverride: 
      - spec.eks.configuration.tags`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family=windows"
  defaults:
    spec:
      eks:
        # invalid TAB character on following line
		configuration:
          tags:
          - key: tag-A
            value: value-A
          - key: tag-B
            value: value-B`
	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	_, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestSetDefaultsWithInvalidConditionalSelector(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Restricted fields are always replaced with default values

	mockBoundaries := `
    shared:
      mergeOverride: 
      - spec.eks.configuration.tags`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family==f==windows"
  defaults:
    spec:
      eks:
        configuration:
          tags:
          - key: tag-A
            value: value-A
          - key: tag-B
            value: value-B`
	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule
`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	c, _ := NewProvisionerConfiguration(cm, cr)
	err := c.SetDefaults()
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestSetDefaultsWithSharedConditionalMerge(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	mockBoundaries := `
    shared:
      merge:
      - spec.eks.configuration.securityGroups`

	mockConditionals := `
- annotationSelector: "instancemgr.keikoproj.io/os-family=windows"
  defaults:
    spec:
      eks:
        configuration:
          securityGroups:
          - sg-923456789012`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      securityGroups:
      - sg-1234`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults, "conditionals", mockConditionals))
	cr := MockResource()
	cr.Annotations = make(map[string]string)
	cr.Annotations["instancemgr.keikoproj.io/os-family"] = "windows"
	cr.Annotations["instancemgr.keikoproj.io/arch"] = "arm64"
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{Type: "crd"}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	//Should not replace
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.Equal([]string{
		"sg-1234",
		"sg-923456789012",
		"sg-000000000000",
	}))
}

func TestSetDefaultsSharedMergeOverride(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Shared merge override fields are resource-provided values merged with default values (favoring the custom resource on conflict)

	mockBoundaries := `
    shared:
      mergeOverride:
      - spec.eks.configuration.roleName
      - spec.eks.configuration.keyPairName
      - spec.eks.configuration.taints
      - spec.eks.configuration.labels
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
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      keyPairName: TestKeyPair
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults))
	cr := MockResource()
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{
		Type: "crd",
		CRDType: &v1alpha1.CRDUpdateStrategy{
			CRDName: "myCrd",
		},
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
	}
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.xlarge"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("other-label-key", "other-label-value", "label-key", "other-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("other-taint-key", "other-taint-value", "NoExecute")}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Shared merge fields must merge slices/maps and consist of both CR/Default objects if there is no conflict
	g.Expect(c.InstanceGroup.Spec.AwsUpgradeStrategy).To(gomega.Equal(v1alpha1.AwsUpgradeStrategy{
		Type: "crd",
		CRDType: &v1alpha1.CRDUpdateStrategy{
			CRDName: "myCrd",
		},
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
	}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.ConsistOf("sg-000000000000", "sg-123456789012"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.xlarge"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("label-key", "other-value", "other-label-key", "other-label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{
		MockTaint("taint-key", "taint-value", "NoSchedule"),
		MockTaint("other-taint-key", "other-taint-value", "NoExecute"),
	}))

	// Fields without defaults should stay as provided
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.EksClusterName).To(gomega.Equal("someCluster"))

	// Defaults without boundary should not be set
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal(""))

	// Fields with defaults are used when CR does not provide it
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.KeyPairName).To(gomega.Equal("TestKeyPair"))
}

func TestSetDefaultsSharedMerge(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Shared merge fields are resource-provided values merged with default values (favoring the default on conflict)

	mockBoundaries := `
    shared:
      merge:
      - spec.eks.configuration.roleName
      - spec.eks.configuration.keyPairName
      - spec.eks.configuration.taints
      - spec.eks.configuration.labels
      - spec.eks.configuration.securityGroups
      - spec.eks.configuration.instanceType`

	mockDefaults := `
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
  eks:
    configuration:
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      keyPairName: TestKeyPair
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults))
	cr := MockResource()

	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.xlarge"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("other-label-key", "other-label-value", "label-key", "other-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("other-taint-key", "other-taint-value", "NoExecute")}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Shared merge fields must merge slices/maps and consist of both CR/Default objects if there is no conflict
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.ConsistOf("sg-000000000000", "sg-123456789012"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.xlarge"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("label-key", "label-value", "other-label-key", "other-label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{
		MockTaint("taint-key", "taint-value", "NoSchedule"),
		MockTaint("other-taint-key", "other-taint-value", "NoExecute"),
	}))

	// Fields without defaults should stay as provided
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.EksClusterName).To(gomega.Equal("someCluster"))

	// Defaults without boundary should not be set
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal(""))

	// Fields with defaults are used when CR does not provide it
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.KeyPairName).To(gomega.Equal("TestKeyPair"))
}

func TestSetDefaultsSharedMergeOverrideConflict(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// When there are merge conflicts, the resource value should override the default

	mockBoundaries := `
    shared:
      mergeOverride:
      - spec.eks.configuration.tags
      - spec.eks.configuration.volumes
      - spec.eks.configuration.taints
      - spec.eks.configuration.labels
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
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      tags:
      - key: tag
        value: tag-value
      - key: tag2
        value: tag-value-2
      volumes:
      - name: /dev/xvda
        type: gp2
        size: 30
      - name: /dev/xvdc
        type: gp2
        size: 30
      labels:
        test: test
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults))
	cr := MockResource()

	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-025bf02d663404bbc"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.large"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("label-key", "other-label-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("taint-key", "taint-value", "NoExecute")}
	cr.Spec.EKSSpec.EKSConfiguration.Tags = []map[string]string{
		MockTag("tag", "other-value"),
		MockTag("other-tag", "value"),
		MockTag("tag2", "tag-value-2"),
	}
	cr.Spec.EKSSpec.EKSConfiguration.Volumes = []v1alpha1.NodeVolume{
		MockVolume("/dev/xvda", "gp2", 35),
		MockVolume("/dev/xvdb", "gp2", 30),
		MockVolume("/dev/xvdc", "gp2", 30),
	}
	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Shared merge fields must merge slices/maps and consist of both CR/Default objects if there is no conflict
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.ConsistOf("sg-025bf02d663404bbc", "sg-123456789012"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.large"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("test", "test", "label-key", "other-label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{
		MockTaint("taint-key", "taint-value", "NoSchedule"),
		MockTaint("taint-key", "taint-value", "NoExecute"),
	}))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Tags).To(gomega.ConsistOf(
		MockTag("tag", "other-value"),
		MockTag("other-tag", "value"),
		MockTag("tag2", "tag-value-2"),
	))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Volumes).To(gomega.ConsistOf(
		MockVolume("/dev/xvda", "gp2", 35),
		MockVolume("/dev/xvdb", "gp2", 30),
		MockVolume("/dev/xvdc", "gp2", 30),
	))
}

func TestSetDefaultsSharedReplace(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	// Shared replace fields are resource-provided values which can replace default values

	mockBoundaries := `
    shared:
      replace:
      - spec.eks.configuration.roleName
      - spec.eks.configuration.keyPairName
      - spec.eks.configuration.taints
      - spec.eks.configuration.labels
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
      keyPairName: TestKeyPair
      image: ami-025bf02d663404bbc
      securityGroups:
      - sg-123456789012
      instanceType: m5.large
      labels:
        label-key: label-value
      taints:
      - key: taint-key
        value: taint-value
        effect: NoSchedule`

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults))
	cr := MockResource()
	cr.Spec.AwsUpgradeStrategy = v1alpha1.AwsUpgradeStrategy{
		Type: "crd",
		CRDType: &v1alpha1.CRDUpdateStrategy{
			CRDName: "myCrd",
		},
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
	}
	cr.Spec.EKSSpec.EKSConfiguration.EksClusterName = "someCluster"
	cr.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups = []string{"sg-000000000000"}
	cr.Spec.EKSSpec.EKSConfiguration.InstanceType = "m5.xlarge"
	cr.Spec.EKSSpec.EKSConfiguration.Labels = MockLabels("other-label-key", "other-label-value")
	cr.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{MockTaint("other-taint-key", "other-taint-value", "NoExecute")}

	c, err := NewProvisionerConfiguration(cm, cr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.SetDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Shared merge fields must merge slices/maps and consist of both CR/Default objects if there is no conflict
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.NodeSecurityGroups).To(gomega.ConsistOf("sg-000000000000"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.InstanceType).To(gomega.Equal("m5.xlarge"))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Labels).To(gomega.Equal(MockLabels("other-label-key", "other-label-value")))
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Taints).To(gomega.Equal([]corev1.Taint{
		MockTaint("other-taint-key", "other-taint-value", "NoExecute"),
	}))

	// Fields without defaults should stay as provided
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.EksClusterName).To(gomega.Equal("someCluster"))

	// Defaults without boundary should not be set
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal(""))

	// Fields with defaults are used when CR does not provide it
	g.Expect(c.InstanceGroup.Spec.EKSSpec.EKSConfiguration.KeyPairName).To(gomega.Equal("TestKeyPair"))
}

func TestUnmarshalConfiguration(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	mockBoundaries := `
restricted:
- spec.eks.configuration.taints
- spec.eks.configuration.labels
shared:
  mergeOverride:
  - spec.eks.configuration.volumes
  merge:
  - spec.eks.configuration.volumes
  - spec.eks.configuration.tags
  replace:
  - spec.eks.configuration.taints
  - spec.eks.configuration.labels`

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
	err := yaml.Unmarshal([]byte(mockDefaults), &expectedDefaults)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	cm := MockConfigMap(MockConfigData("boundaries", mockBoundaries, "defaults", mockDefaults))
	c, err := NewProvisionerConfiguration(cm, &v1alpha1.InstanceGroup{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(c.Boundaries.Restricted).To(gomega.ConsistOf("spec.eks.configuration.taints", "spec.eks.configuration.labels"))
	g.Expect(c.Boundaries.Shared.Merge).To(gomega.ConsistOf("spec.eks.configuration.volumes", "spec.eks.configuration.tags"))
	g.Expect(c.Boundaries.Shared.MergeOverride).To(gomega.ConsistOf("spec.eks.configuration.volumes"))
	g.Expect(c.Boundaries.Shared.Replace).To(gomega.ConsistOf("spec.eks.configuration.taints", "spec.eks.configuration.labels"))
	g.Expect(c.Defaults).To(gomega.Equal(expectedDefaults))
}

func TestIsRetryable(t *testing.T) {
	var (
		g  = gomega.NewGomegaWithT(t)
		ig = &v1alpha1.InstanceGroup{}
	)

	tests := []struct {
		state             v1alpha1.ReconcileState
		expectedRetryable bool
	}{
		{state: v1alpha1.ReconcileErr, expectedRetryable: false},
		{state: v1alpha1.ReconcileReady, expectedRetryable: false},
		{state: v1alpha1.ReconcileDeleted, expectedRetryable: false},
		{state: v1alpha1.ReconcileDeleting, expectedRetryable: true},
		{state: v1alpha1.ReconcileInit, expectedRetryable: true},
		{state: v1alpha1.ReconcileInitCreate, expectedRetryable: true},
		{state: v1alpha1.ReconcileInitDelete, expectedRetryable: true},
		{state: v1alpha1.ReconcileInitUpdate, expectedRetryable: true},
		{state: v1alpha1.ReconcileInitUpgrade, expectedRetryable: true},
		{state: v1alpha1.ReconcileModified, expectedRetryable: true},
		{state: v1alpha1.ReconcileModifying, expectedRetryable: true},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		ig.SetState(tc.state)

		retryable := IsRetryable(ig)
		g.Expect(retryable).To(gomega.Equal(tc.expectedRetryable))
	}
}
