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
	"errors"
	"fmt"
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
	log "github.com/sirupsen/logrus"
	"os"
	"time"
)

type AwsWorker struct {
	CfClient        cloudformationiface.CloudFormationAPI
	AsgClient       autoscalingiface.AutoScalingAPI
	EksClient       eksiface.EKSAPI
	TemplateBody    string
	StackName       string
	StackTags       []*cloudformation.Tag
	StackParameters []*cloudformation.Parameter
	Parameters      map[string]interface{}
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

type AwsFargateWorker struct {
	ClusterName               *string
	EksClient                 eksiface.EKSAPI
	ExecutionArn              *string
	IamClient                 iamiface.IAMAPI
	PodExecutionRoleStackName *string
	ProfileName               *string
	RetryLimit                int
	RoleName                  *string
	Selectors                 []*eks.FargateProfileSelector
	Subnets                   []*string
	Tags                      map[string]*string
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

// GetAwsAsgClient returns an ASG client
func GetAwsEksClient(region string) eksiface.EKSAPI {
	var config aws.Config
	config.Region = aws.String(region)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	return eks.New(sess)
}

// GetAwsAsgClient returns an ASG client
func GetAwsIAMClient(region string) iamiface.IAMAPI {
	var config aws.Config
	config.Region = aws.String(region)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            config,
	}))
	return iam.New(sess)
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
	conditionStates := map[string]CloudResourceReconcileState{
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
func IsProfileInConditionState(key string, condition string) bool {
	conditionStates := map[string]CloudResourceReconcileState{
		"NONE":          FiniteDeleted,
		"CREATING":      OngoingState,
		"ACTIVE":        FiniteState,
		"DELETING":      OngoingState,
		"CREATE_FAILED": UpdateRecoverableError,
		"DELETE_FAILED": UnrecoverableDeleteError,
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

type ResourceState struct {
	RoleExists bool
	Profile    *eks.FargateProfile
}

func (state *ResourceState) GetRoleExists() bool {
	return state.RoleExists

}
func (state *ResourceState) GetProfileState() *string {
	none := "NONE"
	if state.Profile != nil {
		return state.Profile.Status

	} else {
		return &none
	}
}
func (state *ResourceState) IsProvisioned() bool {
	return *state.GetProfileState() != "NONE"
}

func (w *AwsFargateWorker) GetState() *ResourceState {
	state := ResourceState{}
	state.Profile, _ = w.DescribeProfile()
	state.RoleExists = w.HasDefaultRole()
	return &state

}

func (w *AwsFargateWorker) Create() error {
	input := &eks.CreateFargateProfileInput{
		ClusterName:         w.ClusterName,
		FargateProfileName:  w.ProfileName,
		PodExecutionRoleArn: w.ExecutionArn,
		Selectors:           w.Selectors,
	}
	_, err := w.EksClient.CreateFargateProfile(input)
	return err
}

var defaultPolicyArn = "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"

func (w *AwsFargateWorker) DeleteDefaultRolePolicy() error {
	if w.RoleName == nil {
		return errors.New("DeleteDefaultRolePolicy - AwsFargateWorker.RoleName is nil.  Needs a role name.")
	}
	rolePolicy := &iam.DetachRolePolicyInput{
		PolicyArn: aws.String(defaultPolicyArn),
		RoleName:  w.RoleName,
	}
	_, err := w.IamClient.DetachRolePolicy(rolePolicy)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {

			case iam.ErrCodeNoSuchEntityException:
				log.Infof("DeleteDefaultRolePolicy - ErrCodeNoSuchEntityException: %v:", aerr.Error())
				return nil
			default:
				log.Errorf("DeleteDefaultRolePolicy - %v:", aerr.Error())
				return aerr
			}
		} else {
			log.Errorf("DeleteDefaultRolePolicy - %v:", err)
		}
		return err
	}
	role := &iam.DeleteRoleInput{
		RoleName: w.RoleName,
	}
	_, err = w.IamClient.DeleteRole(role)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {

			case iam.ErrCodeNoSuchEntityException:
				log.Infof("DeleteDefaultRolePolicy - ErrCodeNoSuchEntityException: %v:", aerr.Error())
				return nil
			default:
				log.Errorf("DeleteDefaultRolePolicy - %v:", aerr.Error())
				return aerr
			}
		} else {
			log.Errorf("DeleteDefaultRolePolicy - %v:", err)
		}
	}
	return err
}
func (w *AwsFargateWorker) CreateDefaultRoleName() *string {
	s := fmt.Sprintf("%v_%v_Role", *w.ClusterName, *w.ProfileName)
	return &s
}

func (w *AwsFargateWorker) CreateDefaultRolePolicy() (*string, error) {
	var roleName = w.CreateDefaultRoleName()
	var template = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks-fargate-pods.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	role := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: &template,
		Path:                     aws.String("/"),
		RoleName:                 roleName,
	}
	createRoleOutput, err := w.IamClient.CreateRole(role)

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == iam.ErrCodeEntityAlreadyExistsException {
				// Role already exists.  Need the arn
				getRoleInput := &iam.GetRoleInput{
					RoleName: roleName,
				}
				getRoleOutput, err := w.IamClient.GetRole(getRoleInput)
				if err != nil {
					return nil, err
				}
				w.RoleName = getRoleOutput.Role.RoleName
				return getRoleOutput.Role.Arn, nil
			}
			return nil, aerr
		}
		return nil, err
	} else {
		w.RoleName = roleName
		rolePolicy := &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(defaultPolicyArn),
			RoleName:  roleName,
		}
		_, err = w.IamClient.AttachRolePolicy(rolePolicy)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() == iam.ErrCodeEntityAlreadyExistsException {
					return createRoleOutput.Role.Arn, nil
				}
				return nil, aerr
			}
			return nil, err
		}
		return createRoleOutput.Role.Arn, nil
	}
}
func (w *AwsFargateWorker) CreateProfile(arn *string) error {
	input := &eks.CreateFargateProfileInput{
		ClusterName:         w.ClusterName,
		FargateProfileName:  w.ProfileName,
		PodExecutionRoleArn: arn,
		Selectors:           w.Selectors,
		Subnets:             w.Subnets,
		Tags:                w.Tags,
	}
	// Lets see if the profile exists.  Return now if it does.
	_, err := w.DescribeProfile()
	if err == nil {
		//profile exists
		return nil
	}

	for i := 0; i < w.RetryLimit && err != nil; i++ {
		log.Infof("CreateProfile - %d try - creating profile for cluster: %s, name: %s, arn: %s", i, *w.ClusterName, *w.ProfileName, *arn)
		time.Sleep(time.Duration(i*500) * time.Millisecond)
		_, err = w.EksClient.CreateFargateProfile(input)
	}
	return err
}

func (w *AwsFargateWorker) DeleteProfile() error {
	input := &eks.DeleteFargateProfileInput{
		ClusterName:        w.ClusterName,
		FargateProfileName: w.ProfileName,
	}
	_, err := w.EksClient.DeleteFargateProfile(input)
	return err
}

/*
func (w *AwsFargateWorker) DescribeProfile() (*eks.FargateProfile, error) {
	input := &eks.DescribeFargateProfileInput{
		ClusterName:        w.ClusterName,
		FargateProfileName: w.ProfileName,
	}
	output, err := w.EksClient.DescribeFargateProfile(input)
	if err != nil {
		return nil, err
	}
	return output.FargateProfile, nil
}
*/

func (w *AwsFargateWorker) HasDefaultRole() bool {
	input := &iam.GetRoleInput{
		RoleName: w.CreateDefaultRoleName(),
	}
	_, err := w.IamClient.GetRole(input)
	return err == nil
}

func (w *AwsFargateWorker) DescribeAllProfiles(profiles []*string) ([]*eks.FargateProfile, error) {
	fargateProfiles := []*eks.FargateProfile{}
	for _, profile := range profiles {
		x, err := DescribeProfileWithParms(w.EksClient, w.ClusterName, profile)
		if err != nil {
			return nil, err
		} else {
			fargateProfiles = append(fargateProfiles, x)
		}
	}
	return fargateProfiles, nil
}

func (w *AwsFargateWorker) ListAllProfiles() ([]*string, error) {
	profiles := []*string{}
	input := &eks.ListFargateProfilesInput{
		ClusterName: w.ClusterName,
	}
	err := w.EksClient.ListFargateProfilesPages(input,
		func(page *eks.ListFargateProfilesOutput, lastPage bool) bool {
			profiles = append(profiles, page.FargateProfileNames...)
			return !lastPage
		})
	if err != nil {
		log.Errorf("ListAllProfiles - Failed on cluster: %s with error: %v", *w.ClusterName, err)
		return nil, err
	}
	return profiles, nil
}

func DescribeProfileWithParms(client eksiface.EKSAPI, clusterName *string, profileName *string) (*eks.FargateProfile, error) {
	input := &eks.DescribeFargateProfileInput{
		ClusterName:        clusterName,
		FargateProfileName: profileName,
	}
	output, err := client.DescribeFargateProfile(input)
	if err != nil {
		return nil, err
	}
	return output.FargateProfile, nil
}
func (w *AwsFargateWorker) DescribeProfile() (*eks.FargateProfile, error) {
	return DescribeProfileWithParms(w.EksClient, w.ClusterName, w.ProfileName)
}
func IsDeleting(fargateProfiles []*eks.FargateProfile) bool {
	for _, profile := range fargateProfiles {
		if *profile.Status == "DELETING" {
			return true
		}
	}
	return false
}
