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
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
)

func (ctx *EksInstanceGroupContext) Delete() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		role          = state.GetRole()
		roleARN       = aws.StringValue(role.Arn)
	)

	instanceGroup.SetState(v1alpha1.ReconcileDeleting)
	// delete scaling group
	err := ctx.DeleteScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to delete scaling group")
	}

	// if scaling group is deleted, defer removal from aws-auth
	defer common.RemoveAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN})

	// delete launchconfig
	err = ctx.DeleteLaunchConfiguration()
	if err != nil {
		return errors.Wrap(err, "failed to delete launch configuration")
	}

	// delete the managed IAM role if one was created
	err = ctx.DeleteManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to delete scaling group role")
	}

	return nil
}

func (ctx *EksInstanceGroupContext) DeleteScalingGroup() error {
	var (
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
		asgName      = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)
	if !state.HasScalingGroup() {
		return nil
	}
	log.Infof("deleting scaling group %s", asgName)

	err := ctx.AwsWorker.DeleteScalingGroup(asgName)
	if err != nil {
		return err
	}
	return nil
}

func (ctx *EksInstanceGroupContext) DeleteLaunchConfiguration() error {
	var (
		state  = ctx.GetDiscoveredState()
		lcName = state.GetActiveLaunchConfigurationName()
	)

	if !state.HasLaunchConfiguration() {
		return nil
	}
	log.Infof("deleting launch configuration %s", lcName)

	err := ctx.AwsWorker.DeleteLaunchConfig(lcName)
	if err != nil {
		return err
	}
	return nil
}

func (ctx *EksInstanceGroupContext) DeleteManagedRole() error {
	var (
		instanceGroup      = ctx.GetInstanceGroup()
		configuration      = instanceGroup.GetEKSConfiguration()
		state              = ctx.GetDiscoveredState()
		additionalPolicies = configuration.GetManagedPolicies()
		role               = state.GetRole()
		roleName           = aws.StringValue(role.RoleName)
	)

	if !state.HasRole() || configuration.HasExistingRole() {
		return nil
	}

	log.Infof("deleting managed role %s", roleName)

	managedPolicies := ctx.GetManagedPoliciesList(additionalPolicies)

	err := ctx.AwsWorker.DeleteScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}
	return nil
}
