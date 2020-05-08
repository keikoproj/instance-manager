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
	"flag"
	"time"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	loggingEnabled bool
)

func init() {
	flag.BoolVar(&loggingEnabled, "logging-enabled", false, "Enable Logging")
	awsprovider.DefaultInstanceProfilePropagationDelay = time.Millisecond * 1
	awsprovider.DefaultWaiterDuration = time.Millisecond * 1
	awsprovider.DefaultWaiterRetries = 1
}

func NewAutoScalingMocker() *MockAutoScalingClient {
	return &MockAutoScalingClient{}
}

func NewIamMocker() *MockIamClient {
	return &MockIamClient{}
}

func MockAwsWorker(asgClient *MockAutoScalingClient, iamClient *MockIamClient) awsprovider.AwsWorker {
	return awsprovider.AwsWorker{
		AsgClient: asgClient,
		IamClient: iamClient,
	}
}

func MockKubernetesClientSet() common.KubernetesClientSet {
	return common.KubernetesClientSet{
		Kubernetes:  fake.NewSimpleClientset(),
		KubeDynamic: dynamic.NewSimpleDynamicClient(runtime.NewScheme()),
	}
}

func MockInstanceGroup() *v1alpha1.InstanceGroup {
	return &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-group-1",
			Namespace: "instance-manager",
		},
		Spec: v1alpha1.InstanceGroupSpec{
			Provisioner: ProvisionerName,
			EKSSpec: &v1alpha1.EKSSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					EksClusterName: "my-cluster",
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				CRDType:           &v1alpha1.CRDUpgradeStrategy{},
				RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
			},
		},
		Status: v1alpha1.InstanceGroupStatus{},
	}
}

func MockLaunchConfigFromInput(input *autoscaling.CreateLaunchConfigurationInput) *autoscaling.LaunchConfiguration {
	return &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: input.LaunchConfigurationName,
		ImageId:                 input.ImageId,
		InstanceType:            input.InstanceType,
		IamInstanceProfile:      input.IamInstanceProfile,
		SpotPrice:               input.SpotPrice,
		SecurityGroups:          input.SecurityGroups,
		KeyName:                 input.KeyName,
		UserData:                input.UserData,
		BlockDeviceMappings:     input.BlockDeviceMappings,
	}
}

type MockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	DescribeLaunchConfigurationsErr error
	DescribeAutoScalingGroupsErr    error
	CreateLaunchConfigurationErr    error
	DeleteLaunchConfigurationErr    error
	CreateAutoScalingGroupErr       error
	UpdateAutoScalingGroupErr       error
	DeleteAutoScalingGroupErr       error
	LaunchConfiguration             *autoscaling.LaunchConfiguration
	LaunchConfigurations            []*autoscaling.LaunchConfiguration
	AutoScalingGroup                *autoscaling.Group
	AutoScalingGroups               []*autoscaling.Group
}

func (a *MockAutoScalingClient) CreateOrUpdateTags(input *autoscaling.CreateOrUpdateTagsInput) (*autoscaling.CreateOrUpdateTagsOutput, error) {
	return &autoscaling.CreateOrUpdateTagsOutput{}, nil
}

func (a *MockAutoScalingClient) DeleteTags(input *autoscaling.DeleteTagsInput) (*autoscaling.DeleteTagsOutput, error) {
	return &autoscaling.DeleteTagsOutput{}, nil
}

func (a *MockAutoScalingClient) CreateLaunchConfiguration(input *autoscaling.CreateLaunchConfigurationInput) (*autoscaling.CreateLaunchConfigurationOutput, error) {
	return &autoscaling.CreateLaunchConfigurationOutput{}, a.CreateLaunchConfigurationErr
}

func (a *MockAutoScalingClient) DescribeLaunchConfigurations(input *autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	return &autoscaling.DescribeLaunchConfigurationsOutput{LaunchConfigurations: a.LaunchConfigurations}, a.DescribeLaunchConfigurationsErr
}

func (a *MockAutoScalingClient) DeleteLaunchConfiguration(input *autoscaling.DeleteLaunchConfigurationInput) (*autoscaling.DeleteLaunchConfigurationOutput, error) {
	return &autoscaling.DeleteLaunchConfigurationOutput{}, a.DeleteLaunchConfigurationErr
}

func (a *MockAutoScalingClient) CreateAutoScalingGroup(input *autoscaling.CreateAutoScalingGroupInput) (*autoscaling.CreateAutoScalingGroupOutput, error) {
	return &autoscaling.CreateAutoScalingGroupOutput{}, a.CreateAutoScalingGroupErr
}

func (a *MockAutoScalingClient) DeleteAutoScalingGroup(input *autoscaling.DeleteAutoScalingGroupInput) (*autoscaling.DeleteAutoScalingGroupOutput, error) {
	return &autoscaling.DeleteAutoScalingGroupOutput{}, a.DeleteAutoScalingGroupErr
}

func (a *MockAutoScalingClient) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: a.AutoScalingGroups}, a.DescribeAutoScalingGroupsErr
}

func (a *MockAutoScalingClient) UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return &autoscaling.UpdateAutoScalingGroupOutput{}, a.UpdateAutoScalingGroupErr
}

type MockIamClient struct {
	iamiface.IAMAPI
	CreateRoleErr                     error
	GetRoleErr                        error
	DeleteRoleErr                     error
	CreateInstanceProfileErr          error
	GetInstanceProfileErr             error
	DeleteInstanceProfileErr          error
	AddRoleToInstanceProfileErr       error
	RemoveRoleFromInstanceProfileErr  error
	AttachRolePolicyErr               error
	DetachRolePolicyErr               error
	WaitUntilInstanceProfileExistsErr error
	Role                              *iam.Role
	InstanceProfile                   *iam.InstanceProfile
}

func (i *MockIamClient) CreateRole(input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	if i.Role != nil {
		return &iam.CreateRoleOutput{Role: i.Role}, i.CreateRoleErr
	}
	return &iam.CreateRoleOutput{}, i.CreateRoleErr
}

func (i *MockIamClient) GetRole(input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	return &iam.GetRoleOutput{Role: i.Role}, i.GetRoleErr
}

func (i *MockIamClient) DeleteRole(input *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error) {
	return &iam.DeleteRoleOutput{}, i.DeleteRoleErr
}

func (i *MockIamClient) CreateInstanceProfile(input *iam.CreateInstanceProfileInput) (*iam.CreateInstanceProfileOutput, error) {
	if i.InstanceProfile != nil {
		return &iam.CreateInstanceProfileOutput{InstanceProfile: i.InstanceProfile}, i.CreateInstanceProfileErr
	}
	return &iam.CreateInstanceProfileOutput{}, i.CreateInstanceProfileErr
}

func (i *MockIamClient) DeleteInstanceProfile(input *iam.DeleteInstanceProfileInput) (*iam.DeleteInstanceProfileOutput, error) {
	return &iam.DeleteInstanceProfileOutput{}, i.DeleteInstanceProfileErr
}

func (i *MockIamClient) AddRoleToInstanceProfile(input *iam.AddRoleToInstanceProfileInput) (*iam.AddRoleToInstanceProfileOutput, error) {
	return &iam.AddRoleToInstanceProfileOutput{}, i.AddRoleToInstanceProfileErr
}

func (i *MockIamClient) RemoveRoleFromInstanceProfile(input *iam.RemoveRoleFromInstanceProfileInput) (*iam.RemoveRoleFromInstanceProfileOutput, error) {
	return &iam.RemoveRoleFromInstanceProfileOutput{}, i.RemoveRoleFromInstanceProfileErr
}

func (i *MockIamClient) AttachRolePolicy(input *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error) {
	return &iam.AttachRolePolicyOutput{}, i.AttachRolePolicyErr
}

func (i *MockIamClient) DetachRolePolicy(input *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error) {
	return &iam.DetachRolePolicyOutput{}, i.DetachRolePolicyErr
}

func (i *MockIamClient) GetInstanceProfile(input *iam.GetInstanceProfileInput) (*iam.GetInstanceProfileOutput, error) {
	return &iam.GetInstanceProfileOutput{InstanceProfile: i.InstanceProfile}, i.GetInstanceProfileErr
}

func (i *MockIamClient) WaitUntilInstanceProfileExists(input *iam.GetInstanceProfileInput) error {
	return i.WaitUntilInstanceProfileExistsErr
}
