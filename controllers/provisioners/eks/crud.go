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

	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) CreateScalingGroup() error {
	var (
		asgInput      = &autoscaling.CreateAutoScalingGroupInput{}
		tags          []*autoscaling.Tag
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

	// default tags
	tags = append(tags, ctx.AwsWorker.NewTag(TagName, asgName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagKubernetesCluster, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(fmt.Sprintf(TagClusterOwnershipFmt, clusterName), TagClusterOwned, asgName))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		tags = append(tags, ctx.AwsWorker.NewTag(tagSlice["key"], tagSlice["value"], asgName))
	}

	asgInput.AutoScalingGroupName = aws.String(asgName)
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
		asgInput      = &autoscaling.UpdateAutoScalingGroupInput{}
		tags          []*autoscaling.Tag
		rmTags        []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(state.ScalingGroup.AutoScalingGroupName)
	)

	log.Infof("updating scaling group %s", asgName)

	// TODO: GetAddedTags() ; GetRemovedTags()

	// get existing tags
	var xTags []*autoscaling.Tag
	for _, xTag := range scalingGroup.Tags {
		xTags = append(xTags, ctx.AwsWorker.NewTag(aws.StringValue(xTag.Key), aws.StringValue(xTag.Value), asgName))
	}

	// default tags
	tags = append(tags, ctx.AwsWorker.NewTag(TagName, asgName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagKubernetesCluster, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName(), asgName))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		tags = append(tags, ctx.AwsWorker.NewTag(tagSlice["key"], tagSlice["value"], asgName))
	}

	// find removals
	for _, xTag := range xTags {
		var match bool
		for _, tag := range tags {
			if reflect.DeepEqual(tag, xTag) {
				match = true
			}
		}
		if !match {
			rmTags = append(rmTags, xTag)
		}
	}

	asgInput.AutoScalingGroupName = aws.String(asgName)
	asgInput.DesiredCapacity = aws.Int64(spec.GetMinSize())
	asgInput.LaunchConfigurationName = aws.String(state.GetActiveLaunchConfigurationName())
	asgInput.MinSize = aws.Int64(spec.GetMinSize())
	asgInput.MaxSize = aws.Int64(spec.GetMaxSize())
	asgInput.VPCZoneIdentifier = aws.String(common.ConcatonateList(configuration.GetSubnets(), ","))

	err := ctx.AwsWorker.UpdateScalingGroup(asgInput, tags, rmTags)
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

func (ctx *EksInstanceGroupContext) GetLaunchConfigurationInput() *autoscaling.CreateLaunchConfigurationInput {
	var (
		lcInput         = &autoscaling.CreateLaunchConfigurationInput{}
		instanceGroup   = ctx.GetInstanceGroup()
		configuration   = instanceGroup.GetEKSConfiguration()
		clusterName     = configuration.GetClusterName()
		state           = ctx.GetDiscoveredState()
		instanceProfile = state.GetInstanceProfile()
	)

	// TODO: GetBlockDeviceList() ; GetTaintList(); GetLabelList()

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

	// get list of taints
	taints := configuration.GetTaints()
	taintList := make([]string, 0)
	if len(taints) > 0 {
		for _, t := range taints {
			taintList = append(taintList, fmt.Sprintf("%v=%v:%v", t.Key, t.Value, t.Effect))
		}
	}

	// get custom labels
	customLabels := configuration.GetLabels()
	labelList := make([]string, 0)
	if len(customLabels) > 0 {
		for k, v := range customLabels {
			labelList = append(labelList, fmt.Sprintf("%v=%v", k, v))
		}
	}

	// add role label
	for _, label := range RoleLabelsFmt {
		labelList = append(labelList, fmt.Sprintf(label, instanceGroup.GetName()))
	}

	// update userdata with bootstrap arguments
	var args string
	bootstrapArgs := configuration.GetBootstrapArguments()

	labelsFlag := fmt.Sprintf("--node-labels=%v", strings.Join(labelList, ","))
	taintsFlag := fmt.Sprintf("--register-with-taints=%v", strings.Join(taintList, ","))
	args = fmt.Sprintf("--kubelet-extra-args '%v %v %v'", labelsFlag, taintsFlag, bootstrapArgs)
	userData := ctx.AwsWorker.GetBasicUserData(clusterName, args)
	name := fmt.Sprintf("%v-%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName(), common.GetTimeString())

	lcInput.LaunchConfigurationName = aws.String(name)
	lcInput.IamInstanceProfile = instanceProfile.Arn
	lcInput.ImageId = aws.String(configuration.Image)
	lcInput.InstanceType = aws.String(configuration.InstanceType)
	lcInput.KeyName = aws.String(configuration.KeyPairName)
	lcInput.SecurityGroups = aws.StringSlice(configuration.NodeSecurityGroups)
	lcInput.BlockDeviceMappings = devices
	lcInput.UserData = aws.String(userData)

	if configuration.SpotPrice != "" {
		lcInput.SpotPrice = aws.String(configuration.SpotPrice)
	}

	return lcInput
}

func (ctx *EksInstanceGroupContext) CreateLaunchConfiguration() error {
	var (
		lcInput = ctx.GetLaunchConfigurationInput()
		state   = ctx.GetDiscoveredState()
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
	if len(lcOut.LaunchConfigurations) == 1 {
		state.SetLaunchConfiguration(lcOut.LaunchConfigurations[0])
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
