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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
)

type DiscoveredState struct {
	ProfileStatus string
}

func (ds *DiscoveredState) GetProfileStatus() string {
	return ds.ProfileStatus
}
func (ds *DiscoveredState) IsProvisioned() bool {
	return ds.GetProfileStatus() != aws.StringValue(nil)
}

type FargateInstanceGroupContext struct {
	InstanceGroup   *v1alpha1.InstanceGroup
	AwsWorker       awsprovider.AwsWorker
	DiscoveredState DiscoveredState
	Log             logr.Logger
}

func (ctx *FargateInstanceGroupContext) GetDiscoveredState() *DiscoveredState {
	return &ctx.DiscoveredState
}
func (ctx *FargateInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}
