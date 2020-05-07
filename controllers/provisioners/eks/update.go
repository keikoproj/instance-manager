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
		tags          = ctx.GetAddedTags()
		rmTags        = ctx.GetRemovedTags()
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
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
