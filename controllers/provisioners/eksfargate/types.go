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
	aws "github.com/keikoproj/instance-manager/controllers/providers/aws"
)

// EksCfInstanceGroupContext defines the main type of an EKS Cloudformation provisioner
type InstanceGroupContext struct {
	InstanceGroup             *v1alpha1.InstanceGroup
	AwsFargateWorker          *aws.AwsFargateWorker
	ExecutionPodRoleStackName string
}

type CreateContext struct {
	ClusterName  *string
	ProfileName  *string
	ExecutionArn *string
}
type DeleteContext struct {
	ClusterName *string
	ProfileName *string
}

func (ctx *InstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}

func (ctx *CreateContext) SetClusterName(name *string) {
	ctx.ClusterName = name
}
func (ctx *CreateContext) SetProfileName(name *string) {
	ctx.ProfileName = name
}
func (ctx *CreateContext) SetExecutionArn(arn *string) {
	ctx.ExecutionArn = arn
}
func (ctx *CreateContext) GetClusterName() *string {
	return ctx.ClusterName
}
func (ctx *CreateContext) GetProfileName() *string {
	return ctx.ProfileName
}
func (ctx *CreateContext) GetExecutionArn() *string {
	return ctx.ExecutionArn
}
func (ctx *DeleteContext) SetClusterName(name *string) {
	ctx.ClusterName = name
}
func (ctx *DeleteContext) SetProfileName(name *string) {
	ctx.ProfileName = name
}
func (ctx *DeleteContext) GetClusterName() *string {
	return ctx.ClusterName
}
func (ctx *DeleteContext) GetProfileName() *string {
	return ctx.ProfileName
}
