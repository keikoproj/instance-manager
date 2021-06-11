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

package aws

import (
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	log = ctrl.Log.WithName("aws-provider")
)

const (
	CacheDefaultTTL                   time.Duration = 0 * time.Second
	DescribeWarmPoolTTL               time.Duration = 60 * time.Second
	DescribeAutoScalingGroupsTTL      time.Duration = 60 * time.Second
	DescribeLaunchConfigurationsTTL   time.Duration = 60 * time.Second
	ListAttachedRolePoliciesTTL       time.Duration = 60 * time.Second
	GetRoleTTL                        time.Duration = 60 * time.Second
	GetInstanceProfileTTL             time.Duration = 60 * time.Second
	DescribeNodegroupTTL              time.Duration = 60 * time.Second
	DescribeLifecycleHooksTTL         time.Duration = 180 * time.Second
	DescribeClusterTTL                time.Duration = 180 * time.Second
	DescribeSecurityGroupsTTL         time.Duration = 180 * time.Second
	DescribeSubnetsTTL                time.Duration = 180 * time.Second
	DescribeLaunchTemplatesTTL        time.Duration = 60 * time.Second
	DescribeLaunchTemplateVersionsTTL time.Duration = 60 * time.Second
	DescribeInstanceTypesTTL          time.Duration = 24 * time.Hour
	DescribeInstanceTypeOfferingTTL   time.Duration = 1 * time.Hour

	CacheBackgroundPruningInterval time.Duration = 1 * time.Hour
	CacheMaxItems                  int64         = 250
	CacheItemsToPrune              uint32        = 25

	LaunchTemplateStrategyCapacityOptimized = "capacity-optimized"
	LaunchTemplateStrategyLowestPrice       = "lowest-price"
	LaunchTemplateAllocationStrategy        = "prioritized"
	LaunchTemplateLatestVersionKey          = "$Latest"
	IAMPolicyPrefix                         = "arn:aws:iam::aws:policy"
	IAMARNPrefix                            = "arn:aws:iam::"
	ARNPrefix                               = "arn:aws:"
	LaunchConfigurationNotFoundErrorMessage = "Launch configuration name not found"
	defaultPolicyArn                        = "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"
)

var (
	DefaultInstanceProfilePropagationDelay = time.Second * 35
	DefaultWaiterDuration                  = time.Second * 5
	DefaultWaiterRetries                   = 12

	DefaultSuspendProcesses = []string{
		"Launch",
		"Terminate",
		"AddToLoadBalancer",
		"AlarmNotification",
		"AZRebalance",
		"HealthCheck",
		"InstanceRefresh",
		"ReplaceUnhealthy",
		"ScheduledActions",
	}

	DefaultAutoscalingMetrics = []string{
		"GroupMinSize",
		"GroupMaxSize",
		"GroupDesiredCapacity",
		"GroupInServiceInstances",
		"GroupPendingInstances",
		"GroupStandbyInstances",
		"GroupTerminatingInstances",
		"GroupInServiceCapacity",
		"GroupPendingCapacity",
		"GroupTerminatingCapacity",
		"GroupStandbyCapacity",
		"GroupTotalInstances",
		"GroupTotalCapacity",
	}

	ConfigurationAllowedVolumeTypes             = []string{"gp2", "io1", "sc1", "st1"}
	TemplateAllowedVolumeTypes                  = []string{"gp2", "gp3", "io1", "io2", "sc1", "st1"}
	AllowedVolumeTypesWithProvisionedIOPS       = []string{"io1", "io2", "gp3"}
	AllowedVolumeTypesWithProvisionedThroughput = []string{"gp3"}
	LifecycleHookTransitionLaunch               = "autoscaling:EC2_INSTANCE_LAUNCHING"
	LifecycleHookTransitionTerminate            = "autoscaling:EC2_INSTANCE_TERMINATING"
)

type AwsWorker struct {
	AsgClient   autoscalingiface.AutoScalingAPI
	EksClient   eksiface.EKSAPI
	IamClient   iamiface.IAMAPI
	Ec2Client   ec2iface.EC2API
	Ec2Metadata *ec2metadata.EC2Metadata
	Parameters  map[string]interface{}
}

func (w *AwsWorker) WithRetries(f func() bool) error {
	var counter int
	for {
		if counter >= DefaultWaiterRetries {
			break
		}
		if f() {
			return nil
		}
		time.Sleep(DefaultWaiterDuration)
		counter++
	}
	return errors.New("waiter timed out")
}

func (w *AwsWorker) compactTags(tags []map[string]string) map[string]string {
	compacted := make(map[string]string)
	for _, tagSet := range tags {
		var (
			key   string
			value string
		)
		for t, v := range tagSet {
			if t == "key" {
				key = v
			} else if t == "value" {
				value = v
			}
		}
		compacted[key] = value
	}
	return compacted
}

func GetTagValueByKey(tags []*autoscaling.TagDescription, key string) string {
	for _, tag := range tags {
		k := aws.StringValue(tag.Key)
		v := aws.StringValue(tag.Value)
		if key == k {
			return v
		}
	}
	return ""
}

func GetRegion(metadata *ec2metadata.EC2Metadata) (string, error) {
	if os.Getenv("AWS_REGION") != "" {
		return os.Getenv("AWS_REGION"), nil
	}

	region, err := metadata.Region()
	if err != nil {
		return "", err
	}
	return region, nil
}

func GetOfferingVCPU(typeInfo []*ec2.InstanceTypeInfo, instanceType string) int64 {
	for _, i := range typeInfo {
		t := aws.StringValue(i.InstanceType)
		if strings.EqualFold(instanceType, t) {
			return aws.Int64Value(i.VCpuInfo.DefaultVCpus)
		}
	}
	return 0
}

func GetOfferingMemory(typeInfo []*ec2.InstanceTypeInfo, instanceType string) int64 {
	for _, i := range typeInfo {
		t := aws.StringValue(i.InstanceType)
		if strings.EqualFold(instanceType, t) {
			return aws.Int64Value(i.MemoryInfo.SizeInMiB)
		}
	}
	return 0
}

func GetInstanceGeneration(instanceType string) string {
	typeSplit := strings.Split(instanceType, ".")
	if len(typeSplit) < 2 {
		return ""
	}
	instanceClass := typeSplit[0]
	re := regexp.MustCompile("[0-9]+")
	gen := re.FindAllString(instanceClass, -1)
	if len(gen) < 1 {
		return ""
	}
	return gen[0]
}

func GetInstanceFamily(instanceType string) string {
	if len(instanceType) > 0 {
		return instanceType[0:1]
	}
	return ""
}

func GetScalingConfigName(group *autoscaling.Group) string {
	var configName string
	if IsUsingLaunchConfiguration(group) {
		configName = aws.StringValue(group.LaunchConfigurationName)
	} else if IsUsingLaunchTemplate(group) {
		configName = aws.StringValue(group.LaunchTemplate.LaunchTemplateName)
	} else if IsUsingMixedInstances(group) {
		configName = aws.StringValue(group.MixedInstancesPolicy.LaunchTemplate.LaunchTemplateSpecification.LaunchTemplateName)
	}
	return configName
}

func GetInstanceTypeNetworkInfo(instanceTypes []*ec2.InstanceTypeInfo, instanceType string) *ec2.NetworkInfo {
	for _, instanceTypeInfo := range instanceTypes {
		if aws.StringValue(instanceTypeInfo.InstanceType) == instanceType {
			return instanceTypeInfo.NetworkInfo
		}
	}
	return nil
}
