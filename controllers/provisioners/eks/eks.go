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
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	ProvisionerName = "eks"
)

var (
	TagClusterName            = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName      = "instancegroups.keikoproj.io/InstanceGroup"
	TagInstanceGroupNamespace = "instancegroups.keikoproj.io/Namespace"
	TagClusterOwnershipFmt    = "kubernetes.io/cluster/%s"
	TagKubernetesCluster      = "KubernetesCluster"
	TagClusterOwned           = "owned"
	TagName                   = "Name"
	RoleLabelsFmt             = []string{"node.kubernetes.io/role=%s", "node-role.kubernetes.io/%s=\"\""}
	DefaultManagedPolicies    = []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
)

func init() {
	ctrl.SetLogger(zap.Logger(true))
}

// New constructs a new instance group provisioner of EKS type
func New(p provisioners.ProvisionerInput) *EksInstanceGroupContext {

	ctx := &EksInstanceGroupContext{
		InstanceGroup:    p.InstanceGroup,
		KubernetesClient: p.Kubernetes,
		AwsWorker:        p.AwsWorker,
		Log:              p.Log,
	}
	instanceGroup := ctx.GetInstanceGroup()
	configuration := instanceGroup.GetEKSConfiguration()
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
	for _, state := range RetryableStates {
		if instanceGroup.GetState() == state {
			return true
		}
	}
	return false
}

type EksDefaultConfiguration struct {
	DefaultSubnets []string `yaml:"defaultSubnets,omitempty"`
	EksClusterName string   `yaml:"defaultClusterName,omitempty"`
}

type EksInstanceGroupContext struct {
	InstanceGroup    *v1alpha1.InstanceGroup
	KubernetesClient common.KubernetesClientSet
	AwsWorker        awsprovider.AwsWorker
	DiscoveredState  *DiscoveredState
	Log              logr.Logger
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
