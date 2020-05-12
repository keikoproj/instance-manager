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
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/onsi/gomega"
)

func TestCloudDiscoveryPositive(t *testing.T) {
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
		ownershipTag          = MockTagDescription(provisioners.TagClusterName, clusterName)
		nameTag               = MockTagDescription(provisioners.TagInstanceGroupName, resourceName)
		namespaceTag          = MockTagDescription(provisioners.TagInstanceGroupNamespace, resourceNamespace)
		ownedScalingGroup     = MockScalingGroup(ownedScalingGroupName, ownershipTag, nameTag, namespaceTag)
	)

	ig.SetName(resourceName)
	ig.SetNamespace(resourceNamespace)
	configuration.SetClusterName(clusterName)

	asgMock.AutoScalingGroups = []*autoscaling.Group{
		ownedScalingGroup,
		MockScalingGroup("scaling-group-2", ownershipTag),
		MockScalingGroup("scaling-group-3", ownershipTag),
	}

	launchConfig := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String(launchConfigName),
	}
	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{
		launchConfig,
	}

	err := ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(state.GetRole()).To(gomega.Equal(iamMock.Role))
	g.Expect(state.GetInstanceProfile()).To(gomega.Equal(iamMock.InstanceProfile))
	g.Expect(state.GetOwnedScalingGroups()).To(gomega.Equal(asgMock.AutoScalingGroups))
	g.Expect(state.IsProvisioned()).To(gomega.BeTrue())
	g.Expect(state.GetScalingGroup()).To(gomega.Equal(ownedScalingGroup))
	g.Expect(state.GetLaunchConfiguration()).To(gomega.Equal(launchConfig))
	g.Expect(state.GetActiveLaunchConfigurationName()).To(gomega.Equal(launchConfigName))
	g.Expect(status.GetNodesArn()).To(gomega.Equal(aws.StringValue(iamMock.Role.Arn)))
	g.Expect(status.GetActiveScalingGroupName()).To(gomega.Equal(ownedScalingGroupName))
	g.Expect(status.GetActiveLaunchConfigurationName()).To(gomega.Equal(launchConfigName))
	g.Expect(status.GetCurrentMin()).To(gomega.Equal(3))
	g.Expect(status.GetCurrentMax()).To(gomega.Equal(6))
}

func TestCloudDiscoveryExistingRole(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
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
	)

	w := MockAwsWorker(asgMock, iamMock)
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

	asgMock.AutoScalingGroups = []*autoscaling.Group{
		MockScalingGroup(ownedScalingGroupName, ownershipTag, nameTag, namespaceTag),
	}

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
	_, err = k.Kubernetes.CoreV1().Events("").Create(MockSpotEvent("1", ownedScalingGroupName, "0.80", true, time.Now()))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// recommendation should not be used if nodes are not ready
	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.Equal("0.67"))

	// recommendation should be accepted if nodes are ready
	status.Conditions = append(status.Conditions, v1alpha1.InstanceGroupCondition{
		Type:   v1alpha1.NodesReady,
		Status: corev1.ConditionTrue,
	})

	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.Equal("0.80"))

	_, err = k.Kubernetes.CoreV1().Events("").Create(MockSpotEvent("2", ownedScalingGroupName, "0.90", false, time.Now().Add(time.Minute*time.Duration(3))))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.CloudDiscovery()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configuration.GetSpotPrice()).To(gomega.BeEmpty())
}
