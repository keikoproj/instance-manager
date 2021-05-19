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
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
)

// GetAwsAsgClient returns an ASG client
func GetAwsAsgClient(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) autoscalingiface.AutoScalingAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries, collector))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}

	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("autoscaling", "DescribeAutoScalingGroups", DescribeAutoScalingGroupsTTL)
	cacheCfg.SetCacheTTL("autoscaling", "DescribeWarmPool", DescribeWarmPoolTTL)
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

func (w *AwsWorker) CreateLifecycleHook(input *autoscaling.PutLifecycleHookInput) error {
	_, err := w.AsgClient.PutLifecycleHook(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DescribeWarmPool(asgName string) (*autoscaling.DescribeWarmPoolOutput, error) {
	describeWarmPoolOutput, err := w.AsgClient.DescribeWarmPool(&autoscaling.DescribeWarmPoolInput{
		AutoScalingGroupName: aws.String(asgName),
	})
	if err != nil {
		return nil, err
	}
	return describeWarmPoolOutput, nil
}

func (w *AwsWorker) UpdateWarmPool(asgName string, min, max int64) error {
	_, err := w.AsgClient.PutWarmPool(&autoscaling.PutWarmPoolInput{
		AutoScalingGroupName:     aws.String(asgName),
		MaxGroupPreparedCapacity: aws.Int64(max),
		MinSize:                  aws.Int64(min),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DeleteWarmPool(asgName string) error {
	_, err := w.AsgClient.DeleteWarmPool(&autoscaling.DeleteWarmPoolInput{
		AutoScalingGroupName: aws.String(asgName),
		ForceDelete:          aws.Bool(true),
	})
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
