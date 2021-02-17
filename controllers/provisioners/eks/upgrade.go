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
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"

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

	// process the upgrade strategy
	switch strategyType {
	case kubeprovider.CRDStrategyName:
		ok, err := kubeprovider.ProcessCRDStrategy(ctx.KubernetesClient.KubeDynamic, instanceGroup, scalingConfigName)
		if err != nil {
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.GetName(), "type", kubeprovider.CRDStrategyName, "error", err.Error())
			instanceGroup.SetState(v1alpha1.ReconcileErr)
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
			state.Publisher.Publish(kubeprovider.InstanceGroupUpgradeFailedEvent, "instancegroup", instanceGroup.GetName(), "type", RollingUpdateStrategyName, "error", err)
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
