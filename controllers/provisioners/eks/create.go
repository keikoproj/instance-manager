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

	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) Create() error {
	var (
		instanceGroup   = ctx.GetInstanceGroup()
		state           = ctx.GetDiscoveredState()
		scalingConfig   = state.GetScalingConfiguration()
		configuration   = instanceGroup.GetEKSConfiguration()
		args            = ctx.GetBootstrapArgs()
		kubeletArgs     = ctx.GetKubeletExtraArgs()
		userDataPayload = ctx.GetUserDataStages()
		clusterName     = configuration.GetClusterName()
		mounts          = ctx.GetMountOpts()
		userData        = ctx.GetBasicUserData(clusterName, args, kubeletArgs, userDataPayload, mounts)
		sgs             = ctx.ResolveSecurityGroups()
		spotPrice       = configuration.GetSpotPrice()
		placement       = configuration.GetPlacement()
		metadataOptions = configuration.GetMetadataOptions()
	)

	ctx.SetState(v1alpha1.ReconcileModifying)

	// no need to create a role if one is already provided
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}
	instanceProfile := state.GetInstanceProfile()

	var configName = scalingConfig.Name()

	if common.StringEmpty(configName) {
		configName = fmt.Sprintf("%v-%v", ctx.ResourcePrefix, common.GetTimeString())
	}

	config := &scaling.CreateConfigurationInput{
		Name:                  configName,
		IamInstanceProfileArn: aws.StringValue(instanceProfile.Arn),
		ImageId:               configuration.Image,
		InstanceType:          configuration.InstanceType,
		KeyName:               configuration.KeyPairName,
		SecurityGroups:        sgs,
		Volumes:               configuration.Volumes,
		UserData:              userData,
		SpotPrice:             spotPrice,
		LicenseSpecifications: configuration.LicenseSpecifications,
		Placement:             placement,
		MetadataOptions:       metadataOptions,
	}

	if err := scalingConfig.Create(config); err != nil {
		return errors.Wrap(err, "failed to create scaling configuration")
	}

	// create scaling group
	err = ctx.CreateScalingGroup(configName)
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group")
	}

	ctx.SetState(v1alpha1.ReconcileModified)
	return nil
}

func (ctx *EksInstanceGroupContext) CreateScalingGroup(name string) error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		status        = instanceGroup.GetStatus()
		spec          = instanceGroup.GetEKSSpec()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		asgName       = ctx.ResourcePrefix
		tags          = ctx.GetAddedTags(asgName)
	)

	if state.HasScalingGroup() {
		return nil
	}

	input := &autoscaling.CreateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(spec.GetMinSize()),
		MinSize:              aws.Int64(spec.GetMinSize()),
		MaxSize:              aws.Int64(spec.GetMaxSize()),
		VPCZoneIdentifier:    aws.String(common.ConcatenateList(ctx.ResolveSubnets(), ",")),
		Tags:                 tags,
	}

	if spec.IsLaunchConfiguration() {
		input.LaunchConfigurationName = aws.String(name)
		status.SetActiveLaunchConfigurationName(name)
	}

	if spec.IsLaunchTemplate() {
		if policy := configuration.GetMixedInstancesPolicy(); policy != nil {
			input.MixedInstancesPolicy = ctx.GetDesiredMixedInstancesPolicy(name)
		} else {
			input.LaunchTemplate = &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String(name),
				Version:            aws.String("$Latest"),
			}
		}
		status.SetActiveLaunchTemplateName(name)
	}

	err := ctx.AwsWorker.CreateScalingGroup(input)
	if err != nil {
		return err
	}

	ctx.Log.Info("created scaling group", "instancegroup", instanceGroup.NamespacedName(), "scalinggroup", asgName)

	if err := ctx.UpdateScalingProcesses(asgName); err != nil {
		return err
	}

	if err := ctx.UpdateMetricsCollection(asgName); err != nil {
		return err
	}

	if err := ctx.UpdateLifecycleHooks(asgName); err != nil {
		return err
	}

	state.Publisher.Publish(kubeprovider.InstanceGroupCreatedEvent, "instancegroup", instanceGroup.NamespacedName(), "scalinggroup", asgName)
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

	if len(roleName) > 63 {
		// use a hash of the actual name in case we exceed the max length
		roleName = common.StringMD5(roleName)
	}

	role, profile, err := ctx.AwsWorker.CreateScalingGroupRole(roleName)
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	err = ctx.UpdateManagedPolicies(roleName)
	if err != nil {
		return errors.Wrap(err, "failed to update managed policies")
	}

	ctx.Log.Info("reconciled managed role", "instancegroup", instanceGroup.NamespacedName(), "iamrole", roleName)

	state.SetRole(role)
	state.SetInstanceProfile(profile)

	return nil
}
