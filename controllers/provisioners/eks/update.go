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
	log "github.com/sirupsen/logrus"

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
	log.Info("checking for launch configuration drift")
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

	// update readiness conditions
	ok, err := ctx.UpdateNodeReadyCondition()
	if err != nil {
		log.Warnf("could not update instance group conditions: %v", err)
	}
	if ok {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	}

	if rotationNeeded {
		instanceGroup.SetState(v1alpha1.ReconcileInitUpgrade)
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

	log.Infof("updating scaling group %s", asgName)
	err := ctx.AwsWorker.UpdateScalingGroup(&autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(asgName),
		DesiredCapacity:         aws.Int64(spec.GetMinSize()),
		LaunchConfigurationName: aws.String(state.GetActiveLaunchConfigurationName()),
		MinSize:                 aws.Int64(spec.GetMinSize()),
		MaxSize:                 aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:       aws.String(common.ConcatonateList(configuration.GetSubnets(), ",")),
	}, tags, rmTags)
	if err != nil {
		return err
	}

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
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
	)

	if len(scalingGroup.Instances) == 0 {
		return false
	}

	for _, instance := range scalingGroup.Instances {
		if aws.StringValue(instance.LaunchConfigurationName) != state.GetActiveLaunchConfigurationName() {
			log.Info("upgrade required: scaling instances with different launch-config")
			return true
		}
	}
	return false
}

func (ctx *EksInstanceGroupContext) LaunchConfigurationDrifted() bool {
	var (
		state = ctx.GetDiscoveredState()
		// only used for comparison, no need to generate a name
		newConfig      = ctx.GetLaunchConfigurationInput("")
		existingConfig = state.GetLaunchConfiguration()
		drift          bool
	)

	if state.LaunchConfiguration == nil {
		log.Info("detected drift in launch configuration: launch config does not exist")
		return true
	}

	if aws.StringValue(existingConfig.ImageId) != aws.StringValue(newConfig.ImageId) {
		log.Infof(
			"detected drift in launch configuration: image-id has changed, %s -> %s",
			aws.StringValue(existingConfig.ImageId),
			aws.StringValue(newConfig.ImageId),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.InstanceType) != aws.StringValue(newConfig.InstanceType) {
		log.Infof(
			"detected drift in launch configuration: instance-type has changed, %s -> %s",
			aws.StringValue(existingConfig.InstanceType),
			aws.StringValue(newConfig.InstanceType),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.IamInstanceProfile) != aws.StringValue(newConfig.IamInstanceProfile) {
		log.Infof(
			"detected drift in launch configuration: instance-profile has changed, %s -> %s",
			aws.StringValue(existingConfig.IamInstanceProfile),
			aws.StringValue(newConfig.IamInstanceProfile),
		)
		drift = true
	}

	if !common.StringSliceEquals(aws.StringValueSlice(existingConfig.SecurityGroups), aws.StringValueSlice(newConfig.SecurityGroups)) {
		log.Infof(
			"detected drift in launch configuration: security-groups have changed, %v -> %v",
			aws.StringValueSlice(existingConfig.SecurityGroups),
			aws.StringValueSlice(newConfig.SecurityGroups),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.SpotPrice) != aws.StringValue(newConfig.SpotPrice) {
		log.Infof(
			"detected drift in launch configuration: spot-price has changed, '%s' -> '%s'",
			aws.StringValue(existingConfig.SpotPrice),
			aws.StringValue(newConfig.SpotPrice),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.KeyName) != aws.StringValue(newConfig.KeyName) {
		log.Infof(
			"detected drift in launch configuration: key-pair-name has changed, %s -> %s",
			aws.StringValue(existingConfig.KeyName),
			aws.StringValue(newConfig.KeyName),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.UserData) != aws.StringValue(newConfig.UserData) {
		log.Infof(
			"detected drift in launch configuration: user-data has changed, %s -> %s",
			aws.StringValue(existingConfig.UserData),
			aws.StringValue(newConfig.UserData),
		)
		drift = true
	}

	if !reflect.DeepEqual(existingConfig.BlockDeviceMappings, newConfig.BlockDeviceMappings) {
		log.Infof(
			"detected drift in launch configuration: block-device-mappings has changed;\n < %v\n---\n> %v",
			existingConfig.BlockDeviceMappings,
			newConfig.BlockDeviceMappings,
		)
		drift = true
	}

	if !drift {
		log.Info("no drift detected")
	}

	return drift
}
