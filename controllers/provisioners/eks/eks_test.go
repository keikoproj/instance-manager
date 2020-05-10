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
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
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

func MockKubernetesClientSet() kubeprovider.KubernetesClientSet {
	return kubeprovider.KubernetesClientSet{
		Kubernetes:  fake.NewSimpleClientset(),
		KubeDynamic: dynamic.NewSimpleDynamicClient(runtime.NewScheme()),
	}
}

func MockContext(instanceGroup *v1alpha1.InstanceGroup, kube kubeprovider.KubernetesClientSet, w awsprovider.AwsWorker) *EksInstanceGroupContext {
	input := provisioners.ProvisionerInput{
		AwsWorker:     w,
		Kubernetes:    kube,
		InstanceGroup: instanceGroup,
		Log:           ctrl.Log.WithName("unit-test").WithName("InstanceGroup"),
	}
	return New(input)
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

func MockCustomResourceSpec() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "instancemgr.keikoproj.io/v1alpha1",
			"kind":       "Dog",
			"metadata": map[string]interface{}{
				"creationTimestamp": nil,
				"name":              "captain",
				"namespace":         "default",
			},
			"spec": map[string]interface{}{
				"hostname": "example.com",
			},
			"status": map[string]interface{}{},
		},
	}
}

func MockCustomResourceDefinition() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1beta1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "dogs.instancemgr.keikoproj.io",
			},
			"spec": map[string]interface{}{
				"group":   "instancemgr.keikoproj.io",
				"version": "v1alpha1",
			},
		},
	}
}

func MockTagDescription(key, value string) *autoscaling.TagDescription {
	return &autoscaling.TagDescription{
		Key:   aws.String(key),
		Value: aws.String(value),
	}
}

func MockScalingGroup(name string, t ...*autoscaling.TagDescription) *autoscaling.Group {
	return &autoscaling.Group{
		LaunchConfigurationName: aws.String("some-launch-configuration"),
		AutoScalingGroupName:    aws.String(name),
		Tags:                    t,
		MinSize:                 aws.Int64(3),
		MaxSize:                 aws.Int64(6),
	}
}

func MockAwsCRDStrategy(spec string) v1alpha1.AwsUpgradeStrategy {
	return v1alpha1.AwsUpgradeStrategy{
		Type: kubeprovider.CRDStrategyName,
		CRDType: &v1alpha1.CRDUpgradeStrategy{
			Spec:                spec,
			CRDName:             "dogs",
			StatusJSONPath:      ".status.dogStatus",
			StatusSuccessString: "woof",
			StatusFailureString: "grr",
		},
	}
}

func MockAwsRollingUpdateStrategy(maxUnavailable *intstr.IntOrString) v1alpha1.AwsUpgradeStrategy {
	return v1alpha1.AwsUpgradeStrategy{
		Type: kubeprovider.RollingUpdateStrategyName,
		RollingUpdateType: &v1alpha1.RollingUpdateStrategy{
			MaxUnavailable: maxUnavailable,
		},
	}
}

func MockScalingInstances(nonUpdatable, updatable int) []*autoscaling.Instance {
	instances := []*autoscaling.Instance{}
	for i := 0; i < nonUpdatable; i++ {
		instances = append(instances, &autoscaling.Instance{
			LaunchConfigurationName: aws.String("some-launch-config"),
			InstanceId:              aws.String(fmt.Sprintf("i-00000000%v", i)),
		})
	}

	for i := 0; i < updatable; i++ {
		instances = append(instances, &autoscaling.Instance{
			LaunchConfigurationName: aws.String(""),
			InstanceId:              aws.String(fmt.Sprintf("i-10000000%v", i)),
		})
	}

	return instances
}

func MockNode(id string, status corev1.ConditionStatus) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("node-%v", id),
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("aws:///us-west-2a/%v", id),
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: status,
				},
			},
		},
	}
}

func MockSpotEvent(id, scalingGroup, price string, useSpot bool, ts time.Time) *corev1.Event {
	message := fmt.Sprintf(`{"apiVersion":"v1alpha1","spotPrice":"%v", "useSpot": %t}`, price, useSpot)
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("spot-manager-%v.000000000000000", id),
		},
		LastTimestamp: metav1.Time{Time: ts},
		Reason:        "SpotRecommendationGiven",
		Message:       message,
		Type:          "Normal",
		InvolvedObject: corev1.ObjectReference{
			Namespace: "kube-system",
			Name:      scalingGroup,
		},
	}
	return event
}

type MockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	DescribeLaunchConfigurationsErr        error
	DescribeAutoScalingGroupsErr           error
	CreateLaunchConfigurationErr           error
	DeleteLaunchConfigurationErr           error
	CreateAutoScalingGroupErr              error
	UpdateAutoScalingGroupErr              error
	DeleteAutoScalingGroupErr              error
	TerminateInstanceInAutoScalingGroupErr error
	LaunchConfiguration                    *autoscaling.LaunchConfiguration
	LaunchConfigurations                   []*autoscaling.LaunchConfiguration
	AutoScalingGroup                       *autoscaling.Group
	AutoScalingGroups                      []*autoscaling.Group
}

func (a *MockAutoScalingClient) TerminateInstanceInAutoScalingGroup(input *autoscaling.TerminateInstanceInAutoScalingGroupInput) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error) {
	return &autoscaling.TerminateInstanceInAutoScalingGroupOutput{}, a.TerminateInstanceInAutoScalingGroupErr
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

func (a *MockAutoScalingClient) DescribeAutoScalingGroupsPages(input *autoscaling.DescribeAutoScalingGroupsInput, callback func(*autoscaling.DescribeAutoScalingGroupsOutput, bool) bool) error {
	page, err := a.DescribeAutoScalingGroups(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
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
