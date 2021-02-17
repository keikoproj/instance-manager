// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AwsUpgradeStrategy) DeepCopyInto(out *AwsUpgradeStrategy) {
	*out = *in
	if in.CRDType != nil {
		in, out := &in.CRDType, &out.CRDType
		*out = new(CRDUpdateStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.RollingUpdateType != nil {
		in, out := &in.RollingUpdateType, &out.RollingUpdateType
		*out = new(RollingUpdateStrategy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AwsUpgradeStrategy.
func (in *AwsUpgradeStrategy) DeepCopy() *AwsUpgradeStrategy {
	if in == nil {
		return nil
	}
	out := new(AwsUpgradeStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BootstrapOptions) DeepCopyInto(out *BootstrapOptions) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BootstrapOptions.
func (in *BootstrapOptions) DeepCopy() *BootstrapOptions {
	if in == nil {
		return nil
	}
	out := new(BootstrapOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CRDUpdateStrategy) DeepCopyInto(out *CRDUpdateStrategy) {
	*out = *in
	if in.MaxRetries != nil {
		in, out := &in.MaxRetries, &out.MaxRetries
		*out = new(int)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CRDUpdateStrategy.
func (in *CRDUpdateStrategy) DeepCopy() *CRDUpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(CRDUpdateStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSConfiguration) DeepCopyInto(out *EKSConfiguration) {
	*out = *in
	if in.NodeSecurityGroups != nil {
		in, out := &in.NodeSecurityGroups, &out.NodeSecurityGroups
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]NodeVolume, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Subnets != nil {
		in, out := &in.Subnets, &out.Subnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.SuspendedProcesses != nil {
		in, out := &in.SuspendedProcesses, &out.SuspendedProcesses
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.BootstrapOptions != nil {
		in, out := &in.BootstrapOptions, &out.BootstrapOptions
		*out = new(BootstrapOptions)
		**out = **in
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]map[string]string, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = make(map[string]string, len(*in))
				for key, val := range *in {
					(*out)[key] = val
				}
			}
		}
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Taints != nil {
		in, out := &in.Taints, &out.Taints
		*out = make([]v1.Taint, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.UserData != nil {
		in, out := &in.UserData, &out.UserData
		*out = make([]UserDataStage, len(*in))
		copy(*out, *in)
	}
	if in.ManagedPolicies != nil {
		in, out := &in.ManagedPolicies, &out.ManagedPolicies
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.MetricsCollection != nil {
		in, out := &in.MetricsCollection, &out.MetricsCollection
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.LifecycleHooks != nil {
		in, out := &in.LifecycleHooks, &out.LifecycleHooks
		*out = make([]LifecycleHookSpec, len(*in))
		copy(*out, *in)
	}
	if in.MixedInstancesPolicy != nil {
		in, out := &in.MixedInstancesPolicy, &out.MixedInstancesPolicy
		*out = new(MixedInstancesPolicySpec)
		(*in).DeepCopyInto(*out)
	}
	if in.LicenseSpecifications != nil {
		in, out := &in.LicenseSpecifications, &out.LicenseSpecifications
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Placement != nil {
		in, out := &in.Placement, &out.Placement
		*out = new(PlacementSpec)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSConfiguration.
func (in *EKSConfiguration) DeepCopy() *EKSConfiguration {
	if in == nil {
		return nil
	}
	out := new(EKSConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSFargateSelectors) DeepCopyInto(out *EKSFargateSelectors) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSFargateSelectors.
func (in *EKSFargateSelectors) DeepCopy() *EKSFargateSelectors {
	if in == nil {
		return nil
	}
	out := new(EKSFargateSelectors)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSFargateSpec) DeepCopyInto(out *EKSFargateSpec) {
	*out = *in
	if in.Subnets != nil {
		in, out := &in.Subnets, &out.Subnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Selectors != nil {
		in, out := &in.Selectors, &out.Selectors
		*out = make([]EKSFargateSelectors, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]map[string]string, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = make(map[string]string, len(*in))
				for key, val := range *in {
					(*out)[key] = val
				}
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSFargateSpec.
func (in *EKSFargateSpec) DeepCopy() *EKSFargateSpec {
	if in == nil {
		return nil
	}
	out := new(EKSFargateSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSManagedConfiguration) DeepCopyInto(out *EKSManagedConfiguration) {
	*out = *in
	if in.NodeLabels != nil {
		in, out := &in.NodeLabels, &out.NodeLabels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.NodeSecurityGroups != nil {
		in, out := &in.NodeSecurityGroups, &out.NodeSecurityGroups
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]map[string]string, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = make(map[string]string, len(*in))
				for key, val := range *in {
					(*out)[key] = val
				}
			}
		}
	}
	if in.Subnets != nil {
		in, out := &in.Subnets, &out.Subnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSManagedConfiguration.
func (in *EKSManagedConfiguration) DeepCopy() *EKSManagedConfiguration {
	if in == nil {
		return nil
	}
	out := new(EKSManagedConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSManagedSpec) DeepCopyInto(out *EKSManagedSpec) {
	*out = *in
	if in.EKSManagedConfiguration != nil {
		in, out := &in.EKSManagedConfiguration, &out.EKSManagedConfiguration
		*out = new(EKSManagedConfiguration)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSManagedSpec.
func (in *EKSManagedSpec) DeepCopy() *EKSManagedSpec {
	if in == nil {
		return nil
	}
	out := new(EKSManagedSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EKSSpec) DeepCopyInto(out *EKSSpec) {
	*out = *in
	if in.EKSConfiguration != nil {
		in, out := &in.EKSConfiguration, &out.EKSConfiguration
		*out = new(EKSConfiguration)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EKSSpec.
func (in *EKSSpec) DeepCopy() *EKSSpec {
	if in == nil {
		return nil
	}
	out := new(EKSSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceGroup) DeepCopyInto(out *InstanceGroup) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceGroup.
func (in *InstanceGroup) DeepCopy() *InstanceGroup {
	if in == nil {
		return nil
	}
	out := new(InstanceGroup)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *InstanceGroup) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceGroupCondition) DeepCopyInto(out *InstanceGroupCondition) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceGroupCondition.
func (in *InstanceGroupCondition) DeepCopy() *InstanceGroupCondition {
	if in == nil {
		return nil
	}
	out := new(InstanceGroupCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceGroupList) DeepCopyInto(out *InstanceGroupList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]InstanceGroup, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceGroupList.
func (in *InstanceGroupList) DeepCopy() *InstanceGroupList {
	if in == nil {
		return nil
	}
	out := new(InstanceGroupList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *InstanceGroupList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceGroupSpec) DeepCopyInto(out *InstanceGroupSpec) {
	*out = *in
	if in.EKSManagedSpec != nil {
		in, out := &in.EKSManagedSpec, &out.EKSManagedSpec
		*out = new(EKSManagedSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.EKSFargateSpec != nil {
		in, out := &in.EKSFargateSpec, &out.EKSFargateSpec
		*out = new(EKSFargateSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.EKSSpec != nil {
		in, out := &in.EKSSpec, &out.EKSSpec
		*out = new(EKSSpec)
		(*in).DeepCopyInto(*out)
	}
	in.AwsUpgradeStrategy.DeepCopyInto(&out.AwsUpgradeStrategy)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceGroupSpec.
func (in *InstanceGroupSpec) DeepCopy() *InstanceGroupSpec {
	if in == nil {
		return nil
	}
	out := new(InstanceGroupSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceGroupStatus) DeepCopyInto(out *InstanceGroupStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]InstanceGroupCondition, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceGroupStatus.
func (in *InstanceGroupStatus) DeepCopy() *InstanceGroupStatus {
	if in == nil {
		return nil
	}
	out := new(InstanceGroupStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceTypeSpec) DeepCopyInto(out *InstanceTypeSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceTypeSpec.
func (in *InstanceTypeSpec) DeepCopy() *InstanceTypeSpec {
	if in == nil {
		return nil
	}
	out := new(InstanceTypeSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LifecycleHookSpec) DeepCopyInto(out *LifecycleHookSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LifecycleHookSpec.
func (in *LifecycleHookSpec) DeepCopy() *LifecycleHookSpec {
	if in == nil {
		return nil
	}
	out := new(LifecycleHookSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MixedInstancesPolicySpec) DeepCopyInto(out *MixedInstancesPolicySpec) {
	*out = *in
	if in.Strategy != nil {
		in, out := &in.Strategy, &out.Strategy
		*out = new(string)
		**out = **in
	}
	if in.SpotPools != nil {
		in, out := &in.SpotPools, &out.SpotPools
		*out = new(int64)
		**out = **in
	}
	if in.BaseCapacity != nil {
		in, out := &in.BaseCapacity, &out.BaseCapacity
		*out = new(int64)
		**out = **in
	}
	if in.SpotRatio != nil {
		in, out := &in.SpotRatio, &out.SpotRatio
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.InstancePool != nil {
		in, out := &in.InstancePool, &out.InstancePool
		*out = new(string)
		**out = **in
	}
	if in.InstanceTypes != nil {
		in, out := &in.InstanceTypes, &out.InstanceTypes
		*out = make([]*InstanceTypeSpec, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(InstanceTypeSpec)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MixedInstancesPolicySpec.
func (in *MixedInstancesPolicySpec) DeepCopy() *MixedInstancesPolicySpec {
	if in == nil {
		return nil
	}
	out := new(MixedInstancesPolicySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeVolume) DeepCopyInto(out *NodeVolume) {
	*out = *in
	if in.DeleteOnTermination != nil {
		in, out := &in.DeleteOnTermination, &out.DeleteOnTermination
		*out = new(bool)
		**out = **in
	}
	if in.Encrypted != nil {
		in, out := &in.Encrypted, &out.Encrypted
		*out = new(bool)
		**out = **in
	}
	if in.MountOptions != nil {
		in, out := &in.MountOptions, &out.MountOptions
		*out = new(NodeVolumeMountOptions)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeVolume.
func (in *NodeVolume) DeepCopy() *NodeVolume {
	if in == nil {
		return nil
	}
	out := new(NodeVolume)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeVolumeMountOptions) DeepCopyInto(out *NodeVolumeMountOptions) {
	*out = *in
	if in.Persistance != nil {
		in, out := &in.Persistance, &out.Persistance
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeVolumeMountOptions.
func (in *NodeVolumeMountOptions) DeepCopy() *NodeVolumeMountOptions {
	if in == nil {
		return nil
	}
	out := new(NodeVolumeMountOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlacementSpec) DeepCopyInto(out *PlacementSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlacementSpec.
func (in *PlacementSpec) DeepCopy() *PlacementSpec {
	if in == nil {
		return nil
	}
	out := new(PlacementSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RollingUpdateStrategy) DeepCopyInto(out *RollingUpdateStrategy) {
	*out = *in
	if in.MaxUnavailable != nil {
		in, out := &in.MaxUnavailable, &out.MaxUnavailable
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.MaxRetries != nil {
		in, out := &in.MaxRetries, &out.MaxRetries
		*out = new(int)
		**out = **in
	}
	in.DrainOptions.DeepCopyInto(&out.DrainOptions)
	if in.ReadinessGates != nil {
		in, out := &in.ReadinessGates, &out.ReadinessGates
		*out = make([]RollingUpgradeReadinessGate, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RollingUpdateStrategy.
func (in *RollingUpdateStrategy) DeepCopy() *RollingUpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(RollingUpdateStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RollingUpgradeDrainOptions) DeepCopyInto(out *RollingUpgradeDrainOptions) {
	*out = *in
	if in.TimeoutSeconds != nil {
		in, out := &in.TimeoutSeconds, &out.TimeoutSeconds
		*out = new(int64)
		**out = **in
	}
	if in.Force != nil {
		in, out := &in.Force, &out.Force
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RollingUpgradeDrainOptions.
func (in *RollingUpgradeDrainOptions) DeepCopy() *RollingUpgradeDrainOptions {
	if in == nil {
		return nil
	}
	out := new(RollingUpgradeDrainOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RollingUpgradeReadinessGate) DeepCopyInto(out *RollingUpgradeReadinessGate) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RollingUpgradeReadinessGate.
func (in *RollingUpgradeReadinessGate) DeepCopy() *RollingUpgradeReadinessGate {
	if in == nil {
		return nil
	}
	out := new(RollingUpgradeReadinessGate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UserDataStage) DeepCopyInto(out *UserDataStage) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UserDataStage.
func (in *UserDataStage) DeepCopy() *UserDataStage {
	if in == nil {
		return nil
	}
	out := new(UserDataStage)
	in.DeepCopyInto(out)
	return out
}
