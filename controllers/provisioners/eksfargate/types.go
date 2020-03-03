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

package eksfargate

import (
	"github.com/keikoproj/instance-manager/api/v1alpha1"
)

// EksCfInstanceGroupContext defines the main type of an EKS Cloudformation provisioner
type EksFargateInstanceGroupContext struct {
	InstanceGroup *v1alpha1.InstanceGroup
}

type EksFargateCreateContext struct {
	ClusterName  *string
	ProfileName  *string
	ExecutionArn *string
}
type EksFargateDeleteContext struct {
	ClusterName *string
	ProfileName *string
}

func (ctx *EksFargateInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}

func (ctx *EksFargateCreateContext) SetClusterName(name *string) {
	ctx.ClusterName = name
}
func (ctx *EksFargateCreateContext) SetProfileName(name *string) {
	ctx.ProfileName = name
}
func (ctx *EksFargateCreateContext) SetExecutionArn(arn *string) {
	ctx.ExecutionArn = arn
}
func (ctx *EksFargateCreateContext) GetClusterName() *string {
	return ctx.ClusterName
}
func (ctx *EksFargateCreateContext) GetProfileName() *string {
	return ctx.ProfileName
}
func (ctx *EksFargateCreateContext) GetExecutionArn() *string {
	return ctx.ExecutionArn
}
func (ctx *EksFargateDeleteContext) SetClusterName(name *string) {
	ctx.ClusterName = name
}
func (ctx *EksFargateDeleteContext) SetProfileName(name *string) {
	ctx.ProfileName = name
}
func (ctx *EksFargateDeleteContext) GetClusterName() *string {
	return ctx.ClusterName
}
func (ctx *EksFargateDeleteContext) GetProfileName() *string {
	return ctx.ProfileName
}
