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

package ekscloudformation

import (
	"reflect"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type AwsAuthConfig struct {
	RoleARN  string   `yaml:"rolearn"`
	Username string   `yaml:"username"`
	Groups   []string `yaml:"groups"`
}

type AwsAuthConfigMapRolesData struct {
	MapRoles []AwsAuthConfig `yaml:"mapRoles"`
}

func (m *AwsAuthConfigMapRolesData) AddUnique(config AwsAuthConfig) {
	for _, existingConf := range m.MapRoles {
		if reflect.DeepEqual(existingConf, config) {
			return
		}
	}
	if config.RoleARN == "" || config.Username == "" || len(config.Groups) == 0 {
		return
	}
	m.MapRoles = append(m.MapRoles, config)
}

// EksCfInstanceGroupContext defines the main type of an EKS Cloudformation provisioner
type EksCfInstanceGroupContext struct {
	InstanceGroup    *v1alpha1.InstanceGroup
	KubernetesClient common.KubernetesClientSet
	AwsWorker        aws.AwsWorker
	DiscoveredState  *DiscoveredState
	StackExists      bool
	InstanceArn      string
	ControllerRegion string
	VpcID            string
	DefaultARNList   []string
}

type EksCfDefaultConfiguration struct {
	DefaultSubnets []string `yaml:"defaultSubnets,omitempty"`
	EksClusterName string   `yaml:"defaultClusterName,omitempty"`
	DefaultARNs    []string `yaml:"defaultArns,omitempty"`
}

func (ctx *EksCfInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}

func (ctx *EksCfInstanceGroupContext) GetState() v1alpha1.ReconcileState {
	return ctx.InstanceGroup.GetState()
}

func (ctx *EksCfInstanceGroupContext) SetState(state v1alpha1.ReconcileState) {
	ctx.InstanceGroup.SetState(state)
}

func (ctx *EksCfInstanceGroupContext) GetDiscoveredState() *DiscoveredState {
	if ctx != nil {
		return ctx.DiscoveredState
	}
	return &DiscoveredState{}
}

func (ctx *EksCfInstanceGroupContext) SetDiscoveredState(state *DiscoveredState) {
	ctx.DiscoveredState = state
}

// DiscoveredState is the output type of DiscoverState method
type DiscoveredState struct {
	SelfStack            *cloudformation.Stack
	SelfGroup            *DiscoveredInstanceGroup
	CloudformationStacks []*cloudformation.Stack
	ScalingGroups        []*autoscaling.Group
	LaunchConfigurations []*autoscaling.LaunchConfiguration
	OwnedResources       []*unstructured.Unstructured
	ActiveOwnedResources []*unstructured.Unstructured
	InstanceGroups       DiscoveredInstanceGroups
}

func (s *DiscoveredState) GetOwnedResources() []*unstructured.Unstructured {
	return s.OwnedResources
}

func (s *DiscoveredState) AddOwnedResources(resource unstructured.Unstructured) {
	s.OwnedResources = append(s.OwnedResources, &resource)
}

func (s *DiscoveredState) GetActiveOwnedResources() []*unstructured.Unstructured {
	return s.ActiveOwnedResources
}

func (s *DiscoveredState) AddActiveOwnedResources(resource *unstructured.Unstructured) {
	s.ActiveOwnedResources = append(s.ActiveOwnedResources, resource)
}

func (s *DiscoveredState) GetInstanceGroups() DiscoveredInstanceGroups {
	if s != nil {
		return s.InstanceGroups
	}
	return DiscoveredInstanceGroups{}
}

func (s *DiscoveredState) SetInstanceGroups(instanceGroups DiscoveredInstanceGroups) {
	s.InstanceGroups = instanceGroups
}

func (s *DiscoveredState) GetSelfStack() *cloudformation.Stack {
	if s != nil {
		return s.SelfStack
	}
	return &cloudformation.Stack{}
}

func (s *DiscoveredState) SetSelfStack(stack *cloudformation.Stack) {
	s.SelfStack = stack
}

func (s *DiscoveredState) GetSelfGroup() *DiscoveredInstanceGroup {
	if s != nil {
		return s.SelfGroup
	}
	return &DiscoveredInstanceGroup{}
}

func (s *DiscoveredState) SetSelfGroup(group *DiscoveredInstanceGroup) {
	s.SelfGroup = group
}

func (s *DiscoveredState) GetCloudformationStacks() []*cloudformation.Stack {
	if s != nil {
		return s.CloudformationStacks
	}
	return []*cloudformation.Stack{}
}

func (s *DiscoveredState) SetCloudformationStacks(stacks []*cloudformation.Stack) {
	s.CloudformationStacks = stacks
}

func (s *DiscoveredState) GetScalingGroups() []*autoscaling.Group {
	if s != nil {
		return s.ScalingGroups
	}
	return []*autoscaling.Group{}
}

func (s *DiscoveredState) SetScalingGroups(groups []*autoscaling.Group) {
	s.ScalingGroups = groups
}

func (s *DiscoveredState) GetLaunchConfigurations() []*autoscaling.LaunchConfiguration {
	if s != nil {
		return s.LaunchConfigurations
	}
	return []*autoscaling.LaunchConfiguration{}
}

func (s *DiscoveredState) SetLaunchConfigurations(launchConfigs []*autoscaling.LaunchConfiguration) {
	s.LaunchConfigurations = launchConfigs
}

type DiscoveredInstanceGroups struct {
	Items []DiscoveredInstanceGroup
}

func (groups *DiscoveredInstanceGroups) AddGroup(group DiscoveredInstanceGroup) []DiscoveredInstanceGroup {
	if group.Name == "" || group.Namespace == "" || group.ClusterName == "" || group.StackName == "" ||
		group.ARN == "" || group.ScalingGroupName == "" || group.LaunchConfigName == "" {
		return groups.Items
	}
	groups.Items = append(groups.Items, group)
	return groups.Items
}

type DiscoveredInstanceGroup struct {
	Name             string
	Namespace        string
	ClusterName      string
	StackName        string
	ARN              string
	ScalingGroupName string
	LaunchConfigName string
	IsClusterMember  bool
}

func (d *DiscoveredInstanceGroup) GetLaunchConfigName() string {
	if d != nil {
		return d.LaunchConfigName
	}
	return ""
}

func (d *DiscoveredInstanceGroup) GetScalingGroupName() string {
	if d != nil {
		return d.ScalingGroupName
	}
	return ""
}

func (d *DiscoveredInstanceGroup) GetARN() string {
	if d != nil {
		return d.ARN
	}
	return ""
}
