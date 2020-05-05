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
	"encoding/base64"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type AwsWorker struct {
	CfClient        cloudformationiface.CloudFormationAPI
	AsgClient       autoscalingiface.AutoScalingAPI
	EksClient       eksiface.EKSAPI
	IamClient       iamiface.IAMAPI
	TemplateBody    string
	StackName       string
	StackTags       []*cloudformation.Tag
	StackParameters []*cloudformation.Parameter
	Parameters      map[string]interface{}
}

func DefaultUserDataFmt() string {
	return `#!/bin/bash
set -o xtrace
/etc/eks/bootstrap.sh %s %s`
}

func (w *AwsWorker) RoleExist(name string) (*iam.Role, bool) {
	var role *iam.Role
	input := &iam.GetRoleInput{
		RoleName: aws.String(name),
	}
	out, err := w.IamClient.GetRole(input)
	if err != nil {
		return role, false
	}
	return out.Role, true
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

func (w *AwsWorker) GetBasicBlockDevice(name, volType string, volSize int64) *autoscaling.BlockDeviceMapping {
	return &autoscaling.BlockDeviceMapping{
		DeviceName: aws.String(name),
		Ebs: &autoscaling.Ebs{
			VolumeSize:          aws.Int64(volSize),
			VolumeType:          aws.String(volType),
			DeleteOnTermination: aws.Bool(true),
		},
	}
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

func (w *AwsWorker) UpdateScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput, tags []*autoscaling.Tag) error {
	tagsInput := &autoscaling.CreateOrUpdateTagsInput{
		Tags: tags,
	}
	_, err := w.AsgClient.CreateOrUpdateTags(tagsInput)
	if err != nil {
		return err
	}

	_, err = w.AsgClient.UpdateAutoScalingGroup(input)
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

func (w *AwsWorker) GetBasicUserData(clusterName, bootstrapArgs string) string {
	userData := fmt.Sprintf(DefaultUserDataFmt(), clusterName, bootstrapArgs)
	return base64.StdEncoding.EncodeToString([]byte(userData))
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

func (w *AwsWorker) DeleteScalingGroupRole(name string, managedPolicies []string) error {

	for _, policy := range managedPolicies {
		policyBindingInput := &iam.DetachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		}

		_, err := w.IamClient.DetachRolePolicy(policyBindingInput)
		if err != nil {
			return err
		}
	}

	profileBindingInput := &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(name),
		RoleName:            aws.String(name),
	}

	_, err := w.IamClient.RemoveRoleFromInstanceProfile(profileBindingInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	profileInput := &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	}

	_, err = w.IamClient.DeleteInstanceProfile(profileInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	roleInput := &iam.DeleteRoleInput{
		RoleName: aws.String(name),
	}

	_, err = w.IamClient.DeleteRole(roleInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}
	return nil
}

func (w *AwsWorker) CreateUpdateScalingGroupRole(name string, managedPolicies []string) (*iam.Role, *iam.InstanceProfile, error) {
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
		log.Infof("created IAM role %s", aws.StringValue(createdRole.RoleName))
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
		log.Infof("created instance-profile %s", aws.StringValue(createdProfile.InstanceProfileName))
		createdProfile = out.InstanceProfile
	} else {
		createdProfile = instanceProfile
	}

	_, err := w.IamClient.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
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

	for _, policy := range managedPolicies {
		_, err = w.IamClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return createdRole, createdProfile, errors.Wrap(err, "failed to attach policies")
		}
	}

	return createdRole, createdProfile, nil
}

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
		log.Errorln(err)
		return false
	}

	return true
}

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

	for k, _ := range existing {
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

func (w *AwsWorker) CreateCloudformationStack() error {
	capabilities := []*string{
		aws.String("CAPABILITY_IAM"),
	}

	input := &cloudformation.CreateStackInput{
		TemplateBody: aws.String(w.TemplateBody),
		StackName:    aws.String(w.StackName),
		Parameters:   w.StackParameters,
		Capabilities: capabilities,
		Tags:         w.StackTags,
	}
	_, err := w.CfClient.CreateStack(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			return awsErr
		}
		log.Errorln(err)
		return err
	}
	return nil
}

func (w *AwsWorker) UpdateCloudformationStack() (error, bool) {
	capabilities := []*string{
		aws.String("CAPABILITY_IAM"),
	}
	input := &cloudformation.UpdateStackInput{
		TemplateBody: aws.String(w.TemplateBody),
		StackName:    aws.String(w.StackName),
		Parameters:   w.StackParameters,
		Capabilities: capabilities,
		Tags:         w.StackTags,
	}
	_, err := w.CfClient.UpdateStack(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == "ValidationError" && awsErr.Message() == "No updates are to be performed." {
				log.Infof("update not required")
				return nil, false
			}
			return awsErr, false
		}
		return err, false
	}
	return nil, true
}

func (w *AwsWorker) DeleteCloudformationStack() error {
	input := &cloudformation.DeleteStackInput{
		StackName: aws.String(w.StackName),
	}
	_, err := w.CfClient.DeleteStack(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			return awsErr
		}
		return err
	}
	return nil
}

func (w *AwsWorker) CloudformationStackExists() bool {
	input := &cloudformation.DescribeStacksInput{
		StackName: aws.String(w.StackName),
	}
	stacks, err := w.CfClient.DescribeStacks(input)
	if err != nil {
		return false
	}

	if len(stacks.Stacks) == 0 {
		return false
	}

	return true
}

func (w *AwsWorker) GetStackState() (string, error) {
	input := &cloudformation.DescribeStacksInput{
		StackName: aws.String(w.StackName),
	}

	d, err := w.CfClient.DescribeStacks(input)
	if err != nil {
		return "", err
	}

	if len(d.Stacks) == 0 {
		return "", fmt.Errorf("Could not find stack state for %v", w.StackName)
	}

	return *d.Stacks[0].StackStatus, nil
}

func (w *AwsWorker) DescribeCloudformationStacks() (cloudformation.DescribeStacksOutput, error) {
	out, err := w.CfClient.DescribeStacks(&cloudformation.DescribeStacksInput{})
	if err != nil {
		return cloudformation.DescribeStacksOutput{}, err
	}
	return *out, nil
}

func (w *AwsWorker) DescribeAutoscalingGroups() (autoscaling.DescribeAutoScalingGroupsOutput, error) {
	out, err := w.AsgClient.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	if err != nil {
		return autoscaling.DescribeAutoScalingGroupsOutput{}, err
	}
	return *out, nil
}

func (w *AwsWorker) DescribeAutoscalingLaunchConfigs() (autoscaling.DescribeLaunchConfigurationsOutput, error) {
	out, err := w.AsgClient.DescribeLaunchConfigurations(&autoscaling.DescribeLaunchConfigurationsInput{})
	if err != nil {
		return autoscaling.DescribeLaunchConfigurationsOutput{}, err
	}
	return *out, nil
}

func (w *AwsWorker) GetAutoscalingLaunchConfig(name string) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	out, err := w.AsgClient.DescribeLaunchConfigurations(&autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: aws.StringSlice([]string{name}),
	})
	if err != nil {
		return &autoscaling.DescribeLaunchConfigurationsOutput{}, err
	}
	return out, nil
}

func (w *AwsWorker) GetAutoscalingGroup(name string) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	out, err := w.AsgClient.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{name}),
	})
	if err != nil {
		return &autoscaling.DescribeAutoScalingGroupsOutput{}, err
	}
	return out, nil
}

func (w *AwsWorker) DetectScalingGroupDrift(scalingGroupName string) (bool, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{scalingGroupName}),
	}
	out, err := w.AsgClient.DescribeAutoScalingGroups(input)
	if err != nil {
		return false, err
	}
	if len(out.AutoScalingGroups) != 1 {
		err = fmt.Errorf("could not find active scaling group")
		return false, err
	}
	for _, group := range out.AutoScalingGroups {
		for _, instance := range group.Instances {
			if instance.LaunchConfigurationName == nil {
				return true, nil
			}
		}
	}
	return false, nil
}

func GetScalingGroupTagsByName(name string, client autoscalingiface.AutoScalingAPI) ([]*autoscaling.TagDescription, error) {
	tags := []*autoscaling.TagDescription{}
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{name}),
	}
	out, err := client.DescribeAutoScalingGroups(input)
	if err != nil {
		return tags, err
	}

	if len(out.AutoScalingGroups) < 1 {
		err := errors.New("could not find scaling group")
		return tags, err
	}
	tags = out.AutoScalingGroups[0].Tags
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

// GetAwsCloudformationClient returns a cloudformation client
func GetAwsCloudformationClient(region string) cloudformationiface.CloudFormationAPI {
	var config aws.Config
	config.Region = aws.String(region)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	return cloudformation.New(sess)
}

// GetAwsAsgClient returns an ASG client
func GetAwsAsgClient(region string) autoscalingiface.AutoScalingAPI {
	var config aws.Config
	config.Region = aws.String(region)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	return autoscaling.New(sess)
}

// GetAwsEksClient returns an EKS client
func GetAwsEksClient(region string) eksiface.EKSAPI {
	var config aws.Config
	config.Region = aws.String(region)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	return eks.New(sess)
}

// GetAwsIAMClient returns an IAM client
func GetAwsIamClient(region string) iamiface.IAMAPI {
	mySession := session.Must(session.NewSession())
	return iam.New(mySession, aws.NewConfig().WithRegion(region))
}

func (w *AwsWorker) DeriveEksVpcID(clusterName string) (string, error) {
	out, err := w.EksClient.DescribeCluster(&eks.DescribeClusterInput{Name: aws.String(clusterName)})
	if err != nil {
		return "", err
	}
	return aws.StringValue(out.Cluster.ResourcesVpcConfig.VpcId), nil
}

type CloudformationReconcileState struct {
	OngoingState             bool
	FiniteState              bool
	FiniteDeleted            bool
	UpdateRecoverableError   bool
	UnrecoverableError       bool
	UnrecoverableDeleteError bool
}

var OngoingState = CloudformationReconcileState{OngoingState: true}
var FiniteState = CloudformationReconcileState{FiniteState: true}
var FiniteDeleted = CloudformationReconcileState{FiniteDeleted: true}
var UpdateRecoverableError = CloudformationReconcileState{UpdateRecoverableError: true}
var UnrecoverableError = CloudformationReconcileState{UnrecoverableError: true}
var UnrecoverableDeleteError = CloudformationReconcileState{UnrecoverableDeleteError: true}

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

func IsStackInConditionState(key string, condition string) bool {
	conditionStates := map[string]CloudformationReconcileState{
		"CREATE_COMPLETE":                              FiniteState,
		"UPDATE_COMPLETE":                              FiniteState,
		"DELETE_COMPLETE":                              FiniteDeleted,
		"CREATE_IN_PROGRESS":                           OngoingState,
		"DELETE_IN_PROGRESS":                           OngoingState,
		"ROLLBACK_IN_PROGRESS":                         OngoingState,
		"UPDATE_COMPLETE_CLEANUP_IN_PROGRESS":          OngoingState,
		"UPDATE_IN_PROGRESS":                           OngoingState,
		"UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS": OngoingState,
		"UPDATE_ROLLBACK_IN_PROGRESS":                  OngoingState,
		"UPDATE_ROLLBACK_COMPLETE":                     UpdateRecoverableError,
		"UPDATE_ROLLBACK_FAILED":                       UnrecoverableError,
		"CREATE_FAILED":                                UnrecoverableError,
		"DELETE_FAILED":                                UnrecoverableDeleteError,
		"ROLLBACK_COMPLETE":                            UnrecoverableError,
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
