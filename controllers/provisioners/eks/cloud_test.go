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
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestCloudDiscoveryPositive(t *testing.T) {
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
	status := ig.GetStatus()
	configuration := ig.GetEKSConfiguration()

	iamMock.Role = &iam.Role{
		RoleName: aws.String("some-role"),
		Arn:      aws.String("some-arn"),
	}

	iamMock.InstanceProfile = &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
	}

	var (
		clusterName           = "some-cluster"
		resourceName          = "some-instance-group"
		resourceNamespace     = "default"
		launchConfigName      = "some-launch-configuration"
		ownedScalingGroupName = "scaling-group-1"
		vpcId                 = "vpc-1234567890"
		ownershipTag          = MockTagDescription(provisioners.TagClusterName, clusterName)
		nameTag               = MockTagDescription(provisioners.TagInstanceGroupName, resourceName)
		namespaceTag          = MockTagDescription(provisioners.TagInstanceGroupNamespace, resourceNamespace)
		ownedScalingGroup     = MockScalingGroup(ownedScalingGroupName, false, ownershipTag, nameTag, namespaceTag)
	)

	ig.SetName(resourceName)
	ig.SetNamespace(resourceNamespace)
	configuration.SetClusterName(clusterName)

	asgMock.AutoScalingGroups = []*autoscaling.Group{
		ownedScalingGroup,
		MockScalingGroup("scaling-group-2", false, ownershipTag),
		MockScalingGroup("scaling-group-3", false, ownershipTag),
	}

	launchConfig := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String(launchConfigName),
	}
	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{
		launchConfig,
	}

	eksMock.EksCluster = &eks.Cluster{
		ResourcesVpcConfig: &eks.VpcConfigResponse{
			VpcId: aws.String(vpcId),
		},
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	lc := state.ScalingConfiguration.Resource().(*autoscaling.LaunchConfiguration)

	g.Expect(state.GetRole()).To(gomega.Equal(iamMock.Role))
	g.Expect(state.GetInstanceProfile()).To(gomega.Equal(iamMock.InstanceProfile))
	g.Expect(state.GetOwnedScalingGroups()).To(gomega.Equal(asgMock.AutoScalingGroups))
	g.Expect(state.IsProvisioned()).To(gomega.BeTrue())
	g.Expect(state.GetScalingGroup()).To(gomega.Equal(ownedScalingGroup))
	g.Expect(lc).To(gomega.Equal(launchConfig))
	g.Expect(state.ScalingConfiguration.Name()).To(gomega.Equal(launchConfigName))
	g.Expect(state.GetVPCId()).To(gomega.Equal(vpcId))
	g.Expect(status.GetNodesArn()).To(gomega.Equal(aws.StringValue(iamMock.Role.Arn)))
	g.Expect(status.GetActiveScalingGroupName()).To(gomega.Equal(ownedScalingGroupName))
	g.Expect(status.GetActiveLaunchConfigurationName()).To(gomega.Equal(launchConfigName))
	g.Expect(status.GetCurrentMin()).To(gomega.Equal(3))
	g.Expect(status.GetCurrentMax()).To(gomega.Equal(6))
}

func TestCloudDiscoveryWithTemplatePositive(t *testing.T) {
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
	status := ig.GetStatus()
	configuration := ig.GetEKSConfiguration()
	spec := ig.GetEKSSpec()
	spec.Type = v1alpha1.LaunchTemplate
	spotRatio := intstr.FromInt(100)
	configuration.MixedInstancesPolicy = &v1alpha1.MixedInstancesPolicySpec{
		InstancePool: aws.String(v1alpha1.SubFamilyFlexibleInstancePool),
		SpotRatio:    &spotRatio,
	}

	iamMock.Role = &iam.Role{
		RoleName: aws.String("some-role"),
		Arn:      aws.String("some-arn"),
	}

	iamMock.InstanceProfile = &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
	}

	var (
		clusterName           = "some-cluster"
		resourceName          = "some-instance-group"
		resourceNamespace     = "default"
		launchTemplateName    = "some-launch-template"
		ownedScalingGroupName = "scaling-group-1"
		vpcId                 = "vpc-1234567890"
		ownershipTag          = MockTagDescription(provisioners.TagClusterName, clusterName)
		nameTag               = MockTagDescription(provisioners.TagInstanceGroupName, resourceName)
		namespaceTag          = MockTagDescription(provisioners.TagInstanceGroupNamespace, resourceNamespace)
		ownedScalingGroup     = MockScalingGroup(ownedScalingGroupName, true, ownershipTag, nameTag, namespaceTag)
	)

	ig.SetName(resourceName)
	ig.SetNamespace(resourceNamespace)
	configuration.SetClusterName(clusterName)

	asgMock.AutoScalingGroups = []*autoscaling.Group{
		ownedScalingGroup,
		MockScalingGroup("scaling-group-2", true, ownershipTag),
		MockScalingGroup("scaling-group-3", true, ownershipTag),
	}

	launchTemplate := &ec2.LaunchTemplate{
		LaunchTemplateName: aws.String(launchTemplateName),
	}

	ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{
		launchTemplate,
	}

	eksMock.EksCluster = &eks.Cluster{
		ResourcesVpcConfig: &eks.VpcConfigResponse{
			VpcId: aws.String(vpcId),
		},
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	lt := state.ScalingConfiguration.Resource().(*ec2.LaunchTemplate)

	g.Expect(state.GetRole()).To(gomega.Equal(iamMock.Role))
	g.Expect(state.GetInstanceProfile()).To(gomega.Equal(iamMock.InstanceProfile))
	g.Expect(state.GetOwnedScalingGroups()).To(gomega.Equal(asgMock.AutoScalingGroups))
	g.Expect(state.IsProvisioned()).To(gomega.BeTrue())
	g.Expect(state.GetScalingGroup()).To(gomega.Equal(ownedScalingGroup))
	g.Expect(lt).To(gomega.Equal(launchTemplate))
	g.Expect(state.ScalingConfiguration.Name()).To(gomega.Equal(launchTemplateName))
	g.Expect(state.GetVPCId()).To(gomega.Equal(vpcId))
	g.Expect(status.GetNodesArn()).To(gomega.Equal(aws.StringValue(iamMock.Role.Arn)))
	g.Expect(status.GetActiveScalingGroupName()).To(gomega.Equal(ownedScalingGroupName))
	g.Expect(status.GetActiveLaunchTemplateName()).To(gomega.Equal(launchTemplateName))
	g.Expect(status.GetCurrentMin()).To(gomega.Equal(3))
	g.Expect(status.GetCurrentMax()).To(gomega.Equal(6))
}

func TestDeriveSubFamilyFlexiblePool(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	mockOfferings := MockTypeOffering("us-west-2", "z5.large", "z5.xlarge", "z5.2xlarge", "x4.large", "x4a.large", "x4.xlarge", "x3.2xlarge", "a5i.large", "a5g.large", "a5a.large")

	mockInfo := MockTypeInfo(
		MockInstanceTypeInfo{"z5.large", 1, 100, "amd64"},
		MockInstanceTypeInfo{"z5.xlarge", 1, 100, "amd64"},
		MockInstanceTypeInfo{"z5.2xlarge", 1, 100, "amd64"},
		MockInstanceTypeInfo{"x4.large", 2, 100, "amd64"},
		MockInstanceTypeInfo{"x4a.large", 2, 100, "amd64"},
		MockInstanceTypeInfo{"x4.xlarge", 4, 200, "amd64"},
		MockInstanceTypeInfo{"x3.2xlarge", 6, 400, "amd64"},
		MockInstanceTypeInfo{"a5i.large", 1, 100, "amd64"},
		MockInstanceTypeInfo{"a5g.large", 1, 100, "arm64"},
		MockInstanceTypeInfo{"a5a.large", 1, 100, "amd64"},

	)

	expectedPool := make(map[string][]InstanceSpec, 0)
	expectedPool["z5.large"] = []InstanceSpec{
		{
			Type:   "z5.large",
			Weight: "1",
		},
		{
			Type:   "z5.xlarge",
			Weight: "1",
		},
		{
			Type:   "z5.2xlarge",
			Weight: "1",
		},
	}
	expectedPool["z5.xlarge"] = []InstanceSpec{
		{
			Type:   "z5.xlarge",
			Weight: "1",
		},
		{
			Type:   "z5.large",
			Weight: "1",
		},
		{
			Type:   "z5.2xlarge",
			Weight: "1",
		},
	}
	expectedPool["z5.2xlarge"] = []InstanceSpec{
		{
			Type:   "z5.2xlarge",
			Weight: "1",
		},
		{
			Type:   "z5.large",
			Weight: "1",
		},
		{
			Type:   "z5.xlarge",
			Weight: "1",
		},
	}
	expectedPool["x4.large"] = []InstanceSpec{
		{
			Type:   "x4.large",
			Weight: "1",
		},
		{
			Type:   "x4a.large",
			Weight: "1",
		},
	}
	expectedPool["x4a.large"] = []InstanceSpec{
		{
			Type:   "x4a.large",
			Weight: "1",
		},
		{
			Type:   "x4.large",
			Weight: "1",
		},
	}
	expectedPool["x4.xlarge"] = []InstanceSpec{
		{
			Type:   "x4.xlarge",
			Weight: "1",
		},
	}
	expectedPool["x3.2xlarge"] = []InstanceSpec{
		{
			Type:   "x3.2xlarge",
			Weight: "1",
		},
	}
	expectedPool["a5g.large"] = []InstanceSpec{
		{
			Type:   "a5g.large",
			Weight: "1",
		},
	}
	expectedPool["a5a.large"] = []InstanceSpec{
		{
			Type:   "a5a.large",
			Weight: "1",
		},
		{
			Type:   "a5i.large",
			Weight: "1",
		},
	}
	expectedPool["a5i.large"] = []InstanceSpec{
		{
			Type:   "a5i.large",
			Weight: "1",
		},
		{
			Type:   "a5a.large",
			Weight: "1",
		},
	}

	p := subFamilyFlexiblePool(mockOfferings, mockInfo)
	g.Expect(p).To(gomega.Equal(expectedPool))
}

func TestCloudDiscoveryExistingRole(t *testing.T) {
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
	configuration := ig.GetEKSConfiguration()
	state := ctx.GetDiscoveredState()

	iamMock.Role = &iam.Role{
		RoleName: aws.String("some-role"),
		Arn:      aws.String("some-arn"),
	}

	iamMock.InstanceProfile = &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
	}

	configuration.SetRoleName("some-role")
	configuration.SetInstanceProfileName("some-profile")

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(state.GetRole()).To(gomega.Equal(iamMock.Role))
	g.Expect(state.GetInstanceProfile()).To(gomega.Equal(iamMock.InstanceProfile))
}

func TestCloudDiscoverySpotPrice(t *testing.T) {
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
	status := ig.GetStatus()
	configuration := ig.GetEKSConfiguration()

	iamMock.Role = &iam.Role{
		RoleName: aws.String("some-role"),
		Arn:      aws.String("some-arn"),
	}

	iamMock.InstanceProfile = &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
	}

	var (
		clusterName           = "some-cluster"
		resourceName          = "some-instance-group"
		resourceNamespace     = "default"
		ownedScalingGroupName = "scaling-group-1"
		ownershipTag          = MockTagDescription(provisioners.TagClusterName, clusterName)
		nameTag               = MockTagDescription(provisioners.TagInstanceGroupName, resourceName)
		namespaceTag          = MockTagDescription(provisioners.TagInstanceGroupNamespace, resourceNamespace)
	)

	ig.SetName(resourceName)
	ig.SetNamespace(resourceNamespace)
	configuration.SetClusterName(clusterName)
	mockAsg := []*autoscaling.Group{
		MockScalingGroup(ownedScalingGroupName, false, ownershipTag, nameTag, namespaceTag),
	}
	asgMock.AutoScalingGroups = mockAsg

	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{
		{
			LaunchConfigurationName: aws.String("some-launch-configuration"),
		},
	}

	configuration.SetSpotPrice("0.67")

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status.GetLifecycle()).To(gomega.Equal("spot"))

	status.SetUsingSpotRecommendation(true)
	_, err = k.Kubernetes.CoreV1().Events("").Create(context.Background(), MockSpotEvent("1", ownedScalingGroupName, "0.80", true, time.Now()), metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// recommendation should not be used if nodes are not provisioned yet
	asgMock.AutoScalingGroups = []*autoscaling.Group{}
	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.Equal("0.67"))

	asgMock.AutoScalingGroups = mockAsg
	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.Equal("0.80"))

	_, err = k.Kubernetes.CoreV1().Events("").Create(context.Background(), MockSpotEvent("2", ownedScalingGroupName, "0.90", false, time.Now().Add(time.Minute*time.Duration(3))), metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.BeEmpty())
}

func TestLaunchConfigDeletion(t *testing.T) {
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
	configuration := ig.GetEKSConfiguration()

	iamMock.Role = &iam.Role{
		RoleName: aws.String("some-role"),
		Arn:      aws.String("some-arn"),
	}

	iamMock.InstanceProfile = &iam.InstanceProfile{
		InstanceProfileName: aws.String("some-profile"),
	}

	var (
		clusterName           = "some-cluster"
		resourceName          = "some-instance-group"
		resourceNamespace     = "default"
		ownedScalingGroupName = "scaling-group-1"
		ownershipTag          = MockTagDescription(provisioners.TagClusterName, clusterName)
		nameTag               = MockTagDescription(provisioners.TagInstanceGroupName, resourceName)
		namespaceTag          = MockTagDescription(provisioners.TagInstanceGroupNamespace, resourceNamespace)
	)

	ig.SetName(resourceName)
	ig.SetNamespace(resourceNamespace)
	configuration.SetClusterName(clusterName)

	asgMock.AutoScalingGroups = []*autoscaling.Group{
		MockScalingGroup(ownedScalingGroupName, false, ownershipTag, nameTag, namespaceTag),
	}

	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{
		{
			LaunchConfigurationName: aws.String(fmt.Sprintf("%v-123456", ctx.ResourcePrefix)),
			CreatedTime:             aws.Time(time.Now()),
		},
		{
			LaunchConfigurationName: aws.String(fmt.Sprintf("%v-123457", ctx.ResourcePrefix)),
			CreatedTime:             aws.Time(time.Now().Add(time.Duration(-1) * time.Minute)),
		},
		{
			LaunchConfigurationName: aws.String(fmt.Sprintf("%v-123458", ctx.ResourcePrefix)),
			CreatedTime:             aws.Time(time.Now().Add(time.Duration(-3) * time.Minute)),
		},
		{
			LaunchConfigurationName: aws.String(fmt.Sprintf("%v-123459", ctx.ResourcePrefix)),
			CreatedTime:             aws.Time(time.Now().Add(time.Duration(-5) * time.Minute)),
		},
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(asgMock.DeleteLaunchConfigurationCallCount).To(gomega.Equal(uint(2)))
}
