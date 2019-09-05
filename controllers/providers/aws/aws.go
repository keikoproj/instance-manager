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
	log "github.com/sirupsen/logrus"
)

type AwsWorker struct {
	CfClient        cloudformationiface.CloudFormationAPI
	AsgClient       autoscalingiface.AutoScalingAPI
	EksClient       eksiface.EKSAPI
	TemplateBody    string
	StackName       string
	StackTags       []*cloudformation.Tag
	StackParameters []*cloudformation.Parameter
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

func (w *AwsWorker) DeriveEksVpcID(clusterName string) (string, error) {
	out, err := w.EksClient.DescribeCluster(&eks.DescribeClusterInput{Name: aws.String(clusterName)})
	if err != nil {
		return "", err
	}
	return aws.StringValue(out.Cluster.ResourcesVpcConfig.VpcId), nil
}

type CloudformationReconcileState struct {
	OngoingState           bool
	FiniteState            bool
	FiniteDeleted          bool
	UpdateRecoverableError bool
	UnrecoverableError     bool
}

var OngoingState = CloudformationReconcileState{OngoingState: true}
var FiniteState = CloudformationReconcileState{FiniteState: true}
var FiniteDeleted = CloudformationReconcileState{FiniteDeleted: true}
var UpdateRecoverableError = CloudformationReconcileState{UpdateRecoverableError: true}
var UnrecoverableError = CloudformationReconcileState{UnrecoverableError: true}

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
		"DELETE_FAILED":                                UnrecoverableError,
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
	default:
		return false
	}
}
