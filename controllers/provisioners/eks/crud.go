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
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) CreateScalingGroup() error {
	var (
		asgInput      *autoscaling.CreateAutoScalingGroupInput
		tags          []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
	)

	if state.HasScalingGroup() {
		return nil
	}

	// default tags
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace()))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName()))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		for customKey, customValue := range tagSlice {
			tags = append(tags, ctx.AwsWorker.NewTag(customKey, customValue))
		}
	}

	asgInput.AutoScalingGroupName = aws.String(instanceGroup.GetName())
	asgInput.DesiredCapacity = aws.Int64(spec.GetMinSize())
	asgInput.LaunchConfigurationName = aws.String(state.GetActiveLaunchConfigurationName())
	asgInput.MinSize = aws.Int64(spec.GetMinSize())
	asgInput.MaxSize = aws.Int64(spec.GetMaxSize())
	asgInput.VPCZoneIdentifier = aws.String(common.ConcatonateList(configuration.GetSubnets(), ","))
	asgInput.Tags = tags

	err := ctx.AwsWorker.CreateScalingGroup(asgInput)
	if err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) UpdateScalingGroup() error {
	var (
		asgInput      *autoscaling.UpdateAutoScalingGroupInput
		tags          []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
	)

	// default tags
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace()))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName()))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		for customKey, customValue := range tagSlice {
			tags = append(tags, ctx.AwsWorker.NewTag(customKey, customValue))
		}
	}

	asgInput.AutoScalingGroupName = aws.String(instanceGroup.GetName())
	asgInput.DesiredCapacity = aws.Int64(spec.GetMinSize())
	asgInput.LaunchConfigurationName = aws.String(state.GetActiveLaunchConfigurationName())
	asgInput.MinSize = aws.Int64(spec.GetMinSize())
	asgInput.MaxSize = aws.Int64(spec.GetMaxSize())
	asgInput.VPCZoneIdentifier = aws.String(common.ConcatonateList(configuration.GetSubnets(), ","))

	err := ctx.AwsWorker.UpdateScalingGroup(asgInput, tags)
	if err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) GetLaunchConfigurationInput() *autoscaling.CreateLaunchConfigurationInput {
	var (
		lcInput         *autoscaling.CreateLaunchConfigurationInput
		instanceGroup   = ctx.GetInstanceGroup()
		configuration   = instanceGroup.GetEKSConfiguration()
		clusterName     = configuration.GetClusterName()
		state           = ctx.GetDiscoveredState()
		instanceProfile = state.GetInstanceProfile()
	)

	// get custom volumes or use default volume
	var devices []*autoscaling.BlockDeviceMapping
	customVolumes := configuration.GetVolumes()
	if customVolumes != nil {
		for _, v := range customVolumes {
			devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice(v.Name, v.Type, v.Size))
		}
	} else {
		devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice("/dev/xvda", "gp2", configuration.GetVolumeSize()))
	}

	// get userdata with bootstrap arguments
	var args string
	bootstrapArgs := configuration.GetBootstrapArguments()
	roleLabels := fmt.Sprintf(RoleLabelsFmt, instanceGroup.GetName(), instanceGroup.GetName())
	labelsFlag := fmt.Sprintf("--node-labels=%v", roleLabels)

	args = fmt.Sprintf("--kubelet-extra-args '%v'", labelsFlag)
	if bootstrapArgs != "" {
		args = fmt.Sprintf("--kubelet-extra-args '%v %v'", labelsFlag, bootstrapArgs)
	}
	userData := ctx.AwsWorker.GetBasicUserData(clusterName, args)

	name := fmt.Sprintf("%v-%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName(), common.GetTimeString())

	lcInput.LaunchConfigurationName = aws.String(name)
	lcInput.IamInstanceProfile = instanceProfile.InstanceProfileName
	lcInput.ImageId = aws.String(configuration.Image)
	lcInput.InstanceType = aws.String(configuration.InstanceType)
	lcInput.KeyName = aws.String(configuration.KeyPairName)
	lcInput.SpotPrice = aws.String(configuration.SpotPrice)
	lcInput.SecurityGroups = aws.StringSlice(configuration.NodeSecurityGroups)
	lcInput.BlockDeviceMappings = devices
	lcInput.UserData = aws.String(userData)

	return lcInput
}

func (ctx *EksInstanceGroupContext) CreateLaunchConfiguration() error {
	var (
		lcInput = ctx.GetLaunchConfigurationInput()
		state   = ctx.GetDiscoveredState()
	)

	err := ctx.AwsWorker.CreateLaunchConfig(lcInput)
	if err != nil {
		return err
	}
	state.SetActiveLaunchConfigurationName(aws.StringValue(lcInput.LaunchConfigurationName))

	return nil
}

func (ctx *EksInstanceGroupContext) CreateManagedRole() error {
	var (
		instanceGroup      = ctx.GetInstanceGroup()
		configuration      = instanceGroup.GetEKSConfiguration()
		clusterName        = configuration.GetClusterName()
		additionalPolicies = configuration.GetManagedPolicies()
	)

	if configuration.HasExistingRole() {
		return nil
	}

	// create a controller-owned role for the instancegroup
	roleName := fmt.Sprintf("%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName())
	managedPolicies := make([]string, 0)

	for _, name := range additionalPolicies {
		if strings.HasPrefix(name, IAMPolicyPrefix) {
			managedPolicies = append(managedPolicies, name)
		} else {
			managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", IAMPolicyPrefix, name))
		}
	}

	for _, name := range DefaultManagedPolicies {
		managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", IAMPolicyPrefix, name))
	}

	err := ctx.AwsWorker.CreateUpdateScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) DeleteScalingGroup() error {
	var (
		state   = ctx.GetDiscoveredState()
		asgName = aws.StringValue(state.ScalingGroup.AutoScalingGroupName)
	)
	if !state.HasScalingGroup() {
		return nil
	}
	err := ctx.AwsWorker.DeleteScalingGroup(asgName)
	if err != nil {
		return err
	}
	return nil
}

func (ctx *EksInstanceGroupContext) DeleteLaunchConfiguration() error {
	var (
		state  = ctx.GetDiscoveredState()
		lcName = state.ActiveLaunchConfigurationName
	)

	if !state.HasLaunchConfiguration() {
		return nil
	}

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
	)

	if !state.HasRole() || !configuration.HasExistingRole() {
		return nil
	}

	roleName := aws.StringValue(state.IAMRole.RoleName)
	managedPolicies := make([]string, 0)

	for _, name := range additionalPolicies {
		if strings.HasPrefix(name, IAMPolicyPrefix) {
			managedPolicies = append(managedPolicies, name)
		} else {
			managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", IAMPolicyPrefix, name))
		}
	}

	err := ctx.AwsWorker.DeleteScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}
	return nil
}
