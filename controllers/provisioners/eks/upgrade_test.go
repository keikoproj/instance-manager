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
	"context"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/ghodss/yaml"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestUpgradeCRDStrategyValidation(t *testing.T) {
	var (
		g   = gomega.NewGomegaWithT(t)
		k   = MockKubernetesClientSet()
		ig  = MockInstanceGroup()
		cr  = MockCustomResourceSpec()
		crd = MockCustomResourceDefinition()
	)

	config := ig.GetEKSConfiguration()

	// assume initial state of modifying
	ig.SetState(v1alpha1.ReconcileModifying)
	config.Subnets = []string{"subnet-1"}
	config.Image = "ami-1234"
	config.NodeSecurityGroups = []string{"sg-12323"}
	config.InstanceType = "m5.large"
	config.KeyPairName = "myKey"

	// get custom resource yaml
	crYAML, err := yaml.Marshal(cr.Object)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// create dogs crd
	definitionsGvr := kubeprovider.GetGVR(crd, "customresourcedefinitions")
	_, err = k.KubeDynamic.Resource(definitionsGvr).Create(context.Background(), crd, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var (
		valid              = MockAwsCRDStrategy(string(crYAML))
		missingName        = MockAwsCRDStrategy(string(crYAML))
		missingSpec        = MockAwsCRDStrategy(string(crYAML))
		missingConcurrency = MockAwsCRDStrategy(string(crYAML))
		missingStatusPath  = MockAwsCRDStrategy(string(crYAML))
		missingSuccessStr  = MockAwsCRDStrategy(string(crYAML))
		missingFailureStr  = MockAwsCRDStrategy(string(crYAML))
	)

	missingName.CRDType.CRDName = ""
	missingSpec.CRDType.Spec = ""
	missingConcurrency.CRDType.ConcurrencyPolicy = ""
	missingStatusPath.CRDType.StatusJSONPath = ""
	missingSuccessStr.CRDType.StatusSuccessString = ""
	missingFailureStr.CRDType.StatusFailureString = ""

	tests := []struct {
		input         v1alpha1.AwsUpgradeStrategy
		shouldErr     bool
		expectedState v1alpha1.ReconcileState
	}{
		{input: valid, shouldErr: false},
		{input: missingName, shouldErr: true},
		{input: missingSpec, shouldErr: true},
		{input: missingConcurrency, shouldErr: false},
		{input: missingStatusPath, shouldErr: true},
		{input: missingSuccessStr, shouldErr: true},
		{input: missingFailureStr, shouldErr: true},
	}

	for i, tc := range tests {
		t.Logf("#%v - %+v", i, tc.input)
		var errOccured bool
		ig.SetUpgradeStrategy(tc.input)
		err := ig.Validate(&v1alpha1.ValidationOverrides{})
		if err != nil {
			t.Log(err)
			errOccured = true
		}
		g.Expect(errOccured).To(gomega.Equal(tc.shouldErr))
	}
	g.Expect(missingConcurrency.CRDType.ConcurrencyPolicy).To(gomega.Equal("forbid"))
}

func TestUpgradeInvalidStrategy(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	// assume initial state of modifying
	ig.SetState(v1alpha1.ReconcileModifying)
	ig.SetUpgradeStrategy(v1alpha1.AwsUpgradeStrategy{
		Type: "bad-strategy",
	})
	err := ctx.UpgradeNodes()
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestBootstrapNodes(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	// assume initial state of modifying
	ig.SetState(v1alpha1.ReconcileModifying)
	err := ctx.BootstrapNodes()
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestUpgradeCRDStrategy(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		cr      = MockCustomResourceSpec()
		crd     = MockCustomResourceDefinition()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			TargetResource: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-lc-0123"),
			},
		},
	})
	cr.SetName("captain-0123")
	ig.Status.SetActiveScalingGroupName("my-asg")
	// get custom resource yaml
	crYAML, err := yaml.Marshal(cr.Object)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// create dogs crd
	definitionsGvr := kubeprovider.GetGVR(crd, "customresourcedefinitions")
	crGvr := kubeprovider.GetGVR(cr, "dogs")
	_, err = k.KubeDynamic.Resource(definitionsGvr).Create(context.Background(), crd, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// add dog custom resource to strategy
	ig.SetUpgradeStrategy(MockAwsCRDStrategy(string(crYAML)))

	// initial cr submission
	ig.SetState(v1alpha1.ReconcileModifying)
	err = ctx.UpgradeNodes()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	tests := []struct {
		input         string
		shouldErr     bool
		expectedState v1alpha1.ReconcileState
	}{
		{input: "", shouldErr: false, expectedState: v1alpha1.ReconcileModifying},
		{input: "woof", shouldErr: false, expectedState: v1alpha1.ReconcileModified},
		{input: "grr", shouldErr: true, expectedState: v1alpha1.ReconcileErr},
		{input: "bla", shouldErr: false, expectedState: v1alpha1.ReconcileModifying},
	}

	for i, tc := range tests {
		t.Logf("#%v - \"%v\"", i, tc.input)
		unstructured.SetNestedField(cr.Object, tc.input, "status", "dogStatus")
		_, err := k.KubeDynamic.Resource(crGvr).Namespace("default").Update(context.Background(), cr, metav1.UpdateOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var errOccured bool
		ig.SetState(v1alpha1.ReconcileModifying)
		err = ctx.UpgradeNodes()
		if err != nil {
			errOccured = true
		}
		g.Expect(errOccured).To(gomega.Equal(tc.shouldErr))
		g.Expect(ctx.GetState()).To(gomega.Equal(tc.expectedState))
	}
}

func TestUpgradeRollingUpdateStrategyPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		maxUnavailable          intstr.IntOrString
		scalingInstances        []*autoscaling.Instance
		withTerminateErr        bool
		expectedState           v1alpha1.ReconcileState
		readyNodes              int
		unreadyNodes            int
		withLaunchTemplate      bool
		withLaunchConfiguration bool
		withMixedInstances      bool
	}{
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromString("25%"), readyNodes: 3, scalingInstances: MockScalingInstances(0, 3), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromString("25%"), readyNodes: 2, scalingInstances: MockScalingInstances(1, 2), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromString("25%"), readyNodes: 0, scalingInstances: MockScalingInstances(2, 1), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromString("25%"), readyNodes: 3, scalingInstances: MockScalingInstances(3, 0), expectedState: v1alpha1.ReconcileModified},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromInt(3), readyNodes: 3, scalingInstances: MockScalingInstances(0, 3), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromInt(5), readyNodes: 3, scalingInstances: MockScalingInstances(0, 3), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromInt(0), readyNodes: 3, scalingInstances: MockScalingInstances(0, 3), expectedState: v1alpha1.ReconcileModifying},
		{withLaunchConfiguration: true, maxUnavailable: intstr.FromString("60%"), readyNodes: 2, scalingInstances: MockScalingInstances(1, 2), withTerminateErr: true, expectedState: v1alpha1.ReconcileModifying},
		{withLaunchTemplate: true, maxUnavailable: intstr.FromString("60%"), readyNodes: 2, scalingInstances: MockScalingInstances(1, 2), withTerminateErr: true, expectedState: v1alpha1.ReconcileModifying},
		{withMixedInstances: true, maxUnavailable: intstr.FromString("60%"), readyNodes: 2, scalingInstances: MockScalingInstances(1, 2), withTerminateErr: true, expectedState: v1alpha1.ReconcileModifying},
	}

	for i, tc := range tests {
		t.Logf("#%v - \"%v\"", i, tc.scalingInstances)

		if tc.withTerminateErr {
			asgMock.TerminateInstanceInAutoScalingGroupErr = errors.New("some-error")
		}
		// delete all mock nodes
		allNodes, err := k.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		for _, node := range allNodes.Items {
			err = k.Kubernetes.CoreV1().Nodes().Delete(context.Background(), node.Name, metav1.DeleteOptions{})
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}

		for i := 0; i < tc.readyNodes; i++ {
			id := aws.StringValue(tc.scalingInstances[i].InstanceId)
			_, err := k.Kubernetes.CoreV1().Nodes().Create(context.Background(), MockNode(id, corev1.ConditionTrue), metav1.CreateOptions{})
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}

		for i := tc.readyNodes; i < tc.unreadyNodes; i++ {
			id := aws.StringValue(tc.scalingInstances[i].InstanceId)
			_, err := k.Kubernetes.CoreV1().Nodes().Create(context.Background(), MockNode(id, corev1.ConditionFalse), metav1.CreateOptions{})
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}

		nodes, err := k.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		ig.SetUpgradeStrategy(MockAwsRollingUpdateStrategy(&tc.maxUnavailable))
		mockScalingGroup := &autoscaling.Group{
			AutoScalingGroupName: aws.String("some-scaling-group"),
			Instances:            tc.scalingInstances,
			DesiredCapacity:      aws.Int64(int64(len(tc.scalingInstances))),
		}
		if tc.withLaunchConfiguration {
			mockScalingGroup.LaunchConfigurationName = aws.String("some-launch-config")
		}
		if tc.withLaunchTemplate {
			mockScalingGroup.LaunchTemplate = &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("some-launch-template"),
			}
		}
		if tc.withMixedInstances {
			mockScalingGroup.MixedInstancesPolicy = &autoscaling.MixedInstancesPolicy{
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("some-launch-template"),
					},
				},
			}
		}

		scalingConfig, err := scaling.NewLaunchConfiguration("", w, &scaling.DiscoverConfigurationInput{ScalingGroup: mockScalingGroup})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingGroup:         mockScalingGroup,
			ScalingConfiguration: scalingConfig,
			ClusterNodes:         nodes,
		})

		ig.SetState(v1alpha1.ReconcileModifying)
		err = ctx.UpgradeNodes()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ctx.GetState()).To(gomega.Equal(tc.expectedState))
	}
}

func TestRotateWarmPool(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		isDrifted      bool
		warmPoolSpec   *v1alpha1.WarmPoolSpec
		warmPoolConfig *autoscaling.WarmPoolConfiguration
		shouldRequeue  bool
		shouldDelete   bool
	}{
		// if warmPoolSpec is not configured, rotation should be skipped
		{warmPoolSpec: nil, shouldRequeue: false},
		// if warmPoolConfig is not configured, rotation should be skipped
		{warmPoolConfig: nil, shouldRequeue: false},
		// if warm pool instances are not drifted, rotation should be skipped
		{warmPoolSpec: MockWarmPoolSpec(-1, 0), warmPoolConfig: MockWarmPool(-1, 0, ""), isDrifted: false, shouldRequeue: false},
		// if warm pool instances are drifted, we should rotate
		{warmPoolSpec: MockWarmPoolSpec(-1, 0), warmPoolConfig: MockWarmPool(-1, 0, ""), isDrifted: true, shouldRequeue: true},
	}

	for i, tc := range tests {
		t.Logf("test #%v", i)
		ig.Spec.EKSSpec.WarmPool = tc.warmPoolSpec
		asgMock.DeleteWarmPoolCallCount = 0
		asgMock.PutWarmPoolCallCount = 0
		asgMock.DescribeWarmPoolCallCount = 0

		if tc.isDrifted {
			asgMock.WarmPoolInstances = MockScalingInstances(2, 2)
		} else {
			asgMock.WarmPoolInstances = MockScalingInstances(4, 0)
		}

		mockScalingGroup := MockScalingGroup("my-asg", false)
		mockScalingGroup.LaunchConfigurationName = aws.String("some-launch-config")
		mockScalingGroup.WarmPoolConfiguration = tc.warmPoolConfig

		scalingConfig, err := scaling.NewLaunchConfiguration("", w, &scaling.DiscoverConfigurationInput{ScalingGroup: mockScalingGroup})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingGroup:         mockScalingGroup,
			ScalingConfiguration: scalingConfig,
		})

		ok, err := ctx.rotateWarmPool()
		g.Expect(err).NotTo(gomega.HaveOccurred())

		if tc.shouldDelete {
			g.Expect(asgMock.DeleteWarmPoolCallCount).To(gomega.Equal(1))
			g.Expect(asgMock.DescribeWarmPoolCallCount).To(gomega.Equal(1))
		}
		if tc.shouldRequeue {
			g.Expect(ok).To(gomega.BeTrue())
		}
	}
}
