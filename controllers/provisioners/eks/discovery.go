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
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

type DiscoveredState struct {
	Provisioned                   bool
	OwnedScalingGroups            []*autoscaling.Group
	ScalingGroup                  *autoscaling.Group
	LaunchConfiguration           *autoscaling.LaunchConfiguration
	ActiveLaunchConfigurationName string
	IAMRole                       *iam.Role
	InstanceProfile               *iam.InstanceProfile
}

func init() {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

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
	ownedScalingGroups := ctx.findOwnedScalingGroups(scalingGroups.AutoScalingGroups)
	state.SetOwnedScalingGroups(ownedScalingGroups)

	// cache the scaling group we are reconciling for if it exists
	targetScalingGroup := ctx.findTargetScalingGroup(ownedScalingGroups)
	state.SetScalingGroup(targetScalingGroup)
	status.SetActiveScalingGroupName(aws.StringValue(targetScalingGroup.AutoScalingGroupName))
	status.SetCurrentMin(int(aws.Int64Value(targetScalingGroup.MinSize)))
	status.SetCurrentMax(int(aws.Int64Value(targetScalingGroup.MaxSize)))

	if targetScalingGroup != nil {
		state.SetProvisioned(true)
	}

	// cache the launch configuration we are reconciling for if it exists
	launchConfigName := aws.StringValue(targetScalingGroup.LaunchConfigurationName)
	targetLaunchConfig, err := ctx.AwsWorker.GetAutoscalingLaunchConfig(launchConfigName)
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling launch configurations")
	}

	if len(targetLaunchConfig.LaunchConfigurations) == 1 {
		lc := targetLaunchConfig.LaunchConfigurations[0]
		state.SetLaunchConfiguration(lc)
		lcName := aws.StringValue(lc.LaunchConfigurationName)
		state.SetActiveLaunchConfigurationName(lcName)
		status.SetActiveLaunchConfigurationName(lcName)

	}

	return nil
}

func (ctx *EksInstanceGroupContext) LaunchConfigurationDrifted() bool {
	var (
		state = ctx.GetDiscoveredState()
		drift bool
	)

	if state.LaunchConfiguration == nil {
		log.Info("detected drift in launch configuration: launch config is nil")
		return true
	}

	newConfig := ctx.GetLaunchConfigurationInput()
	existingConfig := state.LaunchConfiguration

	if aws.StringValue(existingConfig.IamInstanceProfile) != aws.StringValue(newConfig.IamInstanceProfile) {
		log.Info("detected drift in launch configuration: instance-profile has changed")
		drift = true
	}

	if aws.StringValue(existingConfig.ImageId) != aws.StringValue(newConfig.ImageId) {
		log.Info("detected drift in launch configuration: image-id has changed")
		drift = true
	}

	if !reflect.DeepEqual(aws.StringValueSlice(existingConfig.SecurityGroups), aws.StringValueSlice(newConfig.SecurityGroups)) {
		log.Info("detected drift in launch configuration: security-groups have changed")
		drift = true
	}

	if aws.StringValue(existingConfig.SpotPrice) != aws.StringValue(newConfig.SpotPrice) {
		log.Info("detected drift in launch configuration: spot-price has changed")
		drift = true
	}

	if aws.StringValue(existingConfig.KeyName) != aws.StringValue(newConfig.KeyName) {
		log.Info("detected drift in launch configuration: key-pair-name has changed")
		drift = true
	}

	if aws.StringValue(existingConfig.UserData) != aws.StringValue(newConfig.UserData) {
		log.Info("detected drift in launch configuration: user-data has changed")
		drift = true
	}

	if !reflect.DeepEqual(existingConfig.BlockDeviceMappings, newConfig.BlockDeviceMappings) {
		log.Info("detected drift in launch configuration: block-device-mappings has changed")
		drift = true
	}

	return drift
}

func (ctx *EksInstanceGroupContext) findOwnedScalingGroups(groups []*autoscaling.Group) []*autoscaling.Group {
	var (
		filteredGroups []*autoscaling.Group
		instanceGroup  = ctx.GetInstanceGroup()
		configuration  = instanceGroup.GetEKSConfiguration()
		clusterName    = configuration.GetClusterName()
	)

	for _, group := range groups {
		for _, tag := range group.Tags {
			var (
				key   = aws.StringValue(tag.Key)
				value = aws.StringValue(tag.Value)
			)
			// if group has the same cluster tag it's owned by the controller
			if key == TagClusterName && strings.ToLower(value) == strings.ToLower(clusterName) {
				filteredGroups = append(filteredGroups, group)
			}
		}
	}
	return filteredGroups
}

func (ctx *EksInstanceGroupContext) findTargetScalingGroup(groups []*autoscaling.Group) *autoscaling.Group {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		nameMatch      bool
		namespaceMatch bool
	)

	for _, group := range groups {
		for _, tag := range group.Tags {
			var (
				key   = aws.StringValue(tag.Key)
				value = aws.StringValue(tag.Value)
			)
			// must match both name and namespace tag
			if key == TagInstanceGroupName && value == instanceGroup.GetName() {
				nameMatch = true
			}
			if key == TagInstanceGroupNamespace && value == instanceGroup.GetNamespace() {
				namespaceMatch = true
			}
		}
		if nameMatch && namespaceMatch {
			return group
		}
	}

	return nil
}

func (d *DiscoveredState) SetScalingGroup(asg *autoscaling.Group) {
	d.ScalingGroup = asg
}

func (d *DiscoveredState) GetScalingGroup() *autoscaling.Group {
	return d.ScalingGroup
}

func (d *DiscoveredState) SetOwnedScalingGroups(groups []*autoscaling.Group) {
	d.OwnedScalingGroups = groups
}

func (d *DiscoveredState) GetOwnedScalingGroups() []*autoscaling.Group {
	return d.OwnedScalingGroups
}

func (d *DiscoveredState) SetLaunchConfiguration(lc *autoscaling.LaunchConfiguration) {
	d.LaunchConfiguration = lc
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

func (d *DiscoveredState) HasScalingGroup() bool {
	return d.ScalingGroup != nil
}

func (d *DiscoveredState) SetRole(role *iam.Role) {
	d.IAMRole = role
}

func (d *DiscoveredState) SetInstanceProfile(profile *iam.InstanceProfile) {
	d.InstanceProfile = profile
}

func (d *DiscoveredState) GetInstanceProfile() *iam.InstanceProfile {
	return d.InstanceProfile
}

func (d *DiscoveredState) GetRole() *iam.Role {
	return d.IAMRole
}

func (d *DiscoveredState) SetProvisioned(provisioned bool) {
	d.Provisioned = provisioned
}

func (d *DiscoveredState) IsProvisioned() bool {
	return d.Provisioned
}
