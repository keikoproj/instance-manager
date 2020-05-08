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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func (ctx *EksInstanceGroupContext) CloudDiscovery() error {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		status        = instanceGroup.GetStatus()
		clusterName   = configuration.GetClusterName()
	)

	var roleName, instanceProfileName string
	if configuration.HasExistingRole() {
		roleName = configuration.GetRoleName()
		instanceProfileName = configuration.GetInstanceProfileName()
	} else {
		roleName = fmt.Sprintf("%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName())
		instanceProfileName = fmt.Sprintf("%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName())
	}

	// cache the instancegroup IAM role if it exists
	if val, ok := ctx.AwsWorker.RoleExist(roleName); ok {
		state.SetRole(val)
		status.SetNodesArn(aws.StringValue(val.Arn))
	}

	if val, ok := ctx.AwsWorker.InstanceProfileExist(instanceProfileName); ok {
		state.SetInstanceProfile(val)
	}

	scalingGroups, err := ctx.AwsWorker.DescribeAutoscalingGroups()
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling groups")
	}

	// find all owned scaling groups
	ownedScalingGroups := ctx.findOwnedScalingGroups(scalingGroups)
	state.SetOwnedScalingGroups(ownedScalingGroups)

	// cache the scaling group we are reconciling for if it exists
	targetScalingGroup := ctx.findTargetScalingGroup(ownedScalingGroups)

	// if there is no scaling group found, it's deprovisioned
	if targetScalingGroup == nil {
		state.SetProvisioned(false)
		// no need to look for launch configurations at this point
		return nil
	}
	state.SetProvisioned(true)
	state.SetScalingGroup(targetScalingGroup)

	err = ctx.discoverSpotPrice()
	if err != nil {
		log.Warnf("failed to discover spot price: %v", err)
	}

	// update status with scaling group info
	status.SetActiveScalingGroupName(aws.StringValue(targetScalingGroup.AutoScalingGroupName))
	status.SetCurrentMin(int(aws.Int64Value(targetScalingGroup.MinSize)))
	status.SetCurrentMax(int(aws.Int64Value(targetScalingGroup.MaxSize)))
	if configuration.GetSpotPrice() == "" {
		status.SetLifecycle(v1alpha1.LifecycleStateNormal)
	} else {
		status.SetLifecycle(v1alpha1.LifecycleStateSpot)
	}

	// cache the launch configuration we are reconciling for if it exists
	launchConfigName := aws.StringValue(targetScalingGroup.LaunchConfigurationName)
	if launchConfigName != "" {
		targetLaunchConfig, err := ctx.AwsWorker.GetAutoscalingLaunchConfig(launchConfigName)
		if err != nil {
			return errors.Wrap(err, "failed to describe autoscaling launch configurations")
		}

		// there can only be 1 launch config since we're searching by name
		if len(targetLaunchConfig.LaunchConfigurations) != 1 {
			return nil
		}

		var lc = targetLaunchConfig.LaunchConfigurations[0]
		var lcName = aws.StringValue(lc.LaunchConfigurationName)

		state.SetLaunchConfiguration(lc)
		state.SetActiveLaunchConfigurationName(lcName)
		status.SetActiveLaunchConfigurationName(lcName)
	}

	return nil
}

type DiscoveredState struct {
	Provisioned                   bool
	OwnedScalingGroups            []*autoscaling.Group
	ScalingGroup                  *autoscaling.Group
	LaunchConfiguration           *autoscaling.LaunchConfiguration
	ActiveLaunchConfigurationName string
	IAMRole                       *iam.Role
	InstanceProfile               *iam.InstanceProfile
}

func (d *DiscoveredState) SetScalingGroup(asg *autoscaling.Group) {
	if asg != nil {
		d.ScalingGroup = asg
	}
}
func (d *DiscoveredState) GetScalingGroup() *autoscaling.Group {
	if d.ScalingGroup != nil {
		return d.ScalingGroup
	}
	return &autoscaling.Group{}
}
func (d *DiscoveredState) SetOwnedScalingGroups(groups []*autoscaling.Group) {
	d.OwnedScalingGroups = groups
}
func (d *DiscoveredState) GetOwnedScalingGroups() []*autoscaling.Group {
	return d.OwnedScalingGroups
}
func (d *DiscoveredState) SetLaunchConfiguration(lc *autoscaling.LaunchConfiguration) {
	if lc != nil {
		d.LaunchConfiguration = lc
	}
}
func (d *DiscoveredState) GetLaunchConfiguration() *autoscaling.LaunchConfiguration {
	return d.LaunchConfiguration
}
func (d *DiscoveredState) SetActiveLaunchConfigurationName(name string) {
	d.ActiveLaunchConfigurationName = name
}
func (d *DiscoveredState) GetActiveLaunchConfigurationName() string {
	return d.ActiveLaunchConfigurationName
}
func (d *DiscoveredState) HasLaunchConfiguration() bool {
	return d.LaunchConfiguration != nil
}
func (d *DiscoveredState) HasRole() bool {
	return d.IAMRole != nil
}
func (d *DiscoveredState) HasInstanceProfile() bool {
	return d.InstanceProfile != nil
}
func (d *DiscoveredState) HasScalingGroup() bool {
	return d.ScalingGroup != nil
}
func (d *DiscoveredState) SetRole(role *iam.Role) {
	if role != nil {
		d.IAMRole = role
	}
}
func (d *DiscoveredState) SetInstanceProfile(profile *iam.InstanceProfile) {
	if profile != nil {
		d.InstanceProfile = profile
	}
}
func (d *DiscoveredState) GetInstanceProfile() *iam.InstanceProfile {
	if d.InstanceProfile != nil {
		return d.InstanceProfile
	}
	return &iam.InstanceProfile{}
}
func (d *DiscoveredState) GetRole() *iam.Role {
	if d.IAMRole != nil {
		return d.IAMRole
	}
	return &iam.Role{}
}
func (d *DiscoveredState) SetProvisioned(provisioned bool) {
	d.Provisioned = provisioned
}
func (d *DiscoveredState) IsProvisioned() bool {
	return d.Provisioned
}
