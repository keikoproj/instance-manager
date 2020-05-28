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
	"sync"

	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
)

const (
	ProvisionerName                     = "eks"
	defaultLaunchConfigurationRetention = 2
)

var (

	// Edited. Temporarily marked for traceability purposes: labelbug fix @agaro
	//######################################################################################
	//RoleLabelsFmt          = []string{"node.kubernetes.io/role=%s", "node-role.kubernetes.io/%s=\"\""}
	RoleNewLabelFmt = "node.kubernetes.io/role=%s"
	//RoleNewLabelFmt = "kubernetes.io/role=%s"
	RoleOldLabelFmt = "node-role.kubernetes.io/%s=\"\""
	//######################################################################################

	DefaultManagedPolicies = []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
)

// New constructs a new instance group provisioner of EKS type
func New(p provisioners.ProvisionerInput) *EksInstanceGroupContext {

	ctx := &EksInstanceGroupContext{
		InstanceGroup:    p.InstanceGroup,
		KubernetesClient: p.Kubernetes,
		AwsWorker:        p.AwsWorker,
		Log:              p.Log.WithName("eks"),
	}
	instanceGroup := ctx.GetInstanceGroup()
	configuration := instanceGroup.GetEKSConfiguration()
	ctx.ResourcePrefix = fmt.Sprintf("%v-%v-%v", configuration.GetClusterName(), instanceGroup.GetNamespace(), instanceGroup.GetName())

	instanceGroup.SetState(v1alpha1.ReconcileInit)

	if len(p.Configuration.DefaultSubnets) != 0 {
		configuration.SetSubnets(p.Configuration.DefaultSubnets)
	}

	if p.Configuration.DefaultClusterName != "" {
		configuration.SetClusterName(p.Configuration.DefaultClusterName)
	}

	return ctx
}

func IsRetryable(instanceGroup *v1alpha1.InstanceGroup) bool {
	for _, state := range NonRetryableStates {
		if state == instanceGroup.GetState() {
			return false
		}
	}
	return true
}

type EksDefaultConfiguration struct {
	DefaultSubnets []string `yaml:"defaultSubnets,omitempty"`
	EksClusterName string   `yaml:"defaultClusterName,omitempty"`
}

type EksInstanceGroupContext struct {
	sync.Mutex
	InstanceGroup    *v1alpha1.InstanceGroup
	KubernetesClient kubeprovider.KubernetesClientSet
	AwsWorker        awsprovider.AwsWorker
	DiscoveredState  *DiscoveredState
	Log              logr.Logger
	ResourcePrefix   string
}

func (ctx *EksInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}
func (ctx *EksInstanceGroupContext) GetUpgradeStrategy() *v1alpha1.AwsUpgradeStrategy {
	if &ctx.InstanceGroup.Spec.AwsUpgradeStrategy != nil {
		return &ctx.InstanceGroup.Spec.AwsUpgradeStrategy
	}
	return &v1alpha1.AwsUpgradeStrategy{}
}
func (ctx *EksInstanceGroupContext) GetState() v1alpha1.ReconcileState {
	return ctx.InstanceGroup.GetState()
}
func (ctx *EksInstanceGroupContext) SetState(state v1alpha1.ReconcileState) {
	ctx.InstanceGroup.SetState(state)
}
func (ctx *EksInstanceGroupContext) GetDiscoveredState() *DiscoveredState {
	if ctx.DiscoveredState == nil {
		ctx.DiscoveredState = &DiscoveredState{}
	}
	return ctx.DiscoveredState
}
func (ctx *EksInstanceGroupContext) SetDiscoveredState(state *DiscoveredState) {
	ctx.DiscoveredState = state
}
