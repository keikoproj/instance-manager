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
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) Create() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		lcName        = state.GetActiveLaunchConfigurationName()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// no need to create a role if one is already provided
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	// create launchconfig
	if !state.HasLaunchConfiguration() {
		lcName = fmt.Sprintf("%v-%v", ctx.ResourcePrefix, common.GetTimeString())
		err := ctx.CreateLaunchConfiguration(lcName)
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	// create scaling group
	err = ctx.CreateScalingGroup(lcName)
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group")
	}

	instanceGroup.SetState(v1alpha1.ReconcileModified)
	return nil
}

func (ctx *EksInstanceGroupContext) CreateScalingGroup(lcName string) error {
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
		LaunchConfigurationName: aws.String(lcName),
		MinSize:                 aws.Int64(spec.GetMinSize()),
		MaxSize:                 aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:       aws.String(common.ConcatenateList(configuration.GetSubnets(), ",")),
		Tags:                    tags,
	})
	if err != nil {
		return err
	}
	ctx.Log.Info("created scaling group", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)

	if spec.EKSConfiguration.SuspendedProcesses != nil {
		err := ctx.AwsWorker.SetSuspendProcesses(asgName, spec.EKSConfiguration.SuspendedProcesses)
		if err != nil {
			return err
		}
		ctx.Log.Info("suspended scaling processes", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)
	}

	if err := ctx.UpdateMetricsCollection(asgName); err != nil {
		return err
	}

	state.Publisher.Publish(kubeprovider.InstanceGroupCreatedEvent, "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)
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
	status.SetActiveLaunchConfigurationName(name)
	state.SetActiveLaunchConfigurationName(name)
	return nil
}

func (ctx *EksInstanceGroupContext) CreateManagedRole() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		configuration = instanceGroup.GetEKSConfiguration()
		roleName      = ctx.ResourcePrefix
	)

	if configuration.HasExistingRole() {
		// avoid updating if using an existing role
		return nil
	}

	role, profile, err := ctx.AwsWorker.CreateScalingGroupRole(roleName)
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	err = ctx.UpdateManagedPolicies(roleName)
	if err != nil {
		return errors.Wrap(err, "failed to update managed policies")
	}

	ctx.Log.Info("reconciled managed role", "instancegroup", instanceGroup.GetName(), "iamrole", roleName)

	state.SetRole(role)
	state.SetInstanceProfile(profile)

	return nil
}
