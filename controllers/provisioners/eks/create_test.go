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
	"fmt"
	"testing"

	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/eks"
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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)
	state := ctx.GetDiscoveredState()
	state.SetCluster(&eks.Cluster{
		Version: aws.String("1.15"),
		CertificateAuthority: &eks.Certificate{
			Data: aws.String("clusterca"),
		},
		Endpoint: aws.String("foo.amazonaws.com"),
	})
	state.Publisher.Client = k.Kubernetes
	state.ScalingConfiguration = &scaling.LaunchConfiguration{
		AwsWorker: w,
	}

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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	iamMock.Role = &iam.Role{
		Arn:      aws.String("some-arn"),
		RoleName: aws.String("some-role"),
	}

	lcPrefix := fmt.Sprintf("my-cluster-%v-%v", ig.GetNamespace(), ig.GetName())
	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(ig.GetStatus().GetActiveLaunchConfigurationName()).To(gomega.HavePrefix(lcPrefix))
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateScalingGroupPositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	// skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")

	asgName := fmt.Sprintf("my-cluster-%v-%v", ig.GetNamespace(), ig.GetName())
	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String(asgName),
	}
	asgMock.AutoScalingGroups = []*autoscaling.Group{mockScalingGroup}
	asgMock.AutoScalingGroup = mockScalingGroup

	lc, err := scaling.NewLaunchConfiguration(ig.NamespacedName(), w, &scaling.DiscoverConfigurationInput{
		ScalingGroup: mockScalingGroup,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		Cluster: &eks.Cluster{
			CertificateAuthority: &eks.Certificate{
				Data: aws.String("foo"),
			},
			Endpoint: aws.String("foo.amazonaws.com"),
		},
		ScalingConfiguration: lc,
	})

	err = ctx.Create()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModified))
}

func TestCreateNoOp(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	// skip role creation
	ig.GetEKSConfiguration().SetInstanceProfileName("some-profile")
	ig.GetEKSConfiguration().SetRoleName("some-role")

	mockLaunchConfiguration := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("some-launch-config"),
	}
	lc := &scaling.LaunchConfiguration{
		TargetResource: mockLaunchConfiguration,
	}

	// skip scaling-group creation
	mockScalingGroup := &autoscaling.Group{
		AutoScalingGroupName: aws.String("some-scaling-group"),
	}

	ctx.SetDiscoveredState(&DiscoveredState{
		Cluster: &eks.Cluster{
			CertificateAuthority: &eks.Certificate{
				Data: aws.String(""),
			},
			Endpoint:           aws.String("foo.amazonaws.com"),
			ResourcesVpcConfig: &eks.VpcConfigResponse{},
		},
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup:         mockScalingGroup,
		ScalingConfiguration: lc,
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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	iamMock.Role = &iam.Role{
		Arn:      aws.String("some-arn"),
		RoleName: aws.String("some-role"),
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	asgMock.CreateLaunchConfigurationErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
	asgMock.CreateLaunchConfigurationErr = nil

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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	iamMock.Role = &iam.Role{
		Arn:      aws.String("some-arn"),
		RoleName: aws.String("some-role"),
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	asgMock.CreateAutoScalingGroupErr = errors.New("some-error")
	err = ctx.Create()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileModifying))
}
