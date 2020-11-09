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
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
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

func NewEksMocker() *MockEksClient {
	mock := &MockEksClient{
		EksCluster: &eks.Cluster{
			CertificateAuthority: &eks.Certificate{
				Data: aws.String(""),
			},
			Endpoint:           aws.String("foo.amazonaws.com"),
			ResourcesVpcConfig: &eks.VpcConfigResponse{},
		},
	}
	return mock
}

func NewEc2Mocker() *MockEc2Client {
	return &MockEc2Client{}
}

func MockAwsWorker(asgClient *MockAutoScalingClient, iamClient *MockIamClient, eksClient *MockEksClient, ec2Client *MockEc2Client) awsprovider.AwsWorker {
	return awsprovider.AwsWorker{
		Ec2Client: ec2Client,
		AsgClient: asgClient,
		IamClient: iamClient,
		EksClient: eksClient,
	}
}

func MockKubernetesClientSet() kubeprovider.KubernetesClientSet {
	return kubeprovider.KubernetesClientSet{
		Kubernetes:  fake.NewSimpleClientset(),
		KubeDynamic: dynamic.NewSimpleDynamicClient(runtime.NewScheme()),
	}
}

func MockContext(instanceGroup *v1alpha1.InstanceGroup, kube kubeprovider.KubernetesClientSet, w awsprovider.AwsWorker) *EksInstanceGroupContext {
	var (
		clusterCa       = "somestring"
		clusterEndpoint = "foo.amazonaws.com"
	)
	input := provisioners.ProvisionerInput{
		AwsWorker:     w,
		Kubernetes:    kube,
		InstanceGroup: instanceGroup,
		Log:           ctrl.Log.WithName("unit-test").WithName("InstanceGroup"),
	}
	context := New(input)

	context.DiscoveredState = &DiscoveredState{
		Provisioned:          false,
		NodesReady:           false,
		ClusterNodes:         nil,
		OwnedScalingGroups:   nil,
		ScalingGroup:         nil,
		LifecycleHooks:       nil,
		ScalingConfiguration: nil,
		IAMRole:              nil,
		AttachedPolicies:     nil,
		InstanceProfile:      nil,
		Publisher:            kubeprovider.EventPublisher{},
		Cluster: &eks.Cluster{
			Endpoint: &clusterEndpoint,
			CertificateAuthority: &eks.Certificate{
				Data: &clusterCa,
			},
		},
		VPCId: "",
	}
	return context
}

func MockInstanceGroup() *v1alpha1.InstanceGroup {
	return &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-group-1",
			Namespace: "instance-manager",
			Annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
			},
		},
		Spec: v1alpha1.InstanceGroupSpec{
			Provisioner: ProvisionerName,
			EKSSpec: &v1alpha1.EKSSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					EksClusterName: "my-cluster",
					SuspendedProcesses: []string{
						"AZRebalance",
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				CRDType:           &v1alpha1.CRDUpdateStrategy{},
				RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
			},
		},
		Status: v1alpha1.InstanceGroupStatus{},
	}
}

func MockWindowsInstanceGroup() *v1alpha1.InstanceGroup {
	return &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-group-1",
			Namespace: "instance-manager",
			Annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				OsFamilyAnnotation:                 OsFamilyWindows,
			},
		},
		Spec: v1alpha1.InstanceGroupSpec{
			Provisioner: ProvisionerName,
			EKSSpec: &v1alpha1.EKSSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					EksClusterName: "my-cluster",
					SuspendedProcesses: []string{
						"AZRebalance",
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				CRDType:           &v1alpha1.CRDUpdateStrategy{},
				RollingUpdateType: &v1alpha1.RollingUpdateStrategy{},
			},
		},
		Status: v1alpha1.InstanceGroupStatus{},
	}
}

func MockBottleRocketInstanceGroup() *v1alpha1.InstanceGroup {
	return &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-group-1",
			Namespace: "instance-manager",
			Annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				OsFamilyAnnotation:                 OsFamilyBottleRocket,
			},
		},
		Spec: v1alpha1.InstanceGroupSpec{
			Provisioner: ProvisionerName,
			EKSSpec: &v1alpha1.EKSSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					EksClusterName: "my-cluster",
					SuspendedProcesses: []string{
						"AZRebalance",
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				CRDType:           &v1alpha1.CRDUpdateStrategy{},
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

func MockAttachedPolicies(policies ...string) []*iam.AttachedPolicy {
	mock := []*iam.AttachedPolicy{}
	for _, p := range policies {
		arn := fmt.Sprintf("%v/%v", awsprovider.IAMPolicyPrefix, p)
		policy := &iam.AttachedPolicy{
			PolicyName: aws.String(p),
			PolicyArn:  aws.String(arn),
		}
		mock = append(mock, policy)
	}
	return mock
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
		VPCZoneIdentifier:       aws.String("subnet-1,subnet-2,subnet-3"),
	}
}

func MockSecurityGroup(id string, withTag bool, name string) *ec2.SecurityGroup {
	sg := &ec2.SecurityGroup{
		GroupId: aws.String(id),
	}
	if withTag {
		sg.Tags = []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(name),
			},
		}
	}
	return sg
}

func MockSubnet(id string, withTag bool, name string) *ec2.Subnet {
	sn := &ec2.Subnet{
		SubnetId: aws.String(id),
	}
	if withTag {
		sn.Tags = []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(name),
			},
		}
	}
	return sn
}

func MockAwsCRDStrategy(spec string) v1alpha1.AwsUpgradeStrategy {
	return v1alpha1.AwsUpgradeStrategy{
		Type: kubeprovider.CRDStrategyName,
		CRDType: &v1alpha1.CRDUpdateStrategy{
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

func MockEnabledMetrics(metrics ...string) []*autoscaling.EnabledMetric {
	mockMetrics := make([]*autoscaling.EnabledMetric, 0)
	for _, m := range metrics {
		metric := &autoscaling.EnabledMetric{
			Granularity: aws.String("1Minute"),
			Metric:      aws.String(m),
		}
		mockMetrics = append(mockMetrics, metric)
	}
	return mockMetrics
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
	EnableMetricsCollectionErr             error
	DisableMetricsCollectionErr            error
	UpdateSuspendProcessesErr              error
	DescribeLifecycleHooksErr              error
	PutLifecycleHookErr                    error
	DeleteLifecycleHookErr                 error
	DeleteLaunchConfigurationCallCount     int
	PutLifecycleHookCallCount              int
	DeleteLifecycleHookCallCount           int
	LaunchConfiguration                    *autoscaling.LaunchConfiguration
	LaunchConfigurations                   []*autoscaling.LaunchConfiguration
	AutoScalingGroup                       *autoscaling.Group
	AutoScalingGroups                      []*autoscaling.Group
	LifecycleHooks                         []*autoscaling.LifecycleHook
}

func (a *MockAutoScalingClient) EnableMetricsCollection(input *autoscaling.EnableMetricsCollectionInput) (*autoscaling.EnableMetricsCollectionOutput, error) {
	return &autoscaling.EnableMetricsCollectionOutput{}, a.EnableMetricsCollectionErr
}

func (a *MockAutoScalingClient) DisableMetricsCollection(input *autoscaling.DisableMetricsCollectionInput) (*autoscaling.DisableMetricsCollectionOutput, error) {
	return &autoscaling.DisableMetricsCollectionOutput{}, a.DisableMetricsCollectionErr
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
	a.DeleteLaunchConfigurationCallCount++
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

func (a *MockAutoScalingClient) DescribeLaunchConfigurationsPages(input *autoscaling.DescribeLaunchConfigurationsInput, callback func(*autoscaling.DescribeLaunchConfigurationsOutput, bool) bool) error {
	page, err := a.DescribeLaunchConfigurations(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (a *MockAutoScalingClient) UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return &autoscaling.UpdateAutoScalingGroupOutput{}, a.UpdateAutoScalingGroupErr
}

func (a *MockAutoScalingClient) SuspendProcesses(input *autoscaling.ScalingProcessQuery) (*autoscaling.SuspendProcessesOutput, error) {
	return &autoscaling.SuspendProcessesOutput{}, a.UpdateSuspendProcessesErr
}

func (a *MockAutoScalingClient) ResumeProcesses(input *autoscaling.ScalingProcessQuery) (*autoscaling.ResumeProcessesOutput, error) {
	return &autoscaling.ResumeProcessesOutput{}, a.UpdateSuspendProcessesErr
}

func (a *MockAutoScalingClient) DescribeLifecycleHooks(input *autoscaling.DescribeLifecycleHooksInput) (*autoscaling.DescribeLifecycleHooksOutput, error) {
	return &autoscaling.DescribeLifecycleHooksOutput{LifecycleHooks: a.LifecycleHooks}, a.DescribeLifecycleHooksErr
}

func (a *MockAutoScalingClient) DeleteLifecycleHook(input *autoscaling.DeleteLifecycleHookInput) (*autoscaling.DeleteLifecycleHookOutput, error) {
	a.DeleteLifecycleHookCallCount++
	return &autoscaling.DeleteLifecycleHookOutput{}, a.DeleteLifecycleHookErr
}

func (a *MockAutoScalingClient) PutLifecycleHook(input *autoscaling.PutLifecycleHookInput) (*autoscaling.PutLifecycleHookOutput, error) {
	a.PutLifecycleHookCallCount++
	return &autoscaling.PutLifecycleHookOutput{}, a.PutLifecycleHookErr
}

type MockEc2Client struct {
	ec2iface.EC2API
	DescribeSubnetsErr        error
	DescribeSecurityGroupsErr error
	Subnets                   []*ec2.Subnet
	SecurityGroups            []*ec2.SecurityGroup
}

func (c *MockEc2Client) DescribeSecurityGroupsPages(input *ec2.DescribeSecurityGroupsInput, callback func(*ec2.DescribeSecurityGroupsOutput, bool) bool) error {
	page, err := c.DescribeSecurityGroups(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeSubnetsPages(input *ec2.DescribeSubnetsInput, callback func(*ec2.DescribeSubnetsOutput, bool) bool) error {
	page, err := c.DescribeSubnets(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeSecurityGroups(input *ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error) {
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: c.SecurityGroups}, c.DescribeSecurityGroupsErr
}

func (c *MockEc2Client) DescribeSubnets(input *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
	return &ec2.DescribeSubnetsOutput{Subnets: c.Subnets}, c.DescribeSubnetsErr
}

type MockEksClient struct {
	eksiface.EKSAPI
	DescribeClusterErr error
	EksCluster         *eks.Cluster
}

func (e *MockEksClient) DescribeCluster(input *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
	return &eks.DescribeClusterOutput{Cluster: e.EksCluster}, e.DescribeClusterErr
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
	AttachRolePolicyCallCount         int
	DetachRolePolicyErr               error
	DetachRolePolicyCallCount         int
	WaitUntilInstanceProfileExistsErr error
	ListAttachedRolePoliciesErr       error
	Role                              *iam.Role
	InstanceProfile                   *iam.InstanceProfile
	AttachedPolicies                  []*iam.AttachedPolicy
}

func (i *MockIamClient) ListAttachedRolePolicies(input *iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error) {
	if i.AttachedPolicies != nil {
		return &iam.ListAttachedRolePoliciesOutput{AttachedPolicies: i.AttachedPolicies}, i.ListAttachedRolePoliciesErr
	}
	return &iam.ListAttachedRolePoliciesOutput{}, i.ListAttachedRolePoliciesErr
}

func (i *MockIamClient) ListAttachedRolePoliciesPages(input *iam.ListAttachedRolePoliciesInput, callback func(*iam.ListAttachedRolePoliciesOutput, bool) bool) error {
	page, err := i.ListAttachedRolePolicies(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
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
	i.AttachRolePolicyCallCount++
	return &iam.AttachRolePolicyOutput{}, i.AttachRolePolicyErr
}

func (i *MockIamClient) DetachRolePolicy(input *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error) {
	i.DetachRolePolicyCallCount++
	return &iam.DetachRolePolicyOutput{}, i.DetachRolePolicyErr
}

func (i *MockIamClient) GetInstanceProfile(input *iam.GetInstanceProfileInput) (*iam.GetInstanceProfileOutput, error) {
	return &iam.GetInstanceProfileOutput{InstanceProfile: i.InstanceProfile}, i.GetInstanceProfileErr
}

func (i *MockIamClient) WaitUntilInstanceProfileExists(input *iam.GetInstanceProfileInput) error {
	return i.WaitUntilInstanceProfileExistsErr
}
