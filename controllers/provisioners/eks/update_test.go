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

	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

func TestUpdateScalingGroupPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize", "GroupMaxSize", "GroupDesiredCapacity"})

	// avoid drift / rotation
	input := ctx.GetLaunchConfigurationInput("some-launch-config")
	mockLaunchConfig := MockLaunchConfigFromInput(input)
	mockScalingGroup := &autoscaling.Group{
		EnabledMetrics:       MockEnabledMetrics("GroupInServiceInstances", "GroupMinSize"),
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

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup:                  mockScalingGroup,
		ActiveLaunchConfigurationName: "some-launch-config",
		LaunchConfiguration:           mockLaunchConfig,
	})

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
	_, err := k.Kubernetes.CoreV1().Nodes().Create(mockNode)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestUpdateWithDriftRotationPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
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

	// missing launch config causes drift
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup:                  mockScalingGroup,
		ActiveLaunchConfigurationName: "some-launch-config",
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("some-instance-arn"),
		},
	})

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
	_, err := k.Kubernetes.CoreV1().Nodes().Create(mockNode)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileInitUpgrade))
}

func TestUpdateWithRotationPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	// avoid drift / rotation
	input := ctx.GetLaunchConfigurationInput("some-launch-config")
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

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup:                  mockScalingGroup,
		ActiveLaunchConfigurationName: "some-launch-config",
		LaunchConfiguration:           mockLaunchConfig,
	})

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
	_, err := k.Kubernetes.CoreV1().Nodes().Create(mockNode)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Update()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileInitUpgrade))
}

func TestLaunchConfigurationDrifted(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)
	input := ctx.GetLaunchConfigurationInput("some-launch-config")

	var (
		imgDrift  = MockLaunchConfigFromInput(input)
		instDrift = MockLaunchConfigFromInput(input)
		ipDrift   = MockLaunchConfigFromInput(input)
		sgDrift   = MockLaunchConfigFromInput(input)
		spDrift   = MockLaunchConfigFromInput(input)
		keyDrift  = MockLaunchConfigFromInput(input)
		usrDrift  = MockLaunchConfigFromInput(input)
		devDrift  = MockLaunchConfigFromInput(input)
	)
	imgDrift.ImageId = aws.String("some-image")
	instDrift.InstanceType = aws.String("some-type")
	ipDrift.IamInstanceProfile = aws.String("some-instance-profile")
	sgDrift.SecurityGroups = aws.StringSlice([]string{"sg-1", "sg-2"})
	spDrift.SpotPrice = aws.String("some-price")
	keyDrift.KeyName = aws.String("some-key")
	usrDrift.UserData = aws.String("some-userdata")
	devDrift.BlockDeviceMappings = []*autoscaling.BlockDeviceMapping{
		w.GetBasicBlockDevice("some-device", "some-type", 0),
	}

	tests := []struct {
		input    *autoscaling.LaunchConfiguration
		expected bool
	}{
		{input: MockLaunchConfigFromInput(input), expected: false},
		{input: imgDrift, expected: true},
		{input: instDrift, expected: true},
		{input: ipDrift, expected: true},
		{input: sgDrift, expected: true},
		{input: spDrift, expected: true},
		{input: keyDrift, expected: true},
		{input: usrDrift, expected: true},
		{input: devDrift, expected: true},
	}

	for _, tc := range tests {
		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ActiveLaunchConfigurationName: "some-launch-config",
			LaunchConfiguration:           tc.input,
		})
		got := ctx.LaunchConfigurationDrifted()
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
	)

	w := MockAwsWorker(asgMock, iamMock)
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
	})

	asgMock.DescribeAutoScalingGroupsErr = errors.New("some-describe-error")
	err := ctx.Update()
	t.Log(err)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.DescribeAutoScalingGroupsErr = nil

	asgMock.UpdateAutoScalingGroupErr = errors.New("some-update-error")
	err = ctx.Update()
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
