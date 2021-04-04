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

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
)

const (
	ProvisionerName                     = "eks"
	defaultLaunchConfigurationRetention = 2
	OverrideDefaultLabelsAnnotationKey  = "instancemgr.keikoproj.io/default-labels"
	OsFamilyAnnotation                  = "instancemgr.keikoproj.io/os-family"
	ClusterAutoscalerEnabledAnnotation  = "instancemgr.keikoproj.io/cluster-autoscaler-enabled"
	CustomNetworkingEnabledAnnotation   = "instancemgr.keikoproj.io/custom-networking-enabled"
	CustomNetworkingHostPodsAnnotation  = "instancemgr.keikoproj.io/custom-networking-host-pods"

	OsFamilyWindows      = "windows"
	OsFamilyBottleRocket = "bottlerocket"
	OsFamilyAmazonLinux2 = "amazonlinux2"
)

var (
	RoleNewLabel              = "node.kubernetes.io/role"
	RoleNewLabelFmt           = "node.kubernetes.io/role=%s"
	RoleOldLabel              = "node-role.kubernetes.io/%s"
	RoleOldLabelFmt           = "node-role.kubernetes.io/%s=\"\""
	InstanceMgrLifecycleLabel = "instancemgr.keikoproj.io/lifecycle"
	InstanceMgrImageLabel     = "instancemgr.keikoproj.io/image"

	DefaultManagedPolicies = []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
)

// New constructs a new instance group provisioner of EKS type
func New(p provisioners.ProvisionerInput) *EksInstanceGroupContext {
	var (
		instanceGroup = p.InstanceGroup
		configuration = instanceGroup.GetEKSConfiguration()
		status        = instanceGroup.GetStatus()
		strategy      = instanceGroup.GetUpgradeStrategy()
	)

	ctx := &EksInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: p.Kubernetes,
		AwsWorker:        p.AwsWorker,
		Log:              p.Log.WithName("eks"),
		ResourcePrefix:   fmt.Sprintf("%v-%v-%v", configuration.GetClusterName(), instanceGroup.GetNamespace(), instanceGroup.GetName()),
		ConfigRetention:  p.ConfigRetention,
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)
	status.SetProvisioner(ProvisionerName)
	status.SetStrategy(strategy.Type)

	return ctx
}

type EksInstanceGroupContext struct {
	sync.Mutex
	InstanceGroup    *v1alpha1.InstanceGroup
	KubernetesClient kubeprovider.KubernetesClientSet
	AwsWorker        awsprovider.AwsWorker
	DiscoveredState  *DiscoveredState
	Log              logr.Logger
	Configuration    *provisioners.ProvisionerConfiguration
	ConfigRetention  int
	ResourcePrefix   string
}

type UserDataPayload struct {
	PreBootstrap  []string
	PostBootstrap []string
}

type MountOpts struct {
	FileSystem  string
	Device      string
	Mount       string
	Persistance bool
}

type EKSUserData struct {
	ApiEndpoint      string
	ClusterCA        string
	ClusterName      string
	NodeLabels       map[string]string
	NodeTaints       []corev1.Taint
	KubeletExtraArgs string
	Arguments        string
	PreBootstrap     []string
	PostBootstrap    []string
	MountOptions     []MountOpts
	MaxPods          int64
}

func (ctx *EksInstanceGroupContext) GetInstanceGroup() *v1alpha1.InstanceGroup {
	if ctx != nil {
		return ctx.InstanceGroup
	}
	return &v1alpha1.InstanceGroup{}
}

func (ctx *EksInstanceGroupContext) GetOsFamily() string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		annotations   = instanceGroup.GetAnnotations()
	)
	if _, exists := annotations[OsFamilyAnnotation]; exists {
		return annotations[OsFamilyAnnotation]
	}

	return OsFamilyAmazonLinux2
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

type InstancePoolType string

const (
	SubFamilyFlexible InstancePoolType = "SubFamilyFlexible"
)

type InstanceSpec struct {
	Type   string
	Weight string
}

type InstancePool struct {
	Type InstancePoolType
	Pool map[string][]InstanceSpec
}

func (p *InstancePool) GetPool(key string) ([]InstanceSpec, bool) {
	if val, ok := p.Pool[key]; ok {
		return val, true
	}
	return nil, false
}
