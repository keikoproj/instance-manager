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
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Masterminds/semver"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
)

type DiscoveredState struct {
	Provisioned                   bool
	NodesReady                    bool
	ClusterNodes                  *corev1.NodeList
	OwnedScalingGroups            []*autoscaling.Group
	ScalingGroup                  *autoscaling.Group
	LaunchConfigurations          []*autoscaling.LaunchConfiguration
	LaunchConfiguration           *autoscaling.LaunchConfiguration
	ActiveLaunchConfigurationName string
	IAMRole                       *iam.Role
	InstanceProfile               *iam.InstanceProfile
	Publisher                     kubeprovider.EventPublisher
	Cluster                       *eks.Cluster
}

func (ctx *EksInstanceGroupContext) CloudDiscovery() error {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		status        = instanceGroup.GetStatus()
		clusterName   = configuration.GetClusterName()
	)

	state.Publisher = kubeprovider.EventPublisher{
		Client:          ctx.KubernetesClient.Kubernetes,
		Namespace:       instanceGroup.GetNamespace(),
		Name:            instanceGroup.GetName(),
		UID:             instanceGroup.GetUID(),
		ResourceVersion: instanceGroup.GetResourceVersion(),
	}

	nodes, err := ctx.KubernetesClient.Kubernetes.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to list cluster nodes")
	}
	state.SetClusterNodes(nodes)

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

	cluster, err := ctx.AwsWorker.DescribeEKSCluster(clusterName)
	if err == nil {
		state.SetCluster(cluster)
	} else {
		return errors.Wrap(err, "failed to describe cluster")
	}

	// find all owned scaling groups
	ownedScalingGroups := ctx.findOwnedScalingGroups(scalingGroups)
	state.SetOwnedScalingGroups(ownedScalingGroups)
	// cache the scaling group we are reconciling for if it exists
	targetScalingGroup := ctx.findTargetScalingGroup(ownedScalingGroups)

	// if there is no scaling group found, it's deprovisioned
	if targetScalingGroup == nil {
		state.SetProvisioned(false)
		return nil
	}

	launchConfigurations, err := ctx.AwsWorker.DescribeAutoscalingLaunchConfigs()
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling groups")
	}

	ctx.DiscoveredState.SetLaunchConfigurations(launchConfigurations)

	state.SetProvisioned(true)
	state.SetScalingGroup(targetScalingGroup)

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
	for _, lc := range launchConfigurations {
		lcName := aws.StringValue(lc.LaunchConfigurationName)
		if aws.StringValue(lc.LaunchConfigurationName) == aws.StringValue(targetScalingGroup.LaunchConfigurationName) {
			state.SetLaunchConfiguration(lc)
			state.SetActiveLaunchConfigurationName(lcName)
			status.SetActiveLaunchConfigurationName(lcName)
		}
	}

	// delete old launch configurations
	sortedConfigs := ctx.GetTimeSortedLaunchConfigurations()
	var deletable []*autoscaling.LaunchConfiguration
	if len(sortedConfigs) > defaultLaunchConfigurationRetention {
		d := len(sortedConfigs) - defaultLaunchConfigurationRetention
		deletable = sortedConfigs[:d]
	}

	for _, d := range deletable {
		name := aws.StringValue(d.LaunchConfigurationName)
		if strings.EqualFold(name, state.GetActiveLaunchConfigurationName()) {
			// never try to delete the active launch config
			continue
		}
		ctx.Log.Info("deleting old launch configuration", "instancegroup", instanceGroup.GetName(), "name", name)
		if err := ctx.AwsWorker.DeleteLaunchConfig(name); err != nil {
			ctx.Log.Error(err, "failed to delete launch configuration", "instancegroup", instanceGroup.GetName(), "name", name)
		}
	}

	if status.GetNodesReadyCondition() == corev1.ConditionTrue {
		state.SetNodesReady(true)
	} else {
		state.SetNodesReady(false)
	}

	err = ctx.discoverSpotPrice()
	if err != nil {
		ctx.Log.Error(err, "failed to discover spot price")
	}

	return nil
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

func (d *DiscoveredState) SetCluster(cluster *eks.Cluster) {
	d.Cluster = cluster
}
func (d *DiscoveredState) GetClusterVersion() (*semver.Version, error) {

	ver, err := semver.NewVersion(*d.Cluster.Version)
	if err != nil {
		return nil, err
	}
	return ver, nil
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
func (d *DiscoveredState) GetLaunchConfigurations() []*autoscaling.LaunchConfiguration {
	return d.LaunchConfigurations
}
func (d *DiscoveredState) SetLaunchConfigurations(configs []*autoscaling.LaunchConfiguration) {
	d.LaunchConfigurations = configs
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
func (d *DiscoveredState) SetNodesReady(condition bool) {
	d.NodesReady = condition
}
func (d *DiscoveredState) IsNodesReady() bool {
	return d.NodesReady
}
func (d *DiscoveredState) SetClusterNodes(nodes *corev1.NodeList) {
	d.ClusterNodes = nodes
}
func (d *DiscoveredState) GetClusterNodes() *corev1.NodeList {
	return d.ClusterNodes
}
