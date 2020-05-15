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
	"reflect"

	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) Update() error {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		state          = ctx.GetDiscoveredState()
		oldConfigName  string
		rotationNeeded bool
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// make sure our managed role exists if instance group has not provided one
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group role")
	}

	// create new launchconfig if it has drifted
	if ctx.LaunchConfigurationDrifted() {
		rotationNeeded = true
		oldConfigName = state.GetActiveLaunchConfigurationName()
		err := ctx.CreateLaunchConfiguration()
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
		defer ctx.AwsWorker.DeleteLaunchConfig(oldConfigName)
	}

	if ctx.RotationNeeded() {
		rotationNeeded = true
	}

	// update scaling group
	err = ctx.UpdateScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group")
	}

	// we should try to bootstrap the role before we wait for nodes to be ready
	// to avoid getting locked if someone made a manual change to aws-auth
	err = ctx.BootstrapNodes()
	if err != nil {
		ctx.Log.Error(err, "failed to bootstrap role", "instancegroup", instanceGroup.GetName())
	}

	// update readiness conditions
	nodesReady := ctx.UpdateNodeReadyCondition()
	if nodesReady {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
		// only allow upgrades when all desired nodes are ready
		if rotationNeeded {
			instanceGroup.SetState(v1alpha1.ReconcileInitUpgrade)
		}
	}

	return nil
}

func (ctx *EksInstanceGroupContext) UpdateScalingGroup() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
		tags          = ctx.GetAddedTags(asgName)
		rmTags        = ctx.GetRemovedTags(asgName)
	)

	err := ctx.AwsWorker.UpdateScalingGroup(&autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(asgName),
		LaunchConfigurationName: aws.String(state.GetActiveLaunchConfigurationName()),
		MinSize:                 aws.Int64(spec.GetMinSize()),
		MaxSize:                 aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:       aws.String(common.ConcatenateList(configuration.GetSubnets(), ",")),
	}, tags, rmTags)
	if err != nil {
		return err
	}
	ctx.Log.Info("updated scaling group", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)

	out, err := ctx.AwsWorker.GetAutoscalingGroup(asgName)
	if err != nil {
		return err
	}

	if len(out.AutoScalingGroups) == 1 {
		state.SetScalingGroup(out.AutoScalingGroups[0])
	}

	return nil
}

func (ctx *EksInstanceGroupContext) RotationNeeded() bool {
	var (
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		instanceGroup = ctx.GetInstanceGroup()
	)

	if len(scalingGroup.Instances) == 0 {
		return false
	}

	for _, instance := range scalingGroup.Instances {
		if aws.StringValue(instance.LaunchConfigurationName) != state.GetActiveLaunchConfigurationName() {
			ctx.Log.Info("rotation needed due to launch-config diff", "instancegroup", instanceGroup.GetName(), "launchconfig", state.GetActiveLaunchConfigurationName())
			return true
		}
	}
	return false
}

func (ctx *EksInstanceGroupContext) LaunchConfigurationDrifted() bool {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		// only used for comparison, no need to generate a name
		newConfig      = ctx.GetLaunchConfigurationInput("")
		existingConfig = state.GetLaunchConfiguration()
		drift          bool
	)

	if state.LaunchConfiguration == nil {
		ctx.Log.Info(
			"detected drift",
			"reason", "launchconfig does not exist",
			"instancegroup", instanceGroup.GetName(),
		)
		return true
	}

	if aws.StringValue(existingConfig.ImageId) != aws.StringValue(newConfig.ImageId) {
		ctx.Log.Info(
			"detected drift",
			"reason", "image-id has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.ImageId),
			"newValue", aws.StringValue(newConfig.ImageId),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.InstanceType) != aws.StringValue(newConfig.InstanceType) {
		ctx.Log.Info(
			"detected drift",
			"reason", "instance-type has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.InstanceType),
			"newValue", aws.StringValue(newConfig.InstanceType),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.IamInstanceProfile) != aws.StringValue(newConfig.IamInstanceProfile) {
		ctx.Log.Info(
			"detected drift",
			"reason", "instance-profile has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.IamInstanceProfile),
			"newValue", aws.StringValue(newConfig.IamInstanceProfile),
		)
		drift = true
	}

	if !common.StringSliceEquals(aws.StringValueSlice(existingConfig.SecurityGroups), aws.StringValueSlice(newConfig.SecurityGroups)) {
		ctx.Log.Info(
			"detected drift",
			"reason", "security-groups has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValueSlice(existingConfig.SecurityGroups),
			"newValue", aws.StringValueSlice(newConfig.SecurityGroups),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.SpotPrice) != aws.StringValue(newConfig.SpotPrice) {
		ctx.Log.Info(
			"detected drift",
			"reason", "spot-price has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.SpotPrice),
			"newValue", aws.StringValue(newConfig.SpotPrice),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.KeyName) != aws.StringValue(newConfig.KeyName) {
		ctx.Log.Info(
			"detected drift",
			"reason", "key-pair-name has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.KeyName),
			"newValue", aws.StringValue(newConfig.KeyName),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.UserData) != aws.StringValue(newConfig.UserData) {
		ctx.Log.Info(
			"detected drift",
			"reason", "user-data has changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", aws.StringValue(existingConfig.UserData),
			"newValue", aws.StringValue(newConfig.UserData),
		)
		drift = true
	}

	if !reflect.DeepEqual(existingConfig.BlockDeviceMappings, newConfig.BlockDeviceMappings) {
		ctx.Log.Info(
			"detected drift",
			"reason", "volumes have changed",
			"instancegroup", instanceGroup.GetName(),
			"previousValue", existingConfig.BlockDeviceMappings,
			"newValue", newConfig.BlockDeviceMappings,
		)
		drift = true
	}

	if !drift {
		ctx.Log.Info("no drift detected", "instancegroup", instanceGroup.GetName())
	}

	return drift
}
