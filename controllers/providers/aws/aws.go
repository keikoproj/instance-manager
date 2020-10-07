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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	log = ctrl.Log.WithName("aws-provider")
)

const (
	CacheDefaultTTL                 time.Duration = 0 * time.Second
	DescribeAutoScalingGroupsTTL    time.Duration = 60 * time.Second
	DescribeLifecycleHooksTTL       time.Duration = 60 * time.Second
	DescribeLaunchConfigurationsTTL time.Duration = 60 * time.Second
	ListAttachedRolePoliciesTTL     time.Duration = 60 * time.Second
	GetRoleTTL                      time.Duration = 60 * time.Second
	GetInstanceProfileTTL           time.Duration = 60 * time.Second
	DescribeNodegroupTTL            time.Duration = 60 * time.Second
	DescribeClusterTTL              time.Duration = 180 * time.Second
	DescribeSecurityGroupsTTL       time.Duration = 180 * time.Second
	DescribeSubnetsTTL              time.Duration = 180 * time.Second
	CacheMaxItems                   int64         = 5000
	CacheItemsToPrune               uint32        = 500
)

type AwsWorker struct {
	AsgClient  autoscalingiface.AutoScalingAPI
	EksClient  eksiface.EKSAPI
	IamClient  iamiface.IAMAPI
	Ec2Client  ec2iface.EC2API
	Parameters map[string]interface{}
}

var (
	DefaultInstanceProfilePropagationDelay = time.Second * 25
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

	AllowedVolumeTypes = []string{"gp2", "io1", "sc1", "st1"}
)

const (
	IAMPolicyPrefix                         = "arn:aws:iam::aws:policy"
	IAMARNPrefix                            = "arn:aws:iam::"
	ARNPrefix                               = "arn:aws:"
	LaunchConfigurationNotFoundErrorMessage = "Launch configuration name not found"
)

func (w *AwsWorker) CreateLifecycleHook(input *autoscaling.PutLifecycleHookInput) error {
	_, err := w.AsgClient.PutLifecycleHook(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DeleteLifecycleHook(asgName, hookName string) error {
	_, err := w.AsgClient.DeleteLifecycleHook(&autoscaling.DeleteLifecycleHookInput{
		AutoScalingGroupName: aws.String(asgName),
		LifecycleHookName:    aws.String(hookName),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DescribeLifecycleHooks(asgName string) ([]*autoscaling.LifecycleHook, error) {
	out, err := w.AsgClient.DescribeLifecycleHooks(&autoscaling.DescribeLifecycleHooksInput{
		AutoScalingGroupName: aws.String(asgName),
	})
	if err != nil {
		return []*autoscaling.LifecycleHook{}, err
	}
	return out.LifecycleHooks, nil
}

func (w *AwsWorker) RoleExist(name string) (*iam.Role, bool) {
	out, err := w.GetRole(name)
	if err != nil {
		var role *iam.Role
		return role, false
	}
	return out, true
}

func (w *AwsWorker) InstanceProfileExist(name string) (*iam.InstanceProfile, bool) {
	var (
		instanceProfile *iam.InstanceProfile
		input           = &iam.GetInstanceProfileInput{
			InstanceProfileName: aws.String(name),
		}
	)

	out, err := w.IamClient.GetInstanceProfile(input)
	if err != nil {
		return instanceProfile, false
	}
	return out.InstanceProfile, true
}

func (w *AwsWorker) GetBasicBlockDevice(name, volType, snapshot string, volSize, iops int64, delete, encrypt *bool) *autoscaling.BlockDeviceMapping {
	device := &autoscaling.BlockDeviceMapping{
		DeviceName: aws.String(name),
		Ebs: &autoscaling.Ebs{
			VolumeType: aws.String(volType),
		},
	}
	if delete != nil {
		device.Ebs.DeleteOnTermination = delete
	} else {
		device.Ebs.DeleteOnTermination = aws.Bool(true)
	}
	if encrypt != nil {
		device.Ebs.Encrypted = encrypt
	}
	if iops != 0 && strings.EqualFold(volType, "io1") {
		device.Ebs.Iops = aws.Int64(iops)
	}
	if volSize != 0 {
		device.Ebs.VolumeSize = aws.Int64(volSize)
	}
	if !common.StringEmpty(snapshot) {
		device.Ebs.SnapshotId = aws.String(snapshot)
	}

	return device
}

func (w *AwsWorker) CreateLaunchConfig(input *autoscaling.CreateLaunchConfigurationInput) error {
	_, err := w.AsgClient.CreateLaunchConfiguration(input)
	if err != nil {
		return err
	}
	return err
}

func (w *AwsWorker) DeleteLaunchConfig(name string) error {
	input := &autoscaling.DeleteLaunchConfigurationInput{
		LaunchConfigurationName: aws.String(name),
	}
	_, err := w.AsgClient.DeleteLaunchConfiguration(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) CreateScalingGroup(input *autoscaling.CreateAutoScalingGroupInput) error {
	_, err := w.AsgClient.CreateAutoScalingGroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) UpdateScalingGroupTags(add []*autoscaling.Tag, remove []*autoscaling.Tag) error {
	if len(add) > 0 {
		_, err := w.AsgClient.CreateOrUpdateTags(&autoscaling.CreateOrUpdateTagsInput{
			Tags: add,
		})
		if err != nil {
			return err
		}
	}

	if len(remove) > 0 {
		_, err := w.AsgClient.DeleteTags(&autoscaling.DeleteTagsInput{
			Tags: remove,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *AwsWorker) UpdateScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) error {
	_, err := w.AsgClient.UpdateAutoScalingGroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DeleteScalingGroup(name string) error {
	input := &autoscaling.DeleteAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(name),
		ForceDelete:          aws.Bool(true),
	}
	_, err := w.AsgClient.DeleteAutoScalingGroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) SetSuspendProcesses(name string, processesToSuspend []string) error {
	input := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(name),
		ScalingProcesses:     aws.StringSlice(processesToSuspend),
	}
	_, err := w.AsgClient.SuspendProcesses(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) SetResumeProcesses(name string, processesToResume []string) error {
	input := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(name),
		ScalingProcesses:     aws.StringSlice(processesToResume),
	}
	_, err := w.AsgClient.ResumeProcesses(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) NewTag(key, val, resource string) *autoscaling.Tag {
	return &autoscaling.Tag{
		Key:               aws.String(key),
		Value:             aws.String(val),
		PropagateAtLaunch: aws.Bool(true),
		ResourceId:        aws.String(resource),
		ResourceType:      aws.String("auto-scaling-group"),
	}
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

func (w *AwsWorker) TerminateScalingInstances(instanceIds []string) error {
	for _, instance := range instanceIds {
		_, err := w.AsgClient.TerminateInstanceInAutoScalingGroup(&autoscaling.TerminateInstanceInAutoScalingGroupInput{
			InstanceId:                     aws.String(instance),
			ShouldDecrementDesiredCapacity: aws.Bool(false),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *AwsWorker) DeleteScalingGroupRole(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return err
		}
	}

	_, err := w.IamClient.RemoveRoleFromInstanceProfile(&iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(name),
		RoleName:            aws.String(name),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	_, err = w.IamClient.DeleteInstanceProfile(&iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	// must wait until all policies are detached
	err = w.WithRetries(func() bool {
		_, err := w.IamClient.DeleteRole(&iam.DeleteRoleInput{
			RoleName: aws.String(name),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() != iam.ErrCodeNoSuchEntityException {
					log.Error(err, "failed to delete role")
					return false
				}
			}
		}
		return true
	})
	if err != nil {
		return errors.Wrap(err, "role deletion failed")
	}

	return nil
}

func (w *AwsWorker) AttachManagedPolicies(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return errors.Wrap(err, "failed to attach role policies")
		}
	}
	return nil
}

func (w *AwsWorker) DetachManagedPolicies(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return errors.Wrap(err, "failed to detach role policies")
		}
	}
	return nil
}

func (w *AwsWorker) ListRolePolicies(name string) ([]*iam.AttachedPolicy, error) {
	policies := []*iam.AttachedPolicy{}
	err := w.IamClient.ListAttachedRolePoliciesPages(
		&iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(name),
		},
		func(page *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
			for _, p := range page.AttachedPolicies {
				policies = append(policies, p)
			}
			return page.Marker != nil
		})
	if err != nil {
		return policies, err
	}
	return policies, nil
}

func (w *AwsWorker) CreateScalingGroupRole(name string) (*iam.Role, *iam.InstanceProfile, error) {
	var (
		assumeRolePolicyDocument = `{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}]
		}`
		createdRole    = &iam.Role{}
		createdProfile = &iam.InstanceProfile{}
	)
	if role, ok := w.RoleExist(name); !ok {
		out, err := w.IamClient.CreateRole(&iam.CreateRoleInput{
			RoleName:                 aws.String(name),
			AssumeRolePolicyDocument: aws.String(assumeRolePolicyDocument),
		})
		if err != nil {
			return createdRole, createdProfile, errors.Wrap(err, "failed to create role")
		}
		createdRole = out.Role
	} else {
		createdRole = role
	}

	if instanceProfile, ok := w.InstanceProfileExist(name); !ok {
		out, err := w.IamClient.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
			InstanceProfileName: aws.String(name),
		})
		if err != nil {
			return createdRole, createdProfile, errors.Wrap(err, "failed to create instance-profile")
		}
		createdProfile = out.InstanceProfile
		time.Sleep(DefaultInstanceProfilePropagationDelay)

		_, err = w.IamClient.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: aws.String(name),
			RoleName:            aws.String(name),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() != iam.ErrCodeLimitExceededException {
					return createdRole, createdProfile, errors.Wrap(err, "failed to attach instance-profile")
				}
			}
		}

	} else {
		createdProfile = instanceProfile
	}

	return createdRole, createdProfile, nil
}

// TODO: Move logic to provisioner
func (w *AwsWorker) IsNodeGroupExist() bool {
	input := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	_, err := w.EksClient.DescribeNodegroup(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == eks.ErrCodeResourceNotFoundException {
				return false
			}
		}
		log.Error(err, "failed to describe nodegroup")
		return false
	}

	return true
}

func (w *AwsWorker) DescribeEKSCluster(clusterName string) (*eks.Cluster, error) {
	cluster := &eks.Cluster{}
	input := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	output, err := w.EksClient.DescribeCluster(input)
	if err != nil {
		return cluster, err
	}
	return output.Cluster, nil
}

// TODO: Rename - GetNodeGroup
func (w *AwsWorker) GetSelfNodeGroup() (error, *eks.Nodegroup) {
	input := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	output, err := w.EksClient.DescribeNodegroup(input)
	if err != nil {
		return err, &eks.Nodegroup{}
	}
	return nil, output.Nodegroup
}

func (w *AwsWorker) DeleteManagedNodeGroup() error {
	input := &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	_, err := w.EksClient.DeleteNodegroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) GetLabelsUpdatePayload(existing, new map[string]string) *eks.UpdateLabelsPayload {

	var (
		removeLabels    = make([]string, 0)
		addUpdateLabels = make(map[string]string)
	)

	for k, v := range new {
		// handle new labels
		if _, ok := existing[k]; !ok {
			addUpdateLabels[k] = v
		}

		// handle label value updates
		if val, ok := existing[k]; ok && val != v {
			addUpdateLabels[k] = v
		}
	}

	for k := range existing {
		// handle removals
		if _, ok := new[k]; !ok {
			removeLabels = append(removeLabels, k)
		}
	}

	return &eks.UpdateLabelsPayload{
		AddOrUpdateLabels: aws.StringMap(addUpdateLabels),
		RemoveLabels:      aws.StringSlice(removeLabels),
	}
}

func (w *AwsWorker) UpdateManagedNodeGroup(currentDesired int64, labelsPayload *eks.UpdateLabelsPayload) error {
	input := &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
		ScalingConfig: &eks.NodegroupScalingConfig{
			MaxSize:     aws.Int64(w.Parameters["MaxSize"].(int64)),
			MinSize:     aws.Int64(w.Parameters["MinSize"].(int64)),
			DesiredSize: aws.Int64(currentDesired),
		},
		Labels: labelsPayload,
	}
	_, err := w.EksClient.UpdateNodegroupConfig(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) CreateManagedNodeGroup() error {
	input := &eks.CreateNodegroupInput{
		AmiType:        aws.String(w.Parameters["AmiType"].(string)),
		ClusterName:    aws.String(w.Parameters["ClusterName"].(string)),
		DiskSize:       aws.Int64(w.Parameters["DiskSize"].(int64)),
		InstanceTypes:  aws.StringSlice(w.Parameters["InstanceTypes"].([]string)),
		Labels:         aws.StringMap(w.Parameters["Labels"].(map[string]string)),
		NodeRole:       aws.String(w.Parameters["NodeRole"].(string)),
		NodegroupName:  aws.String(w.Parameters["NodegroupName"].(string)),
		ReleaseVersion: aws.String(w.Parameters["ReleaseVersion"].(string)),
		RemoteAccess: &eks.RemoteAccessConfig{
			Ec2SshKey:            aws.String(w.Parameters["Ec2SshKey"].(string)),
			SourceSecurityGroups: aws.StringSlice(w.Parameters["SourceSecurityGroups"].([]string)),
		},
		ScalingConfig: &eks.NodegroupScalingConfig{
			MaxSize:     aws.Int64(w.Parameters["MaxSize"].(int64)),
			MinSize:     aws.Int64(w.Parameters["MinSize"].(int64)),
			DesiredSize: aws.Int64(w.Parameters["MinSize"].(int64)),
		},
		Subnets: aws.StringSlice(w.Parameters["Subnets"].([]string)),
		Tags:    aws.StringMap(w.compactTags(w.Parameters["Tags"].([]map[string]string))),
		Version: aws.String(w.Parameters["Version"].(string)),
	}

	_, err := w.EksClient.CreateNodegroup(input)
	if err != nil {
		return err
	}
	return nil
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

func (w *AwsWorker) SubnetByName(name, vpc string) (*ec2.Subnet, error) {
	subnets := []*ec2.Subnet{}
	filteredSubnets := []*ec2.Subnet{}

	err := w.Ec2Client.DescribeSubnetsPages(
		&ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{aws.String(vpc)},
				},
			},
		},
		func(page *ec2.DescribeSubnetsOutput, lastPage bool) bool {
			for _, p := range page.Subnets {
				subnets = append(subnets, p)
			}
			return page.NextToken != nil
		},
	)
	if err != nil {
		return nil, err
	}

	for _, s := range subnets {
		for _, tag := range s.Tags {
			k := aws.StringValue(tag.Key)
			v := aws.StringValue(tag.Value)
			if strings.EqualFold(k, "Name") && strings.EqualFold(v, name) {
				filteredSubnets = append(filteredSubnets, s)
			}
		}
	}

	if len(filteredSubnets) == 0 {
		return nil, nil
	}
	return filteredSubnets[0], nil
}

func (w *AwsWorker) SecurityGroupByName(name, vpc string) (*ec2.SecurityGroup, error) {
	groups := []*ec2.SecurityGroup{}
	filteredGroups := []*ec2.SecurityGroup{}
	err := w.Ec2Client.DescribeSecurityGroupsPages(
		&ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{aws.String(vpc)},
				},
			},
		},
		func(page *ec2.DescribeSecurityGroupsOutput, lastPage bool) bool {
			for _, p := range page.SecurityGroups {
				groups = append(groups, p)
			}
			return page.NextToken != nil
		},
	)
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		for _, tag := range g.Tags {
			k := aws.StringValue(tag.Key)
			v := aws.StringValue(tag.Value)
			if strings.EqualFold(k, "Name") && strings.EqualFold(v, name) {
				filteredGroups = append(filteredGroups, g)
			}
		}
	}
	if len(filteredGroups) == 0 {
		return nil, nil
	}
	return filteredGroups[0], nil
}

func (w *AwsWorker) DescribeAutoscalingGroups() ([]*autoscaling.Group, error) {
	scalingGroups := []*autoscaling.Group{}
	err := w.AsgClient.DescribeAutoScalingGroupsPages(&autoscaling.DescribeAutoScalingGroupsInput{}, func(page *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		scalingGroups = append(scalingGroups, page.AutoScalingGroups...)
		return page.NextToken != nil
	})
	if err != nil {
		return scalingGroups, err
	}
	return scalingGroups, nil
}

func (w *AwsWorker) DescribeAutoscalingLaunchConfigs() ([]*autoscaling.LaunchConfiguration, error) {
	launchConfigurations := []*autoscaling.LaunchConfiguration{}
	err := w.AsgClient.DescribeLaunchConfigurationsPages(&autoscaling.DescribeLaunchConfigurationsInput{}, func(page *autoscaling.DescribeLaunchConfigurationsOutput, lastPage bool) bool {
		launchConfigurations = append(launchConfigurations, page.LaunchConfigurations...)
		return page.NextToken != nil
	})
	if err != nil {
		return launchConfigurations, err
	}
	return launchConfigurations, nil
}

func (w *AwsWorker) EnableMetrics(asgName string, metrics []string) error {
	if common.SliceEmpty(metrics) {
		return nil
	}
	_, err := w.AsgClient.EnableMetricsCollection(&autoscaling.EnableMetricsCollectionInput{
		AutoScalingGroupName: aws.String(asgName),
		Granularity:          aws.String("1Minute"),
		Metrics:              aws.StringSlice(metrics),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DisableMetrics(asgName string, metrics []string) error {
	if common.SliceEmpty(metrics) {
		return nil
	}
	_, err := w.AsgClient.DisableMetricsCollection(&autoscaling.DisableMetricsCollectionInput{
		AutoScalingGroupName: aws.String(asgName),
		Metrics:              aws.StringSlice(metrics),
	})
	if err != nil {
		return err
	}
	return nil
}

func GetScalingGroupTagsByName(name string, client autoscalingiface.AutoScalingAPI) ([]*autoscaling.TagDescription, error) {
	tags := []*autoscaling.TagDescription{}
	input := &autoscaling.DescribeAutoScalingGroupsInput{}
	out, err := client.DescribeAutoScalingGroups(input)
	if err != nil {
		return tags, err
	}
	for _, asg := range out.AutoScalingGroups {
		n := aws.StringValue(asg.AutoScalingGroupName)
		if strings.EqualFold(name, n) {
			tags = asg.Tags
		}
	}
	return tags, nil
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

func GetRegion() (string, error) {
	if os.Getenv("AWS_REGION") != "" {
		return os.Getenv("AWS_REGION"), nil
	}
	// Try Derive
	var config aws.Config
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	c := ec2metadata.New(sess)
	region, err := c.Region()
	if err != nil {
		return "", err
	}
	return region, nil
}

// GetAwsAsgClient returns an ASG client
func GetAwsAsgClient(region string, cacheCfg *cache.Config, maxRetries int) autoscalingiface.AutoScalingAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}

	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("autoscaling", "DescribeAutoScalingGroups", DescribeAutoScalingGroupsTTL)
	cacheCfg.SetCacheTTL("autoscaling", "DescribeLaunchConfigurations", DescribeLaunchConfigurationsTTL)
	cacheCfg.SetCacheTTL("autoscaling", "DescribeLifecycleHooks", DescribeLifecycleHooksTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return autoscaling.New(sess)
}

// GetAwsEc2Client returns an EC2 client
func GetAwsEc2Client(region string, cacheCfg *cache.Config, maxRetries int) ec2iface.EC2API {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}

	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("ec2", "DescribeSecurityGroups", DescribeSecurityGroupsTTL)
	cacheCfg.SetCacheTTL("ec2", "DescribeSubnets", DescribeSubnetsTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return ec2.New(sess)
}

// GetAwsEksClient returns an EKS client
func GetAwsEksClient(region string, cacheCfg *cache.Config, maxRetries int) eksiface.EKSAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("eks", "DescribeCluster", DescribeClusterTTL)
	cacheCfg.SetCacheTTL("eks", "DescribeNodegroup", DescribeNodegroupTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return eks.New(sess, config)
}

func (w *AwsWorker) DeriveEksVpcID(clusterName string) (string, error) {
	out, err := w.EksClient.DescribeCluster(&eks.DescribeClusterInput{Name: aws.String(clusterName)})
	if err != nil {
		return "", err
	}
	return aws.StringValue(out.Cluster.ResourcesVpcConfig.VpcId), nil
}

type CloudResourceReconcileState struct {
	OngoingState             bool
	FiniteState              bool
	FiniteDeleted            bool
	UpdateRecoverableError   bool
	UnrecoverableError       bool
	UnrecoverableDeleteError bool
}

var OngoingState = CloudResourceReconcileState{OngoingState: true}
var FiniteState = CloudResourceReconcileState{FiniteState: true}
var FiniteDeleted = CloudResourceReconcileState{FiniteDeleted: true}
var UpdateRecoverableError = CloudResourceReconcileState{UpdateRecoverableError: true}
var UnrecoverableError = CloudResourceReconcileState{UnrecoverableError: true}
var UnrecoverableDeleteError = CloudResourceReconcileState{UnrecoverableDeleteError: true}

// GetAwsIAMClient returns an IAM client
func GetAwsIamClient(region string, cacheCfg *cache.Config, maxRetries int) iamiface.IAMAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("iam", "GetInstanceProfile", GetInstanceProfileTTL)
	cacheCfg.SetCacheTTL("iam", "GetRole", GetRoleTTL)
	cacheCfg.SetCacheTTL("iam", "ListAttachedRolePolicies", ListAttachedRolePoliciesTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return iam.New(sess, config)
}

type ManagedNodeGroupReconcileState struct {
	OngoingState             bool
	FiniteState              bool
	UnrecoverableError       bool
	UnrecoverableDeleteError bool
}

var ManagedNodeGroupOngoingState = ManagedNodeGroupReconcileState{OngoingState: true}
var ManagedNodeGroupFiniteState = ManagedNodeGroupReconcileState{FiniteState: true}
var ManagedNodeGroupUnrecoverableError = ManagedNodeGroupReconcileState{UnrecoverableError: true}
var ManagedNodeGroupUnrecoverableDeleteError = ManagedNodeGroupReconcileState{UnrecoverableDeleteError: true}

func IsNodeGroupInConditionState(key string, condition string) bool {
	conditionStates := map[string]ManagedNodeGroupReconcileState{
		"CREATING":      ManagedNodeGroupOngoingState,
		"UPDATING":      ManagedNodeGroupOngoingState,
		"DELETING":      ManagedNodeGroupOngoingState,
		"ACTIVE":        ManagedNodeGroupFiniteState,
		"DEGRADED":      ManagedNodeGroupFiniteState,
		"CREATE_FAILED": ManagedNodeGroupUnrecoverableError,
		"DELETE_FAILED": ManagedNodeGroupUnrecoverableDeleteError,
	}
	state := conditionStates[key]

	switch condition {
	case "OngoingState":
		return state.OngoingState
	case "FiniteState":
		return state.FiniteState
	case "UnrecoverableError":
		return state.UnrecoverableError
	case "UnrecoverableDeleteError":
		return state.UnrecoverableDeleteError
	default:
		return false
	}
}

func IsProfileInConditionState(key string, condition string) bool {

	conditionStates := map[string]CloudResourceReconcileState{
		aws.StringValue(nil):                 FiniteDeleted,
		eks.FargateProfileStatusCreating:     OngoingState,
		eks.FargateProfileStatusActive:       FiniteState,
		eks.FargateProfileStatusDeleting:     OngoingState,
		eks.FargateProfileStatusCreateFailed: UpdateRecoverableError,
		eks.FargateProfileStatusDeleteFailed: UnrecoverableDeleteError,
	}
	state := conditionStates[key]
	switch condition {
	case "OngoingState":
		return state.OngoingState
	case "FiniteState":
		return state.FiniteState
	case "FiniteDeleted":
		return state.FiniteDeleted
	case "UpdateRecoverableError":
		return state.UpdateRecoverableError
	case "UnrecoverableError":
		return state.UnrecoverableError
	case "UnrecoverableDeleteError":
		return state.UnrecoverableDeleteError
	default:
		return false
	}
}

const defaultPolicyArn = "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"

func (w *AwsWorker) DetachDefaultPolicyFromDefaultRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	rolePolicy := &iam.DetachRolePolicyInput{
		PolicyArn: aws.String(defaultPolicyArn),
		RoleName:  aws.String(roleName),
	}
	_, err := w.IamClient.DetachRolePolicy(rolePolicy)
	return err
}

func (w *AwsWorker) DeleteDefaultFargateRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	role := &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}
	_, err := w.IamClient.DeleteRole(role)
	return err
}

func (w *AwsWorker) GetDefaultFargateRole() (*iam.Role, error) {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	return w.GetRole(roleName)
}
func (w *AwsWorker) GetRole(roleName string) (*iam.Role, error) {
	role := &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}
	resp, err := w.IamClient.GetRole(role)
	if err != nil {
		return nil, err
	}

	return resp.Role, nil
}
func (w *AwsWorker) CreateDefaultFargateRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	var template = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks-fargate-pods.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	role := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: &template,
		Path:                     aws.String("/"),
		RoleName:                 aws.String(roleName),
	}
	_, err := w.IamClient.CreateRole(role)
	return err
}

func (w *AwsWorker) AttachDefaultPolicyToDefaultRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	rolePolicy := &iam.AttachRolePolicyInput{
		PolicyArn: aws.String(defaultPolicyArn),
		RoleName:  aws.String(roleName),
	}
	_, err := w.IamClient.AttachRolePolicy(rolePolicy)
	if err == nil {
		time.Sleep(DefaultInstanceProfilePropagationDelay)
	}
	return err
}

func (w *AwsWorker) CreateFargateProfile(arn string) error {
	tags := w.Parameters["Tags"].(map[string]*string)
	if len(tags) == 0 {
		tags = nil
	}
	selectors := w.Parameters["Selectors"].([]*eks.FargateProfileSelector)
	if len(selectors) == 0 {
		selectors = nil
	}

	fargateInput := &eks.CreateFargateProfileInput{
		ClusterName:         aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName:  aws.String(w.Parameters["ProfileName"].(string)),
		PodExecutionRoleArn: aws.String(arn),
		Selectors:           selectors,
		Subnets:             aws.StringSlice(w.Parameters["Subnets"].([]string)),
		Tags:                tags,
	}

	_, err := w.EksClient.CreateFargateProfile(fargateInput)
	return err
}

func (w *AwsWorker) DeleteFargateProfile() error {
	deleteInput := &eks.DeleteFargateProfileInput{
		ClusterName:        aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName: aws.String(w.Parameters["ProfileName"].(string)),
	}
	_, err := w.EksClient.DeleteFargateProfile(deleteInput)
	return err
}

func (w *AwsWorker) DescribeFargateProfile() (*eks.FargateProfile, error) {
	describeInput := &eks.DescribeFargateProfileInput{
		ClusterName:        aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName: aws.String(w.Parameters["ProfileName"].(string)),
	}
	output, err := w.EksClient.DescribeFargateProfile(describeInput)
	if err != nil {
		return nil, err
	}
	return output.FargateProfile, nil
}
