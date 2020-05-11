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
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
)

func TestCreateManagedRolePositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)
	state := ctx.GetDiscoveredState()

	// Mock role/profile do not exist so they are always created
	iamMock.GetRoleErr = errors.New("not found")
	iamMock.GetInstanceProfileErr = errors.New("not found")

	fakeRole := &iam.Role{RoleName: aws.String("some-role")}
	fakeProfile := &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
		Arn:                 aws.String("some-profile-arn"),
	}

	iamMock.Role = fakeRole
	iamMock.InstanceProfile = fakeProfile

	err := ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(state.GetRole()).To(gomega.Equal(fakeRole))
	g.Expect(state.GetInstanceProfile()).To(gomega.Equal(fakeProfile))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateLaunchConfigurationPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	// Skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")

	ctx.SetDiscoveredState(&DiscoveredState{
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("some-profile-arn"),
		},
	})

	lcName := "some-launch-config"
	mockLaunchConfiguration := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String(lcName),
	}
	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{mockLaunchConfiguration}

	err := ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetDiscoveredState().GetActiveLaunchConfigurationName()).To(gomega.Equal(lcName))
	g.Expect(ig.GetStatus().GetActiveLaunchConfigurationName()).To(gomega.Equal(lcName))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateScalingGroupPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	// skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")

	// skip launch-config creation
	mockLaunchConfiguration := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("some-launch-config"),
	}

	ctx.SetDiscoveredState(&DiscoveredState{
		LaunchConfiguration: mockLaunchConfiguration,
	})

	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}

	err := ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetDiscoveredState().GetScalingGroup()).To(gomega.Equal(mockScalingGroup))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateNoOp(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	// skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")

	// skip launch-config creation
	mockLaunchConfiguration := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("some-launch-config"),
	}

	// skip scaling-group creation
	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
	}

	ctx.SetDiscoveredState(&DiscoveredState{
		LaunchConfiguration: mockLaunchConfiguration,
		ScalingGroup:        mockScalingGroup,
	})

	err := ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateManagedRoleNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	// Mock role/profile do not exist so they are always created
	iamMock.GetRoleErr = errors.New("not found")
	iamMock.GetInstanceProfileErr = errors.New("not found")

	iamMock.CreateRoleErr = errors.New("some-error")
	err := ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	iamMock.CreateRoleErr = nil

	iamMock.CreateInstanceProfileErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))

	iamMock.WaitUntilInstanceProfileExistsErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	iamMock.WaitUntilInstanceProfileExistsErr = nil
	iamMock.CreateInstanceProfileErr = nil

	iamMock.AddRoleToInstanceProfileErr = awserr.New(iam.ErrCodeNoSuchEntityException, "", errors.New("some-error"))
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	iamMock.AddRoleToInstanceProfileErr = nil

	iamMock.AttachRolePolicyErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	iamMock.AttachRolePolicyErr = nil
}

func TestCreateLaunchConfigNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	ig.GetEKSConfiguration().SetRoleName("some-role")
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")

	ctx.SetDiscoveredState(&DiscoveredState{
		InstanceProfile: &iam.InstanceProfile{
			Arn: aws.String("arn"),
		},
	})

	asgMock.DescribeLaunchConfigurationsErr = errors.New("some-error")
	err := ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.DescribeLaunchConfigurationsErr = nil

	asgMock.CreateLaunchConfigurationErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.CreateLaunchConfigurationErr = nil

	asgMock.DescribeAutoScalingGroupsErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.DescribeAutoScalingGroupsErr = nil

	asgMock.CreateAutoScalingGroupErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}

func TestCreateAutoScalingGroupNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := MockContext(ig, k, w)

	ig.GetEKSConfiguration().SetRoleName("some-role")
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")

	ctx.SetDiscoveredState(&DiscoveredState{
		LaunchConfiguration: &autoscaling.LaunchConfiguration{LaunchConfigurationName: aws.String("launch-config")},
	})

	asgMock.CreateAutoScalingGroupErr = errors.New("some-error")
	err := ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}
