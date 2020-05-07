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
	"sort"
	"strings"

	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx EksInstanceGroupContext) CreateScalingGroup() error {
	var (
		tags          = ctx.GetAddedTags()
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
		asgName       = fmt.Sprintf("%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName())
	)

	if state.HasScalingGroup() {
		return nil
	}

	log.Infof("creating scaling group %s", asgName)
	err := ctx.AwsWorker.CreateScalingGroup(&autoscaling.CreateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(asgName),
		DesiredCapacity:         aws.Int64(spec.GetMinSize()),
		LaunchConfigurationName: aws.String(state.GetActiveLaunchConfigurationName()),
		MinSize:                 aws.Int64(spec.GetMinSize()),
		MaxSize:                 aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:       aws.String(common.ConcatonateList(configuration.GetSubnets(), ",")),
		Tags:                    tags,
	})
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

func (ctx *EksInstanceGroupContext) DeleteScalingGroup() error {
	var (
		state   = ctx.GetDiscoveredState()
		asgName = aws.StringValue(state.ScalingGroup.AutoScalingGroupName)
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

func (ctx *EksInstanceGroupContext) CreateLaunchConfiguration() error {
	var (
		lcInput       = ctx.GetLaunchConfigurationInput()
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		status        = instanceGroup.GetStatus()
	)

	lcName := aws.StringValue(lcInput.LaunchConfigurationName)
	log.Infof("creating new launch configuration %s", lcName)

	err := ctx.AwsWorker.CreateLaunchConfig(lcInput)
	if err != nil {
		return err
	}

	lcOut, err := ctx.AwsWorker.GetAutoscalingLaunchConfig(lcName)
	if err != nil {
		return err
	}

	state.SetActiveLaunchConfigurationName(lcName)
	status.SetActiveLaunchConfigurationName(lcName)
	if len(lcOut.LaunchConfigurations) == 1 {
		state.SetLaunchConfiguration(lcOut.LaunchConfigurations[0])
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

func (ctx *EksInstanceGroupContext) CreateManagedRole() error {
	var (
		instanceGroup      = ctx.GetInstanceGroup()
		state              = ctx.GetDiscoveredState()
		configuration      = instanceGroup.GetEKSConfiguration()
		clusterName        = configuration.GetClusterName()
		additionalPolicies = configuration.GetManagedPolicies()
		roleName           = fmt.Sprintf("%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName())
	)

	if configuration.HasExistingRole() {
		return nil
	}

	// create a controller-owned role for the instancegroup
	log.Infof("updating managed role %s", roleName)
	managedPolicies := make([]string, 0)
	for _, name := range additionalPolicies {
		if strings.HasPrefix(name, awsprovider.IAMPolicyPrefix) {
			managedPolicies = append(managedPolicies, name)
		} else {
			managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
		}
	}

	for _, name := range DefaultManagedPolicies {
		managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
	}

	role, profile, err := ctx.AwsWorker.CreateUpdateScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}

	state.SetRole(role)
	state.SetInstanceProfile(profile)

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

	managedPolicies := make([]string, 0)
	for _, name := range additionalPolicies {
		if strings.HasPrefix(name, awsprovider.IAMPolicyPrefix) {
			managedPolicies = append(managedPolicies, name)
		} else {
			managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
		}
	}

	for _, name := range DefaultManagedPolicies {
		managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
	}

	err := ctx.AwsWorker.DeleteScalingGroupRole(roleName, managedPolicies)
	if err != nil {
		return err
	}
	return nil
}

func (ctx *EksInstanceGroupContext) GetLaunchConfigurationInput() *autoscaling.CreateLaunchConfigurationInput {
	var (
		instanceGroup   = ctx.GetInstanceGroup()
		configuration   = instanceGroup.GetEKSConfiguration()
		clusterName     = configuration.GetClusterName()
		state           = ctx.GetDiscoveredState()
		instanceProfile = state.GetInstanceProfile()
		devices         = ctx.GetBlockDeviceList()
		args            = ctx.GetBootstrapArgs()
		userData        = ctx.AwsWorker.GetBasicUserData(clusterName, args)
	)

	name := fmt.Sprintf("%v-%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName(), common.GetTimeString())
	input := &autoscaling.CreateLaunchConfigurationInput{
		LaunchConfigurationName: aws.String(name),
		IamInstanceProfile:      instanceProfile.Arn,
		ImageId:                 aws.String(configuration.Image),
		InstanceType:            aws.String(configuration.InstanceType),
		KeyName:                 aws.String(configuration.KeyPairName),
		SecurityGroups:          aws.StringSlice(configuration.NodeSecurityGroups),
		BlockDeviceMappings:     devices,
		UserData:                aws.String(userData),
	}

	if configuration.SpotPrice != "" {
		input.SpotPrice = aws.String(configuration.SpotPrice)
	}

	return input
}

func (ctx *EksInstanceGroupContext) GetAddedTags() []*autoscaling.Tag {
	var (
		tags          []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	tags = append(tags, ctx.AwsWorker.NewTag(TagName, asgName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagKubernetesCluster, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName(), asgName))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		tags = append(tags, ctx.AwsWorker.NewTag(tagSlice["key"], tagSlice["value"], asgName))
	}
	return tags
}

func (ctx *EksInstanceGroupContext) GetRemovedTags() []*autoscaling.Tag {
	var (
		existingTags []*autoscaling.Tag
		removal      []*autoscaling.Tag
		addedTags    = ctx.GetAddedTags()
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
		asgName      = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// get existing tags
	for _, tag := range scalingGroup.Tags {
		existingTags = append(existingTags, ctx.AwsWorker.NewTag(aws.StringValue(tag.Key), aws.StringValue(tag.Value), asgName))
	}

	// find removals against incoming tags
	for _, tag := range existingTags {
		var match bool
		for _, t := range addedTags {
			if reflect.DeepEqual(t, tag) {
				match = true
			}
		}
		if !match {
			removal = append(removal, tag)
		}
	}
	return removal
}

func (ctx *EksInstanceGroupContext) GetBlockDeviceList() []*autoscaling.BlockDeviceMapping {
	var (
		devices       []*autoscaling.BlockDeviceMapping
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
	)

	customVolumes := configuration.GetVolumes()
	if customVolumes != nil {
		for _, v := range customVolumes {
			devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice(v.Name, v.Type, v.Size))
		}
	} else {
		devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice("/dev/xvda", "gp2", configuration.GetVolumeSize()))
	}
	return devices
}

func (ctx *EksInstanceGroupContext) GetTaintList() []string {
	var (
		taintList     []string
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		taints        = configuration.GetTaints()
	)

	if len(taints) > 0 {
		for _, t := range taints {
			taintList = append(taintList, fmt.Sprintf("%v=%v:%v", t.Key, t.Value, t.Effect))
		}
	}
	sort.Strings(taintList)
	return taintList
}

func (ctx *EksInstanceGroupContext) GetLabelList() []string {
	var (
		labelList     []string
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		customLabels  = configuration.GetLabels()
	)
	// get custom labels
	if len(customLabels) > 0 {
		for k, v := range customLabels {
			labelList = append(labelList, fmt.Sprintf("%v=%v", k, v))
		}
	}
	sort.Strings(labelList)

	// add role label
	for _, label := range RoleLabelsFmt {
		labelList = append(labelList, fmt.Sprintf(label, instanceGroup.GetName()))
	}
	return labelList
}

func (ctx *EksInstanceGroupContext) GetBootstrapArgs() string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		bootstrapArgs = configuration.GetBootstrapArguments()
	)
	labelsFlag := fmt.Sprintf("--node-labels=%v", strings.Join(ctx.GetLabelList(), ","))
	taintsFlag := fmt.Sprintf("--register-with-taints=%v", strings.Join(ctx.GetTaintList(), ","))
	return fmt.Sprintf("--kubelet-extra-args '%v %v %v'", labelsFlag, taintsFlag, bootstrapArgs)
}
