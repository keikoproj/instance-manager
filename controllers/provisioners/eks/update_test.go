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

	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestUpdateWithDriftRotationPositive(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
		DesiredCapacity:      aws.Int64(1),
		Instances: []*autoscaling.Instance{
			{
				InstanceId:              aws.String("i-1234"),
				LaunchConfigurationName: aws.String("some-launch-config"),
			},
		},
		Tags: []*autoscaling.TagDescription{
			{
				Key:   aws.String("some-tag"),
				Value: aws.String("some-value"),
			},
		},
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}

	configuration.Tags = []map[string]string{
		{
			"key":   "some-tag",
			"value": "some-value",
		},
		{
			"key":   "other-tag",
			"value": "other-value",
		},
	}
	// create matching node object
	mockNode := &corev1.Node{
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-1234",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	_, err := k.Kubernetes.CoreV1().Nodes().Create(context.Background(), mockNode, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nodes, err := k.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// missing launch config causes drift
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: mockScalingGroup,
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("some-instance-arn"),
		},
		ClusterNodes: nodes,
		Cluster:      MockEksCluster("1.15"),
	})

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.TagsUpdateNeeded()).To(gomega.BeTrue())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}

func TestUpdateWithLaunchTemplate(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		spec          = ig.GetEKSSpec()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	spec.Type = v1alpha1.LaunchTemplate

	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
		DesiredCapacity:      aws.Int64(1),
		LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
			LaunchTemplateName: aws.String("some-launch-template"),
		},
		Instances: []*autoscaling.Instance{
			{
				InstanceId: aws.String("i-1234"),
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					LaunchTemplateName: aws.String("some-launch-template"),
				},
			},
		},
		Tags: []*autoscaling.TagDescription{
			{
				Key:   aws.String("some-tag"),
				Value: aws.String("some-value"),
			},
		},
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}

	// create matching node object
	mockNode := &corev1.Node{
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-1234",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	_, err := k.Kubernetes.CoreV1().Nodes().Create(context.Background(), mockNode, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nodes, err := k.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: mockScalingGroup,
		ScalingConfiguration: &scaling.LaunchTemplate{
			AwsWorker:      w,
			TargetResource: &ec2.LaunchTemplate{},
			LatestVersion: &ec2.LaunchTemplateVersion{
				VersionNumber: aws.Int64(1),
				LaunchTemplateData: &ec2.ResponseLaunchTemplateData{
					ImageId: aws.String("ami-1234"),
					IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileSpecification{
						Arn: aws.String("aws:arn:some-role"),
					},
					SecurityGroupIds:    make([]*string, 0),
					KeyName:             aws.String(""),
					UserData:            aws.String(""),
					BlockDeviceMappings: []*ec2.LaunchTemplateBlockDeviceMapping{},
				},
			},
		},
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("some-instance-arn"),
		},
		ClusterNodes: nodes,
		Cluster:      MockEksCluster("1.15"),
	})

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.CreateLaunchTemplateVersionCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ec2Mock.ModifyLaunchTemplateCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))

	state := ctx.GetDiscoveredState()
	ec2Mock.CreateLaunchTemplateVersionCallCount = 0
	ec2Mock.ModifyLaunchTemplateCallCount = 0
	state.ScalingGroup.LaunchTemplate = nil
	state.ScalingGroup.MixedInstancesPolicy = &autoscaling.MixedInstancesPolicy{
		LaunchTemplate: &autoscaling.LaunchTemplate{
			LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("some-launch-template"),
			},
		},
	}

	configuration.InstanceType = "x1.medium"
	pool := v1alpha1.SubFamilyFlexibleInstancePool
	ratio := intstr.FromInt(50)
	strategy := v1alpha1.LaunchTemplateStrategyCapacityOptimized
	state.InstancePool.SubFamilyFlexiblePool.Pool = map[string][]InstanceSpec{
		"x1.medium": {
			{
				Type:   "x1a.medium",
				Weight: "1",
			},
			{
				Type:   "x1d.medium",
				Weight: "1",
			},
		},
	}
	configuration.MixedInstancesPolicy = &v1alpha1.MixedInstancesPolicySpec{
		InstancePool: &pool,
		SpotRatio:    &ratio,
		Strategy:     &strategy,
	}

	ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{
		{
			LaunchTemplateName: aws.String("some-launch-template"),
		},
	}
	state.ScalingConfiguration.Discover(&scaling.DiscoverConfigurationInput{
		ScalingGroup: mockScalingGroup,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.CreateLaunchTemplateVersionCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ec2Mock.ModifyLaunchTemplateCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))

	ec2Mock.CreateLaunchTemplateVersionCallCount = 0
	ec2Mock.ModifyLaunchTemplateCallCount = 0
	instances := []*v1alpha1.InstanceTypeSpec{
		{
			Type:   "x1.large",
			Weight: 1,
		},
		{
			Type:   "x2.large",
			Weight: 1,
		},
	}
	configuration.MixedInstancesPolicy = &v1alpha1.MixedInstancesPolicySpec{
		InstanceTypes: instances,
		SpotRatio:     &ratio,
		Strategy:      &strategy,
	}
	state.ScalingConfiguration.Discover(&scaling.DiscoverConfigurationInput{
		ScalingGroup: mockScalingGroup,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.CreateLaunchTemplateVersionCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ec2Mock.ModifyLaunchTemplateCallCount).To(gomega.Equal(uint(1)))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}

func TestUpdateWithRotationPositive(t *testing.T) {
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

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		Cluster: MockEksCluster("1.15"),
	})

	input := &autoscaling.CreateLaunchConfigurationInput{
		LaunchConfigurationName: aws.String("some-launch-config"),
	}
	mockLaunchConfig := MockLaunchConfigFromInput(input)

	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
		DesiredCapacity:      aws.Int64(1),
		Instances: []*autoscaling.Instance{
			{
				InstanceId: aws.String("i-1234"),
				// wrong launch-config causes rotation
				LaunchConfigurationName: aws.String("some-wrong-launch-config"),
			},
		},
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}

	// create matching node object
	mockNode := &corev1.Node{
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-west-2a/i-1234",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	_, err := k.Kubernetes.CoreV1().Nodes().Create(context.Background(), mockNode, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nodes, err := k.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: mockScalingGroup,
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker:      w,
			TargetResource: mockLaunchConfig,
		},
		ClusterNodes: nodes,
		Cluster:      MockEksCluster("1.15"),
	})

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}

func TestLaunchConfigurationDrifted(t *testing.T) {
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

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		Cluster: &eks.Cluster{
			Version: aws.String("1.15"),
		},
	})

	lcInput := &autoscaling.CreateLaunchConfigurationInput{
		LaunchConfigurationName: aws.String("some-launch-config"),
		ImageId:                 aws.String("ami-123456789"),
		InstanceType:            aws.String("m5.large"),
		IamInstanceProfile:      aws.String("some-profile"),
		SpotPrice:               aws.String("1.0"),
		SecurityGroups:          aws.StringSlice([]string{"sg-1", "sg-2"}),
		KeyName:                 aws.String("somekey"),
		UserData:                aws.String("userdata"),
		BlockDeviceMappings:     []*autoscaling.BlockDeviceMapping{w.GetAutoScalingBasicBlockDevice("/dev/xvda", "gp2", "", 40, 100, 200, nil, nil)},
	}

	existingConfig := &scaling.CreateConfigurationInput{
		Name:                  "some-launch-config",
		ImageId:               "ami-123456789",
		InstanceType:          "m5.large",
		IamInstanceProfileArn: "some-profile",
		SpotPrice:             "1.0",
		SecurityGroups:        []string{"sg-1", "sg-2"},
		KeyName:               "somekey",
		UserData:              "userdata",
		Volumes: []v1alpha1.NodeVolume{
			{
				Name:       "/dev/xvda",
				Type:       "gp2",
				Size:       40,
				Iops:       100,
				Throughput: 200,
			},
		},
	}

	var (
		imgDrift  = MockLaunchConfigFromInput(lcInput)
		instDrift = MockLaunchConfigFromInput(lcInput)
		ipDrift   = MockLaunchConfigFromInput(lcInput)
		sgDrift   = MockLaunchConfigFromInput(lcInput)
		spDrift   = MockLaunchConfigFromInput(lcInput)
		keyDrift  = MockLaunchConfigFromInput(lcInput)
		usrDrift  = MockLaunchConfigFromInput(lcInput)
		devDrift  = MockLaunchConfigFromInput(lcInput)
	)
	imgDrift.ImageId = aws.String("some-image")
	instDrift.InstanceType = aws.String("some-type")
	ipDrift.IamInstanceProfile = aws.String("some-instance-profile")
	sgDrift.SecurityGroups = aws.StringSlice([]string{"sg-4", "sg-3"})
	spDrift.SpotPrice = aws.String("some-price")
	keyDrift.KeyName = aws.String("some-key")
	usrDrift.UserData = aws.String("some-userdata")
	devDrift.BlockDeviceMappings = []*autoscaling.BlockDeviceMapping{
		w.GetAutoScalingBasicBlockDevice("some-device", "some-type", "", 32, 0, 0, nil, nil),
	}

	tests := []struct {
		input    *autoscaling.LaunchConfiguration
		expected bool
	}{
		{input: MockLaunchConfigFromInput(lcInput), expected: false},
		{input: imgDrift, expected: true},
		{input: instDrift, expected: true},
		{input: ipDrift, expected: true},
		{input: sgDrift, expected: true},
		{input: spDrift, expected: true},
		{input: keyDrift, expected: true},
		{input: usrDrift, expected: true},
		{input: devDrift, expected: true},
	}

	for i, tc := range tests {
		t.Logf("Test #%v", i)
		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingConfiguration: &scaling.LaunchConfiguration{
				AwsWorker:      w,
				TargetResource: tc.input,
			},
			Cluster: MockEksCluster("1.15"),
		})
		got := ctx.DiscoveredState.ScalingConfiguration.Drifted(existingConfig)
		g.Expect(got).To(gomega.Equal(tc.expected))
	}
}

func TestUpdateScalingGroupNegative(t *testing.T) {
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
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize", "GroupMaxSize", "GroupDesiredCapacity"})

	mockScalingGroup := &autoscaling.Group{
		EnabledMetrics:       MockEnabledMetrics("GroupInServiceInstances", "GroupMinSize"),
		AutoScalingGroupName: aws.String("some-scaling-group"),
		DesiredCapacity:      aws.Int64(1),
		Instances:            []*autoscaling.Instance{},
	}

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: mockScalingGroup,
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("some-instance-arn"),
		},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
		Cluster: MockEksCluster("1.15"),
	})

	asgMock.UpdateAutoScalingGroupErr = errors.New("some-update-error")
	err := ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.UpdateAutoScalingGroupErr = nil

	asgMock.CreateLaunchConfigurationErr = errors.New("some-create-error")
	err = ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.CreateLaunchConfigurationErr = nil

	iamMock.GetRoleErr = errors.New("some-get-error")
	iamMock.CreateRoleErr = errors.New("some-create-error")
	err = ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	iamMock.GetRoleErr = nil
	iamMock.CreateRoleErr = nil

	asgMock.DisableMetricsCollectionErr = errors.New("some-error")
	err = ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.DisableMetricsCollectionErr = nil

	asgMock.EnableMetricsCollectionErr = errors.New("some-error")
	err = ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}

func TestScalingGroupUpdatePredicate(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		spec          = ig.GetEKSSpec()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)
	spec.MinSize = int64(3)
	spec.MaxSize = int64(6)
	configuration.SetSubnets([]string{"subnet-1", "subnet-2", "subnet-3"})

	mockScalingGroupMin := MockScalingGroup("asg-1", false)
	mockScalingGroupMin.MinSize = aws.Int64(0)
	mockScalingGroupMax := MockScalingGroup("asg-2", false)
	mockScalingGroupMax.MaxSize = aws.Int64(0)
	mockScalingGroupSubnets := MockScalingGroup("asg-3", false)
	mockScalingGroupSubnets.VPCZoneIdentifier = aws.String("subnet-0")
	mockScalingGroupLaunchConfig := MockScalingGroup("asg-4", false)
	mockScalingGroupLaunchConfig.LaunchConfigurationName = aws.String("different-name")

	tests := []struct {
		input    *autoscaling.Group
		expected bool
	}{
		{input: MockScalingGroup("asg-0", false), expected: false},
		{input: mockScalingGroupLaunchConfig, expected: true},
		{input: mockScalingGroupMin, expected: true},
		{input: mockScalingGroupMax, expected: true},
		{input: mockScalingGroupSubnets, expected: true},
	}

	for i, tc := range tests {
		t.Logf("Test #%v", i)
		ctx.SetDiscoveredState(&DiscoveredState{
			Cluster: MockEksCluster(""),
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingGroup: tc.input,
			ScalingConfiguration: &scaling.LaunchConfiguration{
				AwsWorker: w,
				TargetResource: &autoscaling.LaunchConfiguration{
					LaunchConfigurationName: aws.String("some-launch-configuration"),
				},
			},
		})
		got := ctx.ScalingGroupUpdateNeeded("some-launch-configuration")
		g.Expect(got).To(gomega.Equal(tc.expected))
	}
}

func TestUpdateManagedPolicies(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
	)

	defaultPolicies := []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
	defaultPoliciesIrsa := []string{"AmazonEKSWorkerNodePolicy", "AmazonEC2ContainerRegistryReadOnly"}
	defaultPoliciesWarmPool := []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		attachedPolicies   []*iam.AttachedPolicy
		additionalPolicies []string
		expectedAttached   uint
		expectedDetached   uint
		irsaEnabled        bool
		hasWarmPool        bool
	}{
		// default policies attached, no changes needed
		{attachedPolicies: MockAttachedPolicies(defaultPolicies...), additionalPolicies: []string{}, expectedAttached: 0, expectedDetached: 0},
		// when IRSA is enabled, cni policy needs to be detached
		{attachedPolicies: MockAttachedPolicies(defaultPolicies...), additionalPolicies: []string{}, irsaEnabled: true, expectedAttached: 0, expectedDetached: 1},
		// when IRSA is disabled, cni policy needs to be attached
		{attachedPolicies: MockAttachedPolicies(defaultPoliciesIrsa...), additionalPolicies: []string{}, irsaEnabled: false, expectedAttached: 1, expectedDetached: 0},
		// when warm pool is enabled, managed role is added
		{attachedPolicies: MockAttachedPolicies(defaultPolicies...), additionalPolicies: []string{}, hasWarmPool: true, expectedAttached: 0, expectedDetached: 0},
		// when warm pool is disabled, managed role is removed
		{attachedPolicies: MockAttachedPolicies(defaultPoliciesWarmPool...), additionalPolicies: []string{}, hasWarmPool: false, expectedAttached: 0, expectedDetached: 0},
		// default policies not attached
		{attachedPolicies: MockAttachedPolicies(), additionalPolicies: []string{}, expectedAttached: 3, expectedDetached: 0},
		// additional policies need to be attached
		{attachedPolicies: MockAttachedPolicies(defaultPolicies...), additionalPolicies: []string{"policy-1", "policy-2"}, expectedAttached: 2, expectedDetached: 0},
		// additional policies with ARN
		{attachedPolicies: MockAttachedPolicies(defaultPolicies...), additionalPolicies: []string{"arn:aws:iam::aws:policy/policy-1", "arn:aws:iam::12345679012:policy/policy-2"}, expectedAttached: 2, expectedDetached: 0},
		// additional policies need to be detached
		{attachedPolicies: MockAttachedPolicies("AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly", "policy-1"), additionalPolicies: []string{}, expectedAttached: 0, expectedDetached: 1},
		// additional policies need to be attached & detached
		{attachedPolicies: MockAttachedPolicies("AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly", "policy-1"), additionalPolicies: []string{"policy-2"}, expectedAttached: 1, expectedDetached: 1},
	}

	for i, tc := range tests {
		t.Logf("test #%v", i)
		iamMock.AttachRolePolicyCallCount = 0
		iamMock.DetachRolePolicyCallCount = 0

		if tc.irsaEnabled {
			ig.Annotations[IRSAEnabledAnnotation] = "true"
		} else {
			ig.Annotations[IRSAEnabledAnnotation] = "false"
		}

		if tc.hasWarmPool {
			ig.Spec.EKSSpec.WarmPool = &v1alpha1.WarmPoolSpec{
				MaxSize: int64(-1),
				MinSize: int64(0),
			}
		} else {
			ig.Spec.EKSSpec.WarmPool = nil
		}

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			AttachedPolicies: tc.attachedPolicies,
		})
		configuration.SetManagedPolicies(tc.additionalPolicies)
		err := ctx.UpdateManagedPolicies("some-role")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(iamMock.AttachRolePolicyCallCount).To(gomega.Equal(tc.expectedAttached))
		g.Expect(iamMock.DetachRolePolicyCallCount).To(gomega.Equal(tc.expectedDetached))
	}
}

type amiTest struct {
	igImage     string
	expectedAmi string
	osFamily    string
}

func TestUpdateWithLatestAmiID(t *testing.T) {
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

	testLatestAmiID := "ami-12345678"
	testCustomAmi := "ami-98765432"
	testBottlerocketAmi := "ami-56789101"
	testWindowsAmi := "ami-56789201"

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
		DesiredCapacity:      aws.Int64(1),
		Instances: []*autoscaling.Instance{
			{
				InstanceId:              aws.String("i-1234"),
				LaunchConfigurationName: aws.String("some-launch-config"),
			},
		},
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}

	// skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")
	iamMock.Role = &iam.Role{
		Arn:      aws.String("some-arn"),
		RoleName: aws.String("some-role"),
	}

	// Setup Latest AMI
	ssmMock.parameterMap = map[string]string{
		"/aws/service/eks/optimized-ami/1.18/amazon-linux-2/recommended/image_id":                      testLatestAmiID,
		"/aws/service/eks/optimized-ami/1.18/amazon-linux-2/amazon-eks-node-1.18-v20220226/image_id":   testCustomAmi,
		"/aws/service/bottlerocket/aws-k8s-1.18/x86_64/1.6.1/image_id":                                 testBottlerocketAmi,
		"/aws/service/ami-windows-latest/Windows_Server-2019-English-Core-EKS_Optimized-1.18/image_id": testWindowsAmi,
	}
	ec2Mock.InstanceTypes = []*ec2.InstanceTypeInfo{
		&ec2.InstanceTypeInfo{
			InstanceType: aws.String("m5.large"),
			ProcessorInfo: &ec2.ProcessorInfo{
				SupportedArchitectures: []*string{aws.String("x86_64")},
			},
		},
	}

	testAmis := []amiTest{
		{
			igImage:     "latest",
			expectedAmi: testLatestAmiID,
			osFamily:    "amazonlinux2",
		},
		{
			igImage:     "ssm://amazon-eks-node-1.18-v20220226",
			expectedAmi: testCustomAmi,
			osFamily:    "amazonlinux2",
		},
		{
			igImage:     "ssm://1.6.1",
			expectedAmi: testBottlerocketAmi,
			osFamily:    "bottlerocket",
		},
		{
			igImage:     "ssm://latest",
			expectedAmi: testWindowsAmi,
			osFamily:    "windows",
		},
	}

	for _, tc := range testAmis {
		ig.GetEKSConfiguration().Image = tc.igImage
		ig.Annotations[OsFamilyAnnotation] = tc.osFamily
		err := ctx.CloudDiscovery()
		g.Expect(err).NotTo(gomega.HaveOccurred())

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingGroup: mockScalingGroup,
			ScalingConfiguration: &scaling.LaunchConfiguration{
				AwsWorker: w,
			},
			InstanceProfile: &iam.InstanceProfile{
				Arn: aws.String("some-instance-arn"),
			},
			ClusterNodes: nil,
			Cluster:      nil,
			InstanceTypeInfo: []*ec2.InstanceTypeInfo{
				{
					InstanceType: aws.String("m5.large"),
					ProcessorInfo: &ec2.ProcessorInfo{
						SupportedArchitectures: []*string{aws.String("x86_64")},
					},
				},
			},
		})

		err = ctx.Update()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ctx.GetInstanceGroup().Spec.EKSSpec.EKSConfiguration.Image).To(gomega.Equal(tc.expectedAmi))
	}

}
