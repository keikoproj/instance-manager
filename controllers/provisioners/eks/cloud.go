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
	"regexp"
	"strconv"
	"strings"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
)

type DiscoveredState struct {
	Provisioned          bool
	NodesReady           bool
	ClusterNodes         *corev1.NodeList
	OwnedScalingGroups   []*autoscaling.Group
	ScalingGroup         *autoscaling.Group
	LifecycleHooks       []*autoscaling.LifecycleHook
	ScalingConfiguration scaling.Configuration
	IAMRole              *iam.Role
	AttachedPolicies     []*iam.AttachedPolicy
	InstanceProfile      *iam.InstanceProfile
	Publisher            kubeprovider.EventPublisher
	Cluster              *eks.Cluster
	VPCId                string
	SubFamilyFlexible    InstancePool
}

func (ctx *EksInstanceGroupContext) CloudDiscovery() error {
	var (
		state          = ctx.GetDiscoveredState()
		instanceGroup  = ctx.GetInstanceGroup()
		spec           = instanceGroup.GetEKSSpec()
		configuration  = instanceGroup.GetEKSConfiguration()
		mixedInstances = configuration.GetMixedInstancesPolicy()
		status         = instanceGroup.GetStatus()
		clusterName    = configuration.GetClusterName()
	)

	state.Publisher = kubeprovider.EventPublisher{
		Client:          ctx.KubernetesClient.Kubernetes,
		Namespace:       instanceGroup.GetNamespace(),
		Name:            instanceGroup.GetName(),
		UID:             instanceGroup.GetUID(),
		ResourceVersion: instanceGroup.GetResourceVersion(),
	}

	if ok := spec.IsLaunchConfiguration(); ok {
		state.ScalingConfiguration = &scaling.LaunchConfiguration{
			AwsWorker: ctx.AwsWorker,
		}
	}
	if ok := spec.IsLaunchTemplate(); ok {
		state.ScalingConfiguration = &scaling.LaunchTemplate{
			AwsWorker: ctx.AwsWorker,
		}
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

		if !configuration.HasExistingRole() {
			policies, err := ctx.AwsWorker.ListRolePolicies(roleName)
			if err != nil {
				return errors.Wrap(err, "failed to list attached role policies")
			}
			state.SetAttachedPolicies(policies)
		}
	}

	if val, ok := ctx.AwsWorker.InstanceProfileExist(instanceProfileName); ok {
		state.SetInstanceProfile(val)
	}

	scalingGroups, err := ctx.AwsWorker.DescribeAutoscalingGroups()
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling groups")
	}

	cluster, err := ctx.AwsWorker.DescribeEKSCluster(clusterName)
	if err != nil {
		return errors.Wrap(err, "failed to describe cluster")
	}
	state.SetCluster(cluster)

	vpcId := aws.StringValue(cluster.ResourcesVpcConfig.VpcId)
	state.SetVPCId(vpcId)

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

	state.SetProvisioned(true)
	state.SetScalingGroup(targetScalingGroup)
	asgName := aws.StringValue(targetScalingGroup.AutoScalingGroupName)

	state.LifecycleHooks, err = ctx.AwsWorker.DescribeLifecycleHooks(asgName)
	if err != nil {
		return errors.Wrap(err, "failed to describe lifecycle hooks")
	}
	// update status with scaling group info
	status.SetActiveScalingGroupName(asgName)
	status.SetCurrentMin(int(aws.Int64Value(targetScalingGroup.MinSize)))
	status.SetCurrentMax(int(aws.Int64Value(targetScalingGroup.MaxSize)))

	if ok := spec.IsLaunchConfiguration(); ok {
		input := &scaling.DiscoverConfigurationInput{
			ScalingGroup: targetScalingGroup,
		}
		state.ScalingConfiguration, err = scaling.NewLaunchConfiguration(instanceGroup.NamespacedName(), ctx.AwsWorker, input)
		if err != nil {
			return errors.Wrap(err, "failed to discover launch configurations")
		}
		status.SetActiveLaunchConfigurationName(state.ScalingConfiguration.Name())
	}

	if ok := spec.IsLaunchTemplate(); ok {
		input := &scaling.DiscoverConfigurationInput{
			ScalingGroup: targetScalingGroup,
		}
		offerings, err := ctx.AwsWorker.DescribeInstanceOfferings()
		if err != nil {
			return errors.Wrap(err, "failed to discover launch templates")
		}
		instanceTypes, err := ctx.AwsWorker.DescribeInstanceTypes()
		if err != nil {
			return errors.Wrap(err, "failed to discover launch templates")
		}
		pool := subFamilyFlexiblePool(offerings, instanceTypes)
		state.SetSubFamilyFlexiblePool(pool)
		state.ScalingConfiguration, err = scaling.NewLaunchTemplate(instanceGroup.NamespacedName(), ctx.AwsWorker, input)
		if err != nil {
			return errors.Wrap(err, "failed to discover launch templates")
		}
		status.SetActiveLaunchTemplateName(state.ScalingConfiguration.Name())
		resource := state.ScalingConfiguration.Resource()
		var latestVersion int64
		if lt, ok := resource.(*ec2.LaunchTemplate); ok && lt != nil {
			latestVersion = aws.Int64Value(lt.LatestVersionNumber)
			versionString := strconv.FormatInt(latestVersion, 10)
			status.SetLatestTemplateVersion(versionString)
		}
	}

	// delete old launch configurations
	state.ScalingConfiguration.Delete(&scaling.DeleteConfigurationInput{
		Name:           state.ScalingConfiguration.Name(),
		Prefix:         ctx.ResourcePrefix,
		DeleteAll:      false,
		RetainVersions: ctx.ConfigRetention,
	})

	if status.GetNodesReadyCondition() == corev1.ConditionTrue {
		state.SetNodesReady(true)
	} else {
		state.SetNodesReady(false)
	}

	err = ctx.discoverSpotPrice()
	if err != nil {
		ctx.Log.Error(err, "failed to discover spot price")
	}

	if mixedInstances != nil {
		ratio := mixedInstances.SpotRatio.IntValue()
		if ratio < 100 {
			status.SetLifecycle(v1alpha1.LifecycleStateMixed)
		} else {
			status.SetLifecycle(v1alpha1.LifecycleStateNormal)
		}
	} else if configuration.GetSpotPrice() != "" {
		status.SetLifecycle(v1alpha1.LifecycleStateSpot)
	} else {
		status.SetLifecycle(v1alpha1.LifecycleStateNormal)
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

func (d *DiscoveredState) SetVPCId(id string) {
	d.VPCId = id
}

func (d *DiscoveredState) GetVPCId() string {
	return d.VPCId
}

func (d *DiscoveredState) GetClusterVersion() string {
	if d.Cluster == nil {
		return ""
	}
	return aws.StringValue(d.Cluster.Version)
}

func (d *DiscoveredState) SetOwnedScalingGroups(groups []*autoscaling.Group) {
	d.OwnedScalingGroups = groups
}
func (d *DiscoveredState) GetOwnedScalingGroups() []*autoscaling.Group {
	return d.OwnedScalingGroups
}
func (d *DiscoveredState) GetScalingConfiguration() scaling.Configuration {
	return d.ScalingConfiguration
}
func (d *DiscoveredState) SetAttachedPolicies(policies []*iam.AttachedPolicy) {
	d.AttachedPolicies = policies
}
func (d *DiscoveredState) GetAttachedPolicies() []*iam.AttachedPolicy {
	if d.AttachedPolicies == nil {
		d.AttachedPolicies = []*iam.AttachedPolicy{}
	}
	return d.AttachedPolicies
}
func (d *DiscoveredState) HasRole() bool {
	return d.IAMRole != nil
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

func (d *DiscoveredState) SetSubFamilyFlexiblePool(pool map[string][]InstanceSpec) {
	d.SubFamilyFlexible = InstancePool{
		Type: SubFamilyFlexible,
		Pool: pool,
	}
}

func subFamilyFlexiblePool(offerings []*ec2.InstanceTypeOffering, typeInfo []*ec2.InstanceTypeInfo) map[string][]InstanceSpec {
	pool := make(map[string][]InstanceSpec, 0)
	for _, t := range offerings {
		offeringType := aws.StringValue(t.InstanceType)
		desiredFamily, desiredGeneration := getInstanceTypeFamilyGeneration(offeringType)
		pool[offeringType] = make([]InstanceSpec, 0)
		pool[offeringType] = append(pool[offeringType], InstanceSpec{Type: offeringType, Weight: "1"})
		cpu, mem := getOfferingSpec(typeInfo, offeringType)
		for _, i := range typeInfo {
			iType := aws.StringValue(i.InstanceType)
			family, generation := getInstanceTypeFamilyGeneration(iType)
			if !strings.EqualFold(family, desiredFamily) {
				continue
			}
			if !strings.EqualFold(generation, desiredGeneration) {
				continue
			}
			if strings.EqualFold(offeringType, iType) {
				continue
			}
			if cpu == aws.Int64Value(i.VCpuInfo.DefaultVCpus) && mem == aws.Int64Value(i.MemoryInfo.SizeInMiB) {
				pool[offeringType] = append(pool[offeringType], InstanceSpec{Type: iType, Weight: "1"})
			}
		}
	}
	return pool
}

func getOfferingSpec(typeInfo []*ec2.InstanceTypeInfo, instanceType string) (int64, int64) {
	for _, i := range typeInfo {
		t := aws.StringValue(i.InstanceType)
		if strings.EqualFold(instanceType, t) {
			return aws.Int64Value(i.VCpuInfo.DefaultVCpus), aws.Int64Value(i.MemoryInfo.SizeInMiB)
		}
	}
	return 0, 0
}

func getInstanceTypeFamilyGeneration(instanceType string) (string, string) {
	typeSplit := strings.Split(instanceType, ".")
	if len(typeSplit) < 2 {
		return "", ""
	}
	instanceClass := typeSplit[0]
	re := regexp.MustCompile("[0-9]+")

	gen := re.FindAllString(instanceClass, -1)
	if len(gen) < 1 {
		return "", ""
	}
	generation := gen[0]

	genSplit := strings.Split(instanceClass, generation)
	if len(genSplit) < 1 {
		return "", ""
	}
	family := genSplit[0]

	return family, generation
}
