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
	"fmt"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) Create() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// no need to create a role if one is already provided
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	// create launchconfig
	if !state.HasLaunchConfiguration() {
		lcName := fmt.Sprintf("%v-%v", ctx.ResourcePrefix, common.GetTimeString())
		err := ctx.CreateLaunchConfiguration(lcName)
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	// create scaling group
	err = ctx.CreateScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group")
	}

	instanceGroup.SetState(v1alpha1.ReconcileModified)
	return nil
}

func (ctx *EksInstanceGroupContext) CreateScalingGroup() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		asgName       = ctx.ResourcePrefix
		tags          = ctx.GetAddedTags(asgName)
	)

	if state.HasScalingGroup() {
		return nil
	}

	err := ctx.AwsWorker.CreateScalingGroup(&autoscaling.CreateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(asgName),
		DesiredCapacity:         aws.Int64(spec.GetMinSize()),
		LaunchConfigurationName: aws.String(state.GetActiveLaunchConfigurationName()),
		MinSize:                 aws.Int64(spec.GetMinSize()),
		MaxSize:                 aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:       aws.String(common.ConcatenateList(configuration.GetSubnets(), ",")),
		Tags:                    tags,
	})
	if err != nil {
		return err
	}
	ctx.Log.Info("created scaling group", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)

	scalingGroup, err := ctx.AwsWorker.GetAutoscalingGroup(asgName)
	if err != nil {
		return err
	}

	if scalingGroup != nil {
		state.SetScalingGroup(scalingGroup)
	}

	return nil
}

func (ctx *EksInstanceGroupContext) CreateLaunchConfiguration(name string) error {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		status        = instanceGroup.GetStatus()
		input         = ctx.GetLaunchConfigurationInput(name)
	)

	if aws.StringValue(input.IamInstanceProfile) == "" {
		return errors.Errorf("cannot create a launchconfiguration without iam instance profile")
	}

	err := ctx.AwsWorker.CreateLaunchConfig(input)
	if err != nil {
		return err
	}

	ctx.Log.Info("created launchconfig", "instancegroup", instanceGroup.GetName(), "launchconfig", name)
	lc, err := ctx.AwsWorker.GetAutoscalingLaunchConfig(name)
	if err != nil {
		return err
	}

	if lc != nil {
		status.SetActiveLaunchConfigurationName(name)
		state.SetActiveLaunchConfigurationName(name)
		state.SetLaunchConfiguration(lc)
	}

	return nil
}

func (ctx *EksInstanceGroupContext) CreateManagedRole() error {
	var (
		instanceGroup      = ctx.GetInstanceGroup()
		state              = ctx.GetDiscoveredState()
		configuration      = instanceGroup.GetEKSConfiguration()
		additionalPolicies = configuration.GetManagedPolicies()
		roleName           = ctx.ResourcePrefix
	)

	if configuration.HasExistingRole() {
		return nil
	}

	// create a controller-owned role for the instancegroup
	managedPolicies := ctx.GetManagedPoliciesList(additionalPolicies)

	role, profile, err := ctx.AwsWorker.CreateUpdateScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}
	ctx.Log.Info("created managed role", "instancegroup", instanceGroup.GetName(), "iamrole", roleName)

	state.SetRole(role)
	state.SetInstanceProfile(profile)

	return nil
}
