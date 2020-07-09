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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/onsi/gomega"
)

func TestGetDisabledMetrics(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	// No disable required
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"all"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize"),
		},
	})

	metrics, ok := ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeFalse())
	g.Expect(metrics).To(gomega.BeEmpty())

	// Disable all metrics
	ig.GetEKSConfiguration().SetMetricsCollection([]string{})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics(awsprovider.DefaultAutoscalingMetrics...),
		},
	})

	metrics, ok = ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ConsistOf(awsprovider.DefaultAutoscalingMetrics))

	// Disable specific metric
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize", "GroupMaxSize"),
		},
	})

	metrics, ok = ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ContainElement("GroupMaxSize"))
}

func TestGetEnabledMetrics(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	// Enable all metrics
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"all"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics(),
		},
	})

	metrics, ok := ctx.GetEnabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ConsistOf(awsprovider.DefaultAutoscalingMetrics))

	// Enable specific metric
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize", "GroupMaxSize"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize"),
		},
	})

	metrics, ok = ctx.GetEnabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ContainElement("GroupMaxSize"))
}

func TestGetLabelList(t *testing.T) {
	var (
		g                          = gomega.NewGomegaWithT(t)
		k                          = MockKubernetesClientSet()
		ig                         = MockInstanceGroup()
		configuration              = ig.GetEKSConfiguration()
		asgMock                    = NewAutoScalingMocker()
		iamMock                    = NewIamMocker()
		eksMock                    = NewEksMocker()
		expectedLabels115          = []string{"node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
		expectedLabels116          = []string{"node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithCustom   = []string{"custom.kubernetes.io=customlabel", "node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithOverride = []string{"custom.kubernetes.io=customlabel", "override.kubernetes.io=instance-group-1", "override2.kubernetes.io=instance-group-1"}
		overrideAnnotation         = map[string]string{OverrideDefaultLabelsAnnotationKey: "override.kubernetes.io=instance-group-1,override2.kubernetes.io=instance-group-1"}
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		clusterVersion           string
		instanceGroupLabels      map[string]string
		instanceGroupAnnotations map[string]string
		expectedLabels           []string
	}{
		// Default labels with missing cluster version
		{clusterVersion: "", expectedLabels: expectedLabels115},
		// Kubernetes 1.15 default labels
		{clusterVersion: "1.15", expectedLabels: expectedLabels115},
		// Kubernetes 1.16 default labels
		{clusterVersion: "1.16", expectedLabels: expectedLabels116},
		// Kubernetes 1.16 default labels with custom labels
		{clusterVersion: "1.16", instanceGroupLabels: map[string]string{"custom.kubernetes.io": "customlabel"}, expectedLabels: expectedLabelsWithCustom},
		// custom labels with override labels
		{clusterVersion: "1.16", instanceGroupAnnotations: overrideAnnotation, instanceGroupLabels: map[string]string{"custom.kubernetes.io": "customlabel"}, expectedLabels: expectedLabelsWithOverride},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		configuration.SetLabels(tc.instanceGroupLabels)
		ig.SetAnnotations(tc.instanceGroupAnnotations)
		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			Cluster: &eks.Cluster{
				Version: aws.String(tc.clusterVersion),
			},
		})

		labels := ctx.GetLabelList()
		g.Expect(labels).To(gomega.Equal(tc.expectedLabels))
	}
}

func TestGetUserDataStages(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		preBootstrapScript  string
		postBootstrapScript string
	}{
		{},
		{preBootstrapScript: "", postBootstrapScript: ""},
		{preBootstrapScript: "prebootstrap", postBootstrapScript: "postbootstrap"},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
			Stage: v1alpha1.PreBootstrapStage,
			Data:  tc.preBootstrapScript,
		})
		configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
			Stage: v1alpha1.PostBootstrapStage,
			Data:  tc.postBootstrapScript,
		})
		configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
			Stage: "invalid-stage",
			Data:  "test",
		})
		preScript, postScript := ctx.GetUserDataStages()
		g.Expect(preScript).To(gomega.Equal(tc.preBootstrapScript))
		g.Expect(postScript).To(gomega.Equal(tc.postBootstrapScript))
	}
}

func TestIsRetryable(t *testing.T) {
	var (
		g  = gomega.NewGomegaWithT(t)
		ig = MockInstanceGroup()
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
