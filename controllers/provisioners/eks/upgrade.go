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
)

func (ctx *EksInstanceGroupContext) UpgradeNodes() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		strategy      = ctx.GetUpgradeStrategy()
	)

	// process the upgrade strategy
	switch strings.ToLower(strategy.GetType()) {
	case kubeprovider.CRDStrategyName:
		crdStrategy := strategy.GetCRDType()
		if err := crdStrategy.Validate(); err != nil {
			instanceGroup.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to validate strategy spec")
		}
		ok, err := kubeprovider.ProcessCRDStrategy(ctx.KubernetesClient.KubeDynamic, instanceGroup)
		if err != nil {
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
			instanceGroup.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to process CRD strategy")
		}
		if ok {
			break
		}
		return nil
	default:
		return errors.Errorf("'%v' is not an implemented upgrade type, will not process upgrade", strategy.GetType())
	}

	if ctx.UpdateNodeReadyCondition() {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	}

	return nil
}

func (ctx *EksInstanceGroupContext) BootstrapNodes() error {
	var (
		state   = ctx.GetDiscoveredState()
		role    = state.GetRole()
		roleARN = aws.StringValue(role.Arn)
	)

	err := common.UpsertAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN})
	if err != nil {
		return err
	}
	return nil
}
