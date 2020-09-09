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

	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
)

func (ctx *EksInstanceGroupContext) Update() error {
	var (
		rotationNeeded  bool
		instanceGroup   = ctx.GetInstanceGroup()
		state           = ctx.GetDiscoveredState()
		scalingConfig   = state.GetScalingConfiguration()
		configuration   = instanceGroup.GetEKSConfiguration()
		args            = ctx.GetBootstrapArgs()
		userDataPayload = ctx.GetUserDataStages()
		clusterName     = configuration.GetClusterName()
		mounts          = ctx.GetMountOpts()
		userData        = ctx.GetBasicUserData(clusterName, args, userDataPayload, mounts)
		sgs             = ctx.ResolveSecurityGroups()
		spotPrice       = configuration.GetSpotPrice()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// make sure our managed role exists if instance group has not provided one
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group role")
	}
	instanceProfile := state.GetInstanceProfile()

	config := &scaling.CreateConfigurationInput{
		IamInstanceProfileArn: aws.StringValue(instanceProfile.Arn),
		ImageId:               configuration.Image,
		InstanceType:          configuration.InstanceType,
		KeyName:               configuration.KeyPairName,
		SecurityGroups:        sgs,
		Volumes:               configuration.Volumes,
		UserData:              userData,
		SpotPrice:             spotPrice,
	}

	var configName string
	configName = scalingConfig.Name()
	// create new launchconfig if it has drifted
	if scalingConfig.Drifted(config) {
		rotationNeeded = true
		configName = fmt.Sprintf("%v-%v", ctx.ResourcePrefix, common.GetTimeString())
		config.Name = configName
		if err := scalingConfig.Create(config); err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	if ctx.RotationNeeded() {
		rotationNeeded = true
	}

	// update scaling group
	err = ctx.UpdateScalingGroup(configName)
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group")
	}

	// we should try to bootstrap the role before we wait for nodes to be ready
	// to avoid getting locked if someone made a manual change to aws-auth
	if err = ctx.BootstrapNodes(); err != nil {
		ctx.Log.Info("failed to bootstrap role, will retry", "error", err, "instancegroup", instanceGroup.GetName())
	}

	// update readiness conditions
	nodesReady := ctx.UpdateNodeReadyCondition()
	if nodesReady {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	}
	if rotationNeeded {
		instanceGroup.SetState(v1alpha1.ReconcileInitUpgrade)
	}

	return nil
}

func (ctx *EksInstanceGroupContext) UpdateScalingGroup(configName string) error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSSpec()
		status        = instanceGroup.GetStatus()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
		tags          = ctx.GetAddedTags(asgName)
		rmTags        = ctx.GetRemovedTags(asgName)
	)

	if ctx.ScalingGroupUpdateNeeded(configName) {
		err := ctx.AwsWorker.UpdateScalingGroup(&autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName:    aws.String(asgName),
			LaunchConfigurationName: aws.String(configName),
			MinSize:                 aws.Int64(spec.GetMinSize()),
			MaxSize:                 aws.Int64(spec.GetMaxSize()),
			VPCZoneIdentifier:       aws.String(common.ConcatenateList(ctx.ResolveSubnets(), ",")),
		})
		if err != nil {
			return err
		}

		ctx.Log.Info("updated scaling group", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)
	}

	status.SetActiveLaunchConfigurationName(configName)
	status.SetCurrentMin(int(spec.GetMinSize()))
	status.SetCurrentMax(int(spec.GetMaxSize()))

	if ctx.TagsUpdateNeeded() {
		err := ctx.AwsWorker.UpdateScalingGroupTags(tags, rmTags)
		if err != nil {
			return err
		}
		ctx.Log.Info("updated scaling group tags", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName)
	}

	if err := ctx.UpdateScalingProcesses(asgName); err != nil {
		return err
	}

	if err := ctx.UpdateMetricsCollection(asgName); err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) RotationNeeded() bool {
	var (
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		scalingConfig = state.GetScalingConfiguration()
		instanceGroup = ctx.GetInstanceGroup()
	)

	if len(scalingGroup.Instances) == 0 {
		return false
	}

	configName := scalingConfig.Name()
	for _, instance := range scalingGroup.Instances {
		if aws.StringValue(instance.LaunchConfigurationName) != configName {
			ctx.Log.Info("rotation needed due to launch-config diff", "instancegroup", instanceGroup.GetName(), "launchconfig", configName)
			return true
		}
	}
	return false
}

func (ctx *EksInstanceGroupContext) TagsUpdateNeeded() bool {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
		rmTags        = ctx.GetRemovedTags(asgName)
	)

	if len(rmTags) > 0 {
		return true
	}

	existingTags := make([]map[string]string, 0)
	for _, tag := range scalingGroup.Tags {
		tagSet := map[string]string{
			"key":   aws.StringValue(tag.Key),
			"value": aws.StringValue(tag.Value),
		}
		existingTags = append(existingTags, tagSet)
	}

	for _, tag := range configuration.GetTags() {
		if !common.StringMapSliceContains(existingTags, tag) {
			return true
		}
	}

	return false
}

func (ctx *EksInstanceGroupContext) ScalingGroupUpdateNeeded(configName string) bool {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		spec           = instanceGroup.GetEKSSpec()
		state          = ctx.GetDiscoveredState()
		scalingGroup   = state.GetScalingGroup()
		zoneIdentifier = aws.StringValue(scalingGroup.VPCZoneIdentifier)
		groupSubnets   = strings.Split(zoneIdentifier, ",")
		specSubnets    = ctx.ResolveSubnets()
	)

	if configName != aws.StringValue(scalingGroup.LaunchConfigurationName) {
		return true
	}

	if spec.GetMinSize() != aws.Int64Value(scalingGroup.MinSize) {
		return true
	}

	if spec.GetMaxSize() != aws.Int64Value(scalingGroup.MaxSize) {
		return true
	}

	if !common.StringSliceEqualFold(specSubnets, groupSubnets) {
		return true
	}

	return false
}

func (ctx *EksInstanceGroupContext) UpdateManagedPolicies(roleName string) error {
	var (
		instanceGroup      = ctx.GetInstanceGroup()
		state              = ctx.GetDiscoveredState()
		configuration      = instanceGroup.GetEKSConfiguration()
		additionalPolicies = configuration.GetManagedPolicies()
		needsAttach        = make([]string, 0)
		needsDetach        = make([]string, 0)
	)

	managedPolicies := ctx.GetManagedPoliciesList(additionalPolicies)
	attachedPolicies := state.GetAttachedPolicies()

	attachedArns := make([]string, 0)
	for _, p := range attachedPolicies {
		attachedArns = append(attachedArns, aws.StringValue(p.PolicyArn))
	}

	for _, policy := range managedPolicies {
		if !common.ContainsString(attachedArns, policy) {
			needsAttach = append(needsAttach, policy)
		}
	}

	if len(attachedArns) == 0 {
		needsAttach = managedPolicies
	}

	for _, policy := range attachedArns {
		if !common.ContainsString(managedPolicies, policy) {
			needsDetach = append(needsDetach, policy)
		}
	}

	err := ctx.AwsWorker.AttachManagedPolicies(roleName, needsAttach)
	if err != nil {
		return err
	}

	err = ctx.AwsWorker.DetachManagedPolicies(roleName, needsDetach)
	if err != nil {
		return err
	}

	ctx.Log.Info("updated managed policies", "instancegroup", instanceGroup.GetName(), "iamrole", roleName)
	return nil
}
