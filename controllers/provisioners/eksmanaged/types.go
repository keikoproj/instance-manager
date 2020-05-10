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

package eksmanaged

import (
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
)

type EksManagedDefaultConfiguration struct {
	DefaultSubnets []string `yaml:"defaultSubnets,omitempty"`
	EksClusterName string   `yaml:"defaultClusterName,omitempty"`
}

type EksManagedInstanceGroupContext struct {
	InstanceGroup    *v1alpha1.InstanceGroup
	KubernetesClient common.KubernetesClientSet
	AwsWorker        aws.AwsWorker
	DiscoveredState  *DiscoveredState
	Log              logr.Logger
}
type DiscoveredState struct {
	Provisioned   bool
	SelfNodeGroup *eks.Nodegroup
	CurrentState  string
}

func (d *DiscoveredState) SetSelfNodeGroup(ng *eks.Nodegroup) {
	d.SelfNodeGroup = ng
}

func (d *DiscoveredState) GetSelfNodeGroup() *eks.Nodegroup {
	return d.SelfNodeGroup
}

func (d *DiscoveredState) SetProvisioned(provisioned bool) {
	d.Provisioned = provisioned
}

func (d *DiscoveredState) SetCurrentState(state string) {
	d.CurrentState = state
}

func (d *DiscoveredState) GetCurrentState() string {
	return d.CurrentState
}

func (d *DiscoveredState) IsProvisioned() bool {
	return d.Provisioned
}

func (ctx *EksManagedInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}

func (ctx *EksManagedInstanceGroupContext) GetState() v1alpha1.ReconcileState {
	return ctx.InstanceGroup.GetState()
}

func (ctx *EksManagedInstanceGroupContext) SetState(state v1alpha1.ReconcileState) {
	ctx.InstanceGroup.SetState(state)
}

func (ctx *EksManagedInstanceGroupContext) GetDiscoveredState() *DiscoveredState {
	if ctx != nil {
		return ctx.DiscoveredState
	}
	return &DiscoveredState{}
}

func (ctx *EksManagedInstanceGroupContext) SetDiscoveredState(state *DiscoveredState) {
	ctx.DiscoveredState = state
}
