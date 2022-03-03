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
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
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
		EksCluster: MockEksCluster("1.18"),
	}
	return mock
}

func NewEc2Mocker() *MockEc2Client {
	return &MockEc2Client{}
}

func NewSsmMocker() *MockSsmClient {
	return &MockSsmClient{}
}

func MockAwsWorker(asgClient *MockAutoScalingClient, iamClient *MockIamClient, eksClient *MockEksClient, ec2Client *MockEc2Client, ssmClient *MockSsmClient) awsprovider.AwsWorker {
	return awsprovider.AwsWorker{
		Ec2Client: ec2Client,
		AsgClient: asgClient,
		IamClient: iamClient,
		EksClient: eksClient,
		SsmClient: ssmClient,
	}
}

func MockEksCluster(version string) *eks.Cluster {
	return &eks.Cluster{
		CertificateAuthority: &eks.Certificate{
			Data: aws.String("dGVzdA=="),
		},
		Endpoint:           aws.String("foo.amazonaws.com"),
		ResourcesVpcConfig: &eks.VpcConfigResponse{},
		KubernetesNetworkConfig: &eks.KubernetesNetworkConfigResponse{
			ServiceIpv4Cidr: aws.String("172.20.0.0/16"),
		},
		Version: &version,
	}
}

func MockWarmPoolSpec(maxSize, minSize int64) *v1alpha1.WarmPoolSpec {
	return &v1alpha1.WarmPoolSpec{
		MaxSize: maxSize,
		MinSize: minSize,
	}
}

func MockWarmPool(maxSize, minSize int64, status string) *autoscaling.WarmPoolConfiguration {
	return &autoscaling.WarmPoolConfiguration{
		MaxGroupPreparedCapacity: aws.Int64(maxSize),
		MinSize:                  aws.Int64(minSize),
		Status:                   aws.String(status),
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
		Metrics:       common.NewMetricsCollector(),
	}
	context := New(input)

	context.DiscoveredState = &DiscoveredState{
		Publisher: kubeprovider.EventPublisher{},
		Cluster:   MockEksCluster("1.18"),
		VPCId:     "",
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
				Type:    "LaunchConfiguration",
				MaxSize: 3,
				MinSize: 1,
				EKSConfiguration: &v1alpha1.EKSConfiguration{
					Image:          "ami-123456789012",
					EksClusterName: "my-cluster",
					InstanceType:   "m5.large",
					SuspendedProcesses: []string{
						"AZRebalance",
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				CRDType: &v1alpha1.CRDUpdateStrategy{
					MaxRetries: &v1alpha1.DefaultCRDStrategyMaxRetries,
				},
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

func MockTemplateOverrides(weight string, types ...string) []*autoscaling.LaunchTemplateOverrides {
	overrides := make([]*autoscaling.LaunchTemplateOverrides, 0)
	for _, t := range types {
		overrides = append(overrides, &autoscaling.LaunchTemplateOverrides{
			InstanceType:     aws.String(t),
			WeightedCapacity: aws.String(weight),
		})
	}
	return overrides
}

func MockScalingGroup(name string, withTemplate bool, t ...*autoscaling.TagDescription) *autoscaling.Group {
	asg := &autoscaling.Group{
		AutoScalingGroupName: aws.String(name),
		Tags:                 t,
		MinSize:              aws.Int64(3),
		MaxSize:              aws.Int64(6),
		VPCZoneIdentifier:    aws.String("subnet-1,subnet-2,subnet-3"),
		Instances: []*autoscaling.Instance{
			{
				InstanceType: aws.String("m5.xlarge"),
			},
		},
	}

	if withTemplate {
		asg.LaunchTemplate = &autoscaling.LaunchTemplateSpecification{
			LaunchTemplateName: aws.String("some-launch-template"),
		}
	} else {
		asg.LaunchConfigurationName = aws.String("some-launch-configuration")
	}
	return asg
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

func MockTypeOffering(region string, types ...string) []*ec2.InstanceTypeOffering {
	out := make([]*ec2.InstanceTypeOffering, 0)
	for _, t := range types {
		out = append(out, &ec2.InstanceTypeOffering{
			InstanceType: aws.String(t),
			Location:     aws.String(region),
			LocationType: aws.String("region"),
		})
	}
	return out
}

type MockInstanceTypeInfo struct {
	InstanceType string
	VCpus        int64
	MemoryMib    int64
	Arch         string
}

func MockTypeInfo(types ...MockInstanceTypeInfo) []*ec2.InstanceTypeInfo {
	out := make([]*ec2.InstanceTypeInfo, 0)
	for _, t := range types {
		out = append(out, &ec2.InstanceTypeInfo{
			InstanceType: aws.String(t.InstanceType),
			VCpuInfo: &ec2.VCpuInfo{
				DefaultVCpus: aws.Int64(t.VCpus),
			},
			ProcessorInfo: &ec2.ProcessorInfo{
				SupportedArchitectures: []*string{aws.String(t.Arch)},
			},
			MemoryInfo: &ec2.MemoryInfo{
				SizeInMiB: aws.Int64(t.MemoryMib),
			},
		})
	}
	return out
}

func MockAwsCRDStrategy(spec string) v1alpha1.AwsUpgradeStrategy {
	infiniteRetries := -1
	return v1alpha1.AwsUpgradeStrategy{
		Type: kubeprovider.CRDStrategyName,
		CRDType: &v1alpha1.CRDUpdateStrategy{
			Spec:                spec,
			CRDName:             "dogs",
			StatusJSONPath:      ".status.dogStatus",
			StatusSuccessString: "woof",
			StatusFailureString: "grr",
			MaxRetries:          &infiniteRetries,
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
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("some-launch-template"),
				Version:            aws.String("1"),
			},
			InstanceId: aws.String(fmt.Sprintf("i-00000000%v", i)),
		})
	}

	for i := 0; i < updatable; i++ {
		instances = append(instances, &autoscaling.Instance{
			LaunchConfigurationName: aws.String(""),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("some-launch-template"),
				Version:            aws.String("0"),
			},
			InstanceId: aws.String(fmt.Sprintf("i-10000000%v", i)),
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
	DescribeWarmPoolErr                    error
	DeleteWarmPoolErr                      error
	PutWarmPoolErr                         error
	DeleteLaunchConfigurationCallCount     uint
	PutLifecycleHookCallCount              uint
	DeleteLifecycleHookCallCount           uint
	PutWarmPoolCallCount                   uint
	DeleteWarmPoolCallCount                uint
	DescribeWarmPoolCallCount              uint
	LaunchConfiguration                    *autoscaling.LaunchConfiguration
	LaunchConfigurations                   []*autoscaling.LaunchConfiguration
	AutoScalingGroup                       *autoscaling.Group
	AutoScalingGroups                      []*autoscaling.Group
	WarmPoolInstances                      []*autoscaling.Instance
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

func (a *MockAutoScalingClient) DescribeWarmPool(input *autoscaling.DescribeWarmPoolInput) (*autoscaling.DescribeWarmPoolOutput, error) {
	a.DescribeWarmPoolCallCount++
	return &autoscaling.DescribeWarmPoolOutput{Instances: a.WarmPoolInstances}, a.DescribeWarmPoolErr
}

func (a *MockAutoScalingClient) DeleteWarmPool(input *autoscaling.DeleteWarmPoolInput) (*autoscaling.DeleteWarmPoolOutput, error) {
	a.DeleteWarmPoolCallCount++
	return &autoscaling.DeleteWarmPoolOutput{}, a.DeleteWarmPoolErr
}

func (a *MockAutoScalingClient) PutWarmPool(input *autoscaling.PutWarmPoolInput) (*autoscaling.PutWarmPoolOutput, error) {
	a.PutWarmPoolCallCount++
	return &autoscaling.PutWarmPoolOutput{}, a.PutWarmPoolErr
}

type MockEc2Client struct {
	ec2iface.EC2API
	DescribeSubnetsErr                   error
	DescribeSecurityGroupsErr            error
	CreateLaunchTemplateCallCount        uint
	CreateLaunchTemplateVersionCallCount uint
	ModifyLaunchTemplateCallCount        uint
	DeleteLaunchTemplateCallCount        uint
	Subnets                              []*ec2.Subnet
	SecurityGroups                       []*ec2.SecurityGroup
	LaunchTemplates                      []*ec2.LaunchTemplate
	LaunchTemplateVersions               []*ec2.LaunchTemplateVersion
	InstanceTypeOfferings                []*ec2.InstanceTypeOffering
	InstanceTypes                        []*ec2.InstanceTypeInfo
}

func (c *MockEc2Client) CreateLaunchTemplate(input *ec2.CreateLaunchTemplateInput) (*ec2.CreateLaunchTemplateOutput, error) {
	c.CreateLaunchTemplateCallCount++
	return &ec2.CreateLaunchTemplateOutput{}, nil
}

func (c *MockEc2Client) ModifyLaunchTemplate(input *ec2.ModifyLaunchTemplateInput) (*ec2.ModifyLaunchTemplateOutput, error) {
	c.ModifyLaunchTemplateCallCount++
	return &ec2.ModifyLaunchTemplateOutput{}, nil
}

func (c *MockEc2Client) CreateLaunchTemplateVersion(input *ec2.CreateLaunchTemplateVersionInput) (*ec2.CreateLaunchTemplateVersionOutput, error) {
	c.CreateLaunchTemplateVersionCallCount++
	out := &ec2.CreateLaunchTemplateVersionOutput{
		LaunchTemplateVersion: &ec2.LaunchTemplateVersion{
			VersionNumber: aws.Int64(1),
		},
	}

	return out, nil
}

func (c *MockEc2Client) DescribeLaunchTemplateVersionsPages(input *ec2.DescribeLaunchTemplateVersionsInput, callback func(*ec2.DescribeLaunchTemplateVersionsOutput, bool) bool) error {
	page, err := c.DescribeLaunchTemplateVersions(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeLaunchTemplateVersions(input *ec2.DescribeLaunchTemplateVersionsInput) (*ec2.DescribeLaunchTemplateVersionsOutput, error) {
	return &ec2.DescribeLaunchTemplateVersionsOutput{LaunchTemplateVersions: c.LaunchTemplateVersions}, nil
}

func (c *MockEc2Client) DescribeLaunchTemplatesPages(input *ec2.DescribeLaunchTemplatesInput, callback func(*ec2.DescribeLaunchTemplatesOutput, bool) bool) error {
	page, err := c.DescribeLaunchTemplates(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeLaunchTemplates(input *ec2.DescribeLaunchTemplatesInput) (*ec2.DescribeLaunchTemplatesOutput, error) {
	return &ec2.DescribeLaunchTemplatesOutput{LaunchTemplates: c.LaunchTemplates}, nil
}

func (c *MockEc2Client) DescribeInstanceTypesPages(input *ec2.DescribeInstanceTypesInput, callback func(*ec2.DescribeInstanceTypesOutput, bool) bool) error {
	page, err := c.DescribeInstanceTypes(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeInstanceTypes(input *ec2.DescribeInstanceTypesInput) (*ec2.DescribeInstanceTypesOutput, error) {
	return &ec2.DescribeInstanceTypesOutput{InstanceTypes: c.InstanceTypes}, nil
}

func (c *MockEc2Client) DescribeInstanceTypeOfferingsPages(input *ec2.DescribeInstanceTypeOfferingsInput, callback func(*ec2.DescribeInstanceTypeOfferingsOutput, bool) bool) error {
	page, err := c.DescribeInstanceTypeOfferings(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeInstanceTypeOfferings(input *ec2.DescribeInstanceTypeOfferingsInput) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
	return &ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: c.InstanceTypeOfferings}, nil
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
	AttachRolePolicyCallCount         uint
	DetachRolePolicyErr               error
	DetachRolePolicyCallCount         uint
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

type MockSsmClient struct {
	ssmiface.SSMAPI
	parameterMap map[string]string
}

func (i *MockSsmClient) GetParameter(input *ssm.GetParameterInput) (*ssm.GetParameterOutput, error) {
	return &ssm.GetParameterOutput{
		Parameter: &ssm.Parameter{
			Value: aws.String(i.parameterMap[*input.Name]),
		},
	}, nil
}
