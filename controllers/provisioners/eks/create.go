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

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (ctx *EksInstanceGroupContext) Create() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// no need to create a role if one is already provided
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	// create launchconfig
	if !state.HasLaunchConfiguration() {
		err := ctx.CreateLaunchConfiguration()
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	// create scaling group
	err = ctx.CreateScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group")
	}

	instanceGroup.SetState(v1alpha1.ReconcileModified)
	return nil
}

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

func (ctx *EksInstanceGroupContext) BootstrapNodes() error {
	var (
		state   = ctx.GetDiscoveredState()
		role    = state.GetRole()
		roleARN = aws.StringValue(role.Arn)
	)

	err := common.UpsertAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN})
	if err != nil {
		return err
	}
	return nil
}
