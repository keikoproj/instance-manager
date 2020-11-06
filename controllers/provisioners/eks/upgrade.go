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
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (ctx *EksInstanceGroupContext) UpgradeNodes() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		strategy      = ctx.GetUpgradeStrategy()
		state         = ctx.GetDiscoveredState()
		scalingConfig = state.GetScalingConfiguration()
		strategyType  = strings.ToLower(strategy.GetType())
	)

	// process the upgrade strategy
	switch strategyType {
	case kubeprovider.CRDStrategyName:
		ok, err := kubeprovider.ProcessCRDStrategy(ctx.KubernetesClient.KubeDynamic, instanceGroup, scalingConfig.Name())
		if err != nil {
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.GetName(), "type", kubeprovider.CRDStrategyName, "error", err.Error())
			instanceGroup.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to process CRD strategy")
		}
		if ok {
			break
		}
		return nil
	case kubeprovider.RollingUpdateStrategyName:
		req := ctx.NewRollingUpdateRequest()
		ok, err := kubeprovider.ProcessRollingUpgradeStrategy(req)
		if err != nil {
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.GetName(), "type", kubeprovider.RollingUpdateStrategyName, "error", err)
			instanceGroup.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to process rolling-update strategy")
		}
		if ok {
			break
		}
		return nil
	default:
		return errors.Errorf("'%v' is not an implemented upgrade type, will not process upgrade", strategy.GetType())
	}
	ctx.Log.Info("strategy processing completed", "instancegroup", instanceGroup.GetName(), "strategy", strategy.GetType())

	if ctx.UpdateNodeReadyCondition() {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
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
	ctx.Log.Info("bootstrapping arn to aws-auth", "instancegroup", instanceGroup.GetName(), "arn", roleARN)

	// lock to guarantee Upsert and Remove cannot conflict when roles are shared between instancegroups
	ctx.Lock()
	defer ctx.Unlock()

	return common.UpsertAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN}, []string{osFamily})
}

func (ctx *EksInstanceGroupContext) NewRollingUpdateRequest() *kubeprovider.RollingUpdateRequest {
	var (
		needsUpdate        []string
		allInstances       []string
		instanceGroup      = ctx.GetInstanceGroup()
		state              = ctx.GetDiscoveredState()
		scalingGroup       = ctx.GetDiscoveredState().GetScalingGroup()
		activeLaunchConfig = aws.StringValue(scalingGroup.LaunchConfigurationName)
		desiredCount       = int(aws.Int64Value(scalingGroup.DesiredCapacity))
		strategy           = instanceGroup.GetUpgradeStrategy().GetRollingUpdateType()
		maxUnavailable     = strategy.GetMaxUnavailable()
		asgName            = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// Get all Autoscaling Instances that needs update
	for _, instance := range scalingGroup.Instances {
		allInstances = append(allInstances, aws.StringValue(instance.InstanceId))
		if aws.StringValue(instance.LaunchConfigurationName) != activeLaunchConfig {
			needsUpdate = append(needsUpdate, aws.StringValue(instance.InstanceId))
		}
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
