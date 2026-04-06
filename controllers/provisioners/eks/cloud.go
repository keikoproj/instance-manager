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
	"context"
	"strings"

	"github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
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

type InstancePoolSpec struct {
	SubFamilyFlexiblePool InstancePool
}

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
	InstancePool         InstancePoolSpec
	InstanceTypeInfo     []*ec2.InstanceTypeInfo
}

func (ctx *EksInstanceGroupContext) CloudDiscovery() error {
	var (
		state                = ctx.GetDiscoveredState()
		instanceGroup        = ctx.GetInstanceGroup()
		spec                 = instanceGroup.GetEKSSpec()
		configuration        = instanceGroup.GetEKSConfiguration()
		mixedInstancesPolicy = configuration.GetMixedInstancesPolicy()
		status               = instanceGroup.GetStatus()
		clusterName          = configuration.GetClusterName()
	)

	state.Publisher = kubeprovider.EventPublisher{
		Client:          ctx.KubernetesClient.Kubernetes,
		Namespace:       instanceGroup.GetNamespace(),
		Name:            instanceGroup.GetName(),
		UID:             instanceGroup.GetUID(),
		ResourceVersion: instanceGroup.GetResourceVersion(),
	}

	status.SetLifecycle(v1alpha1.LifecycleStateNormal)

	if spec.IsLaunchConfiguration() {
		input := &scaling.DiscoverConfigurationInput{
			TargetConfigName: status.GetActiveLaunchConfigurationName(),
		}

		var (
			config *scaling.LaunchConfiguration
			err    error
		)

		if config, err = scaling.NewLaunchConfiguration(instanceGroup.NamespacedName(), ctx.AwsWorker, input); err != nil {
			return errors.Wrap(err, "failed to discover launch configuration")
		}
		state.ScalingConfiguration = config
		status.SetActiveLaunchConfigurationName(config.Name())
	}

	if spec.IsLaunchTemplate() {
		input := &scaling.DiscoverConfigurationInput{
			TargetConfigName: status.GetActiveLaunchTemplateName(),
		}

		var (
			config *scaling.LaunchTemplate
			err    error
		)

		if config, err = scaling.NewLaunchTemplate(instanceGroup.NamespacedName(), ctx.AwsWorker, input); err != nil {
			return errors.Wrap(err, "failed to discover launch template")
		}
		state.ScalingConfiguration = config
		status.SetActiveLaunchTemplateName(config.Name())

		if mixedInstancesPolicy != nil {
			if ratio := common.IntOrStrValue(mixedInstancesPolicy.SpotRatio); ratio > 0 {
				status.SetLifecycle(v1alpha1.LifecycleStateMixed)
			}
		}
	}

	nodes, err := ctx.KubernetesClient.Kubernetes.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to list cluster nodes")
	}
	state.SetClusterNodes(nodes)

	var roleName, instanceProfileName string
	if configuration.HasExistingRole() {
		roleName = configuration.GetRoleName()
		instanceProfileName = configuration.GetInstanceProfileName()
	} else {
		roleName = ctx.ResourcePrefix
		instanceProfileName = ctx.ResourcePrefix
		if len(roleName) > 63 {
			// use a hash of the actual name in case we exceed the max length
			roleName = common.StringMD5(roleName)
			instanceProfileName = common.StringMD5(instanceProfileName)
		}

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

	vpcID := aws.StringValue(cluster.ResourcesVpcConfig.VpcId)
	state.SetVPCId(vpcID)

	instanceTypes, err := ctx.AwsWorker.DescribeInstanceTypes()
	if err != nil {
		return errors.Wrap(err, "failed to discover instance types")
	}
	state.SetInstanceTypeInfo(instanceTypes)

	if strings.EqualFold(configuration.Image, v1alpha1.ImageLatestValue) {
		latestAmiId, err := ctx.GetEksLatestAmi()
		if err != nil {
			return errors.Wrap(err, "failed to discover latest AMI ID")
		}
		configuration.Image = latestAmiId
		ctx.Log.V(4).Info("Updating Image ID with latest", "ami_id", latestAmiId)
	}

	if strings.HasPrefix(configuration.Image, v1alpha1.ImageSSMPrefix) {
		ssmKey := strings.TrimPrefix(configuration.Image, v1alpha1.ImageSSMPrefix)
		amiId, err := ctx.GetEksSsmAmi(ssmKey)
		if err != nil {
			return errors.Wrap(err, "failed to discover ami")
		}
		configuration.Image = amiId
		ctx.Log.V(4).Info("Updating Image ID with ami", "ami_id", amiId)
	}

	// All information needed to creating the scaling group must happen before this line.
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

	err = ctx.discoverSpotPrice()
	if err != nil {
		ctx.Log.Error(err, "failed to discover spot price")
	}

	spotPrice := configuration.GetSpotPrice()
	if !common.StringEmpty(spotPrice) {
		status.SetLifecycle(v1alpha1.LifecycleStateSpot)
	}

	state.LifecycleHooks, err = ctx.AwsWorker.DescribeLifecycleHooks(asgName)
	if err != nil {
		return errors.Wrap(err, "failed to describe lifecycle hooks")
	}

	// update status with scaling group info
	status.SetActiveScalingGroupName(asgName)
	status.SetCurrentMin(int(aws.Int64Value(targetScalingGroup.MinSize)))
	status.SetCurrentMax(int(aws.Int64Value(targetScalingGroup.MaxSize)))

	if spec.IsLaunchConfiguration() {

		state.ScalingConfiguration, err = scaling.NewLaunchConfiguration(instanceGroup.NamespacedName(), ctx.AwsWorker, &scaling.DiscoverConfigurationInput{
			ScalingGroup:     targetScalingGroup,
			TargetConfigName: state.ScalingConfiguration.Name(),
		})
		if err != nil {
			return errors.Wrap(err, "failed to discover launch configurations")
		}

		var resourceName = state.ScalingConfiguration.Name()
		status.SetActiveLaunchConfigurationName(resourceName)
	}

	if spec.IsLaunchTemplate() {
		state.ScalingConfiguration, err = scaling.NewLaunchTemplate(instanceGroup.NamespacedName(), ctx.AwsWorker, &scaling.DiscoverConfigurationInput{
			ScalingGroup:     targetScalingGroup,
			TargetConfigName: state.ScalingConfiguration.Name(),
		})
		if err != nil {
			return errors.Wrap(err, "failed to discover launch templates")
		}

		offerings, err := ctx.AwsWorker.DescribeInstanceOfferings()
		if err != nil {
			return errors.Wrap(err, "failed to discover launch templates")
		}
		var (
			pool             = subFamilyFlexiblePool(offerings, instanceTypes)
			resource         = state.ScalingConfiguration.Resource()
			resourceName     = state.ScalingConfiguration.Name()
			template         = scaling.ConvertToLaunchTemplate(resource)
			latestVersion    = aws.Int64Value(template.LatestVersionNumber)
			latestVersionStr = common.Int64ToStr(latestVersion)
		)

		state.SetSubFamilyFlexiblePool(pool)
		status.SetActiveLaunchTemplateName(resourceName)
		status.SetLatestTemplateVersion(latestVersionStr)
	}

	// delete old launch configurations
	if err := state.ScalingConfiguration.Delete(&scaling.DeleteConfigurationInput{
		Name:           state.ScalingConfiguration.Name(),
		Prefix:         ctx.ResourcePrefix,
		DeleteAll:      false,
		RetainVersions: ctx.ConfigRetention,
	}); err != nil {
		ctx.Log.Error(err, "failed to delete old scaling configurations")
	}

	switch status.GetNodesReadyCondition() {
	case corev1.ConditionTrue:
		state.SetNodesReady(true)
	default:
		state.SetNodesReady(false)
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

func (d *DiscoveredState) GetCluster() *eks.Cluster {
	return d.Cluster
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

func (d *DiscoveredState) GetClusterCA() string {
	if d.Cluster == nil {
		return ""
	}
	return aws.StringValue(d.Cluster.CertificateAuthority.Data)
}

func (d *DiscoveredState) GetClusterEndpoint() string {
	if d.Cluster == nil {
		return ""
	}
	return aws.StringValue(d.Cluster.Endpoint)
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

func (d *DiscoveredState) SetInstanceTypeInfo(instanceTypeInfo []*ec2.InstanceTypeInfo) {
	if instanceTypeInfo != nil {
		d.InstanceTypeInfo = instanceTypeInfo
	}
}
func (d *DiscoveredState) GetInstanceTypeInfo() []*ec2.InstanceTypeInfo {
	if d.InstanceTypeInfo != nil {
		return d.InstanceTypeInfo
	}
	return []*ec2.InstanceTypeInfo{}
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
func (d *DiscoveredState) GetRunningInstanceTypes() []string {
	types := make([]string, 0)
	if d.ScalingGroup == nil {
		return types
	}
	for _, t := range d.ScalingGroup.Instances {
		instanceType := aws.StringValue(t.InstanceType)
		if !common.ContainsEqualFold(types, instanceType) {
			types = append(types, instanceType)
		}
	}
	return types
}

func (d *DiscoveredState) SetSubFamilyFlexiblePool(pool map[string][]InstanceSpec) {
	d.InstancePool.SubFamilyFlexiblePool = InstancePool{
		Type: SubFamilyFlexible,
		Pool: pool,
	}
}

func subFamilyFlexiblePool(offerings []*ec2.InstanceTypeOffering, typeInfo []*ec2.InstanceTypeInfo) map[string][]InstanceSpec {
	var (
		DefaultOfferingWeight = "1"
		pool                  = make(map[string][]InstanceSpec, 0)
	)

	for _, t := range offerings {
		var (
			offeringType      = aws.StringValue(t.InstanceType)
			desiredArchs      = awsprovider.GetInstanceArchitectures(typeInfo, offeringType)
			desiredFamily     = awsprovider.GetInstanceFamily(offeringType)
			desiredGeneration = awsprovider.GetInstanceGeneration(offeringType)
			cpu               = awsprovider.GetOfferingVCPU(typeInfo, offeringType)
			mem               = awsprovider.GetOfferingMemory(typeInfo, offeringType)
			spec              = InstanceSpec{
				Type:   offeringType,
				Weight: DefaultOfferingWeight,
			}
		)

		pool[offeringType] = make([]InstanceSpec, 0)
		pool[offeringType] = append(pool[offeringType], spec)

		for _, i := range typeInfo {
			var (
				instanceType = aws.StringValue(i.InstanceType)
				instanceVCPU = aws.Int64Value(i.VCpuInfo.DefaultVCpus)
				instanceMem  = aws.Int64Value(i.MemoryInfo.SizeInMiB)
				family       = awsprovider.GetInstanceFamily(instanceType)
				generation   = awsprovider.GetInstanceGeneration(instanceType)
				spec         = InstanceSpec{
					Type:   instanceType,
					Weight: DefaultOfferingWeight,
				}
				supportedArchs = awsprovider.GetInstanceArchitectures(typeInfo, instanceType)
			)

			if len(desiredArchs) != len(supportedArchs) {
				continue
			}

			if !common.StringSliceContains(desiredArchs, supportedArchs) {
				continue
			}

			if !strings.EqualFold(family, desiredFamily) {
				continue
			}
			if !strings.EqualFold(generation, desiredGeneration) {
				continue
			}
			if strings.EqualFold(offeringType, instanceType) {
				continue
			}
			if cpu == instanceVCPU && mem == instanceMem {
				pool[offeringType] = append(pool[offeringType], spec)
			}
		}
	}
	return pool
}
