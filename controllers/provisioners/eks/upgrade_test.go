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

package eks

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ghodss/yaml"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/onsi/gomega"
)

func TestUpgradeCRDStrategyPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		cr      = MockCustomResourceSpec()
		crd     = MockCustomResourceDefinition()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := New(ig, k, w)

	// assume initial state of modifying
	ig.SetState(v1alpha1.ReconcileModifying)

	// get custom resource yaml
	crYAML, err := yaml.Marshal(cr.Object)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// create dogs crd
	definitionsGvr := kubeprovider.GetGVR(crd, "customresourcedefinitions")
	crGvr := kubeprovider.GetGVR(cr, "dogs")
	_, err = k.KubeDynamic.Resource(definitionsGvr).Create(crd, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// add dog custom resource to strategy
	ig.SetUpgradeStrategy(v1alpha1.AwsUpgradeStrategy{
		Type: kubeprovider.CRDStrategyName,
		CRDType: &v1alpha1.CRDUpgradeStrategy{
			Spec:                string(crYAML),
			CRDName:             crGvr.Resource,
			StatusJSONPath:      ".status.dogStatus",
			StatusSuccessString: "woof",
			StatusFailureString: "grr",
		},
	})

	// no status, requeue
	err = ctx.UpgradeNodes()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))

	unstructured.SetNestedField(cr.Object, "woof", "status", "dogStatus")
	_, err = k.KubeDynamic.Resource(crGvr).Namespace(cr.GetNamespace()).Update(cr, metav1.UpdateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.UpgradeNodes()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))

	unstructured.SetNestedField(cr.Object, "grr", "status", "dogStatus")
	_, err = k.KubeDynamic.Resource(crGvr).Namespace(cr.GetNamespace()).Update(cr, metav1.UpdateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.UpgradeNodes()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileErr))
}
