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
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
	"github.com/pkg/errors"
)

func (ctx *EksInstanceGroupContext) UpgradeNodes() error {
	var (
		instanceGroup     = ctx.GetInstanceGroup()
		strategy          = ctx.GetUpgradeStrategy()
		state             = ctx.GetDiscoveredState()
		scalingGroup      = state.GetScalingGroup()
		scalingConfigName = awsprovider.GetScalingConfigName(scalingGroup)
		strategyType      = strings.ToLower(strategy.GetType())
	)

	rotated, err := ctx.rotateWarmPool()
	if err != nil {
		ctx.Log.Info("failed to rotate warm pool", "error", err)
		return nil
	}

	// if warm pool has been just deleted, we skip the rolling upgrade submission and wait for rotation to complete
	if rotated {
		return nil
	}

	// process the upgrade strategy
	switch strategyType {
	case kubeprovider.CRDStrategyName:
		ok, err := kubeprovider.ProcessCRDStrategy(ctx.KubernetesClient.KubeDynamic, instanceGroup, scalingConfigName)
		if err != nil {
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.NamespacedName(), "type", kubeprovider.CRDStrategyName, "error", err.Error())
			ctx.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to process CRD strategy")
		}
		if ok {
			break
		}
		return nil
	case RollingUpdateStrategyName:
		req := ctx.NewRollingUpdateRequest()
		ok, err := req.ProcessRollingUpgradeStrategy()
		if err != nil {
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.NamespacedName(), "type", kubeprovider.RollingUpdateStrategyName, "error", err)
			ctx.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to process rolling-update strategy")
		}
		if ok {
			break
		}
		return nil
	default:
		return errors.Errorf("'%v' is not an implemented upgrade type, will not process upgrade", strategy.GetType())
	}
	ctx.Log.Info("strategy processing completed", "instancegroup", instanceGroup.NamespacedName(), "strategy", strategy.GetType())

	if ctx.UpdateNodeReadyCondition() {
		ctx.SetState(v1alpha1.ReconcileModified)
	}
	return nil
}

func (ctx *EksInstanceGroupContext) BootstrapNodes() error {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		osFamily      = ctx.GetOsFamily()
		role          = state.GetRole()
		roleARN       = aws.StringValue(role.Arn)
	)
	ctx.Log.Info("bootstrapping arn to aws-auth", "instancegroup", instanceGroup.NamespacedName(), "arn", roleARN)

	// lock to guarantee Upsert and Remove cannot conflict when roles are shared between instancegroups
	ctx.Lock()
	defer ctx.Unlock()

	return common.UpsertAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN}, []string{osFamily})
}

// rotateWarmPool checks for drifted instances and if there are any, it deletes the warm pool
func (ctx *EksInstanceGroupContext) rotateWarmPool() (bool, error) {
	var (
		instanceGroup    = ctx.GetInstanceGroup()
		spec             = instanceGroup.GetEKSSpec()
		state            = ctx.GetDiscoveredState()
		scalingGroup     = state.GetScalingGroup()
		warmPoolConfig   = scalingGroup.WarmPoolConfiguration
		scalingGroupName = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// if spec does not configure a warm pool, or asg doesnt have a warm pool, skip this
	if !spec.HasWarmPool() || warmPoolConfig == nil {
		return false, nil
	}

	warmPoolOutput, err := ctx.AwsWorker.DescribeWarmPool(scalingGroupName)
	if err != nil {
		return true, err
	}

	driftedInstances := ctx.getDriftedInstances(warmPoolOutput.Instances)
	if len(driftedInstances) == 0 {
		return false, nil
	}

	// now that we know there are drifted instances, we can delete the warm pool
	ctx.Log.Info("found drifted instances to be", "driftedInstances", driftedInstances)

	if err := ctx.AwsWorker.DeleteWarmPool(scalingGroupName); err != nil {
		ctx.Log.Info("failed to delete warm pool", "error", err)
		return true, err
	}

	return true, nil
}

// getDriftedInstances gets all Instances that need update by checking if either LaunchConfig/ Launch Template have changed
func (ctx *EksInstanceGroupContext) getDriftedInstances(instances []*autoscaling.Instance) []string {
	var (
		needsUpdate     []string
		state           = ctx.GetDiscoveredState()
		scalingConfig   = state.GetScalingConfiguration()
		scalingResource = scalingConfig.Resource()
		scalingGroup    = state.GetScalingGroup()
	)

	for _, instance := range instances {
		var (
			instanceId = aws.StringValue(instance.InstanceId)
		)

		if awsprovider.IsUsingLaunchConfiguration(scalingGroup) {
			if instance.LaunchConfigurationName == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config       = aws.StringValue(instance.LaunchConfigurationName)
				activeConfig = aws.StringValue(scalingGroup.LaunchConfigurationName)
			)

			if !strings.EqualFold(config, activeConfig) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

		if awsprovider.IsUsingLaunchTemplate(scalingGroup) {
			if instance.LaunchTemplate == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config           = aws.StringValue(instance.LaunchTemplate.LaunchTemplateName)
				version          = aws.StringValue(instance.LaunchTemplate.Version)
				launchTemplate   = scaling.ConvertToLaunchTemplate(scalingResource)
				activeConfig     = aws.StringValue(scalingGroup.LaunchTemplate.LaunchTemplateName)
				activeVersionNum = aws.Int64Value(launchTemplate.LatestVersionNumber)
				activeVersion    = common.Int64ToStr(activeVersionNum)
			)
			if !strings.EqualFold(config, activeConfig) || !strings.EqualFold(version, activeVersion) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

		if awsprovider.IsUsingMixedInstances(scalingGroup) {
			if instance.LaunchTemplate == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config           = aws.StringValue(instance.LaunchTemplate.LaunchTemplateName)
				version          = aws.StringValue(instance.LaunchTemplate.Version)
				launchTemplate   = scaling.ConvertToLaunchTemplate(scalingResource)
				activeConfig     = aws.StringValue(scalingGroup.MixedInstancesPolicy.LaunchTemplate.LaunchTemplateSpecification.LaunchTemplateName)
				activeVersionNum = aws.Int64Value(launchTemplate.LatestVersionNumber)
				activeVersion    = common.Int64ToStr(activeVersionNum)
			)

			if !strings.EqualFold(config, activeConfig) || !strings.EqualFold(version, activeVersion) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

	}

	return needsUpdate
}

func (ctx *EksInstanceGroupContext) NewRollingUpdateRequest() *kubeprovider.RollingUpdateRequest {
	var (
		needsUpdate    []string
		allInstances   []string
		instanceGroup  = ctx.GetInstanceGroup()
		state          = ctx.GetDiscoveredState()
		scalingGroup   = state.GetScalingGroup()
		desiredCount   = int(aws.Int64Value(scalingGroup.DesiredCapacity))
		strategy       = instanceGroup.GetUpgradeStrategy().GetRollingUpdateType()
		maxUnavailable = strategy.GetMaxUnavailable()
		asgName        = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// Get all Autoscaling Instances that needs update
	needsUpdate = ctx.getDriftedInstances(scalingGroup.Instances)

	for _, instance := range scalingGroup.Instances {
		var (
			instanceId = aws.StringValue(instance.InstanceId)
		)

		allInstances = append(allInstances, instanceId)
	}

	allCount := len(allInstances)

	var unavailableInt int
	if maxUnavailable.Type == intstr.String {
		unavailableInt, _ = intstr.GetValueFromIntOrPercent(maxUnavailable, allCount, true)
	} else {
		unavailableInt = maxUnavailable.IntValue()
	}

	if unavailableInt == 0 {
		unavailableInt = 1
	}

	return &kubeprovider.RollingUpdateRequest{
		AwsWorker:        ctx.AwsWorker,
		ClusterNodes:     state.GetClusterNodes(),
		MaxUnavailable:   unavailableInt,
		DesiredCapacity:  desiredCount,
		AllInstances:     allInstances,
		UpdateTargets:    needsUpdate,
		ScalingGroupName: asgName,
	}
}