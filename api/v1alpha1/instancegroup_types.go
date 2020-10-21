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

package v1alpha1

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ReconcileState string

const (
	// Init States
	ReconcileInit        ReconcileState = "Init"
	ReconcileInitDelete  ReconcileState = "InitDelete"
	ReconcileInitUpdate  ReconcileState = "InitUpdate"
	ReconcileInitCreate  ReconcileState = "InitCreate"
	ReconcileInitUpgrade ReconcileState = "InitUpgrade"

	// Ongoing States
	ReconcileDeleting  ReconcileState = "Deleting"
	ReconcileDeleted   ReconcileState = "Deleted"
	ReconcileModifying ReconcileState = "ReconcileModifying"
	ReconcileModified  ReconcileState = "ReconcileModified"

	// End States
	ReconcileReady ReconcileState = "Ready"
	ReconcileErr   ReconcileState = "Error"

	// Userdata bootstrap stages
	PreBootstrapStage  = "PreBootstrap"
	PostBootstrapStage = "PostBootstrap"

	LifecycleStateNormal      = "normal"
	LifecycleStateSpot        = "spot"
	LifecycleStateMixed       = "mixed"
	CRDStrategyName           = "crd"
	RollingUpdateStrategyName = "rollingupdate"
	ManagedStrategyName       = "managed"
	EKSProvisionerName        = "eks"
	EKSManagedProvisionerName = "eks-managed"
	EKSFargateProvisionerName = "eks-fargate"

	NodesReady InstanceGroupConditionType = "NodesReady"

	ForbidConcurrencyPolicy  = "forbid"
	AllowConcurrencyPolicy   = "allow"
	ReplaceConcurrencyPolicy = "replace"

	FileSystemTypeXFS  = "xfs"
	FileSystemTypeEXT4 = "ext4"

	LifecycleHookResultAbandon           = "ABANDON"
	LifecycleHookResultContinue          = "CONTINUE"
	LifecycleHookTransitionLaunch        = "Launch"
	LifecycleHookTransitionTerminate     = "Terminate"
	LifecycleHookDefaultHeartbeatTimeout = 300

	LaunchTemplateStrategyCapacityOptimized = "CapacityOptimized"
	LaunchTemplateStrategyLowestPrice       = "LowestPrice"
)

type ScalingConfigurationType string

const (
	LaunchConfiguration ScalingConfigurationType = "LaunchConfiguration"
	LaunchTemplate      ScalingConfigurationType = "LaunchTemplate"
)

var (
	Strategies   = []string{CRDStrategyName, RollingUpdateStrategyName, ManagedStrategyName}
	Provisioners = []string{
		EKSProvisionerName,
		EKSManagedProvisionerName,
		EKSFargateProvisionerName,
	}
	EKSConfigurationTypes = []ScalingConfigurationType{
		LaunchConfiguration,
		LaunchTemplate,
	}

	DefaultRollingUpdateStrategy = &RollingUpdateStrategy{
		MaxUnavailable: &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 1,
		},
	}

	AllowedFileSystemTypes            = []string{FileSystemTypeXFS, FileSystemTypeEXT4}
	AllowedMixedPolicyStrategies      = []string{LaunchTemplateStrategyCapacityOptimized, LaunchTemplateStrategyLowestPrice}
	LifecycleHookAllowedTransitions   = []string{LifecycleHookTransitionLaunch, LifecycleHookTransitionTerminate}
	LifecycleHookAllowedDefaultResult = []string{LifecycleHookResultAbandon, LifecycleHookResultContinue}
	log                               = ctrl.Log.WithName("v1alpha1")
)

// InstanceGroup is the Schema for the instancegroups API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=instancegroups,scope=Namespaced,shortName=ig
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.currentState",description="current state of the instancegroup"
// +kubebuilder:printcolumn:name="Min",type="integer",JSONPath=".status.currentMin",description="currently set min instancegroup size"
// +kubebuilder:printcolumn:name="Max",type="integer",JSONPath=".status.currentMax",description="currently set max instancegroup size"
// +kubebuilder:printcolumn:name="Group Name",type="string",JSONPath=".status.activeScalingGroupName",description="instancegroup created scalinggroup name"
// +kubebuilder:printcolumn:name="Provisioner",type="string",JSONPath=".status.provisioner",description="instance group provisioner"
// +kubebuilder:printcolumn:name="Strategy",type="string",JSONPath=".status.strategy",description="instance group upgrade strategy"
// +kubebuilder:printcolumn:name="Lifecycle",type="string",JSONPath=".status.lifecycle",description="instance group lifecycle spot/normal"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="time passed since instancegroup creation"
type InstanceGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   InstanceGroupSpec   `json:"spec"`
	Status InstanceGroupStatus `json:"status,omitempty"`
}

// InstanceGroupList contains a list of InstanceGroup
// +kubebuilder:object:root=true
type InstanceGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstanceGroup `json:"items"`
}

// AwsUpgradeStrategy defines the upgrade strategy of an AWS Instance Group
type AwsUpgradeStrategy struct {
	Type              string                 `json:"type,omitempty"`
	CRDType           *CRDUpdateStrategy     `json:"crd,omitempty"`
	RollingUpdateType *RollingUpdateStrategy `json:"rollingUpdate,omitempty"`
}

type RollingUpdateStrategy struct {
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

func (s *RollingUpdateStrategy) GetMaxUnavailable() *intstr.IntOrString {
	return s.MaxUnavailable
}

func (s *RollingUpdateStrategy) SetMaxUnavailable(value *intstr.IntOrString) {
	s.MaxUnavailable = value
}

type CRDUpdateStrategy struct {
	Spec                string `json:"spec,omitempty"`
	CRDName             string `json:"crdName,omitempty"`
	ConcurrencyPolicy   string `json:"concurrencyPolicy,omitempty"`
	StatusJSONPath      string `json:"statusJSONPath,omitempty"`
	StatusSuccessString string `json:"statusSuccessString,omitempty"`
	StatusFailureString string `json:"statusFailureString,omitempty"`
}

// InstanceGroupSpec defines the schema of resource Spec
type InstanceGroupSpec struct {
	Provisioner        string             `json:"provisioner,omitempty"`
	EKSManagedSpec     *EKSManagedSpec    `json:"eks-managed,omitempty"`
	EKSFargateSpec     *EKSFargateSpec    `json:"eks-fargate,omitempty"`
	EKSSpec            *EKSSpec           `json:"eks,omitempty"`
	AwsUpgradeStrategy AwsUpgradeStrategy `json:"strategy,omitempty"`
}

type EKSManagedSpec struct {
	MaxSize                 int64                    `json:"maxSize"`
	MinSize                 int64                    `json:"minSize"`
	EKSManagedConfiguration *EKSManagedConfiguration `json:"configuration"`
}

type EKSSpec struct {
	MaxSize          int64                    `json:"maxSize,omitempty"`
	MinSize          int64                    `json:"minSize,omitempty"`
	Type             ScalingConfigurationType `json:"type,omitempty"`
	EKSConfiguration *EKSConfiguration        `json:"configuration"`
}

type EKSConfiguration struct {
	EksClusterName              string                    `json:"clusterName,omitempty"`
	KeyPairName                 string                    `json:"keyPairName,omitempty"`
	Image                       string                    `json:"image,omitempty"`
	InstanceType                string                    `json:"instanceType,omitempty"`
	NodeSecurityGroups          []string                  `json:"securityGroups,omitempty"`
	Volumes                     []NodeVolume              `json:"volumes,omitempty"`
	Subnets                     []string                  `json:"subnets,omitempty"`
	SuspendedProcesses          []string                  `json:"suspendProcesses,omitempty"`
	BootstrapArguments          string                    `json:"bootstrapArguments,omitempty"`
	SpotPrice                   string                    `json:"spotPrice,omitempty"`
	Tags                        []map[string]string       `json:"tags,omitempty"`
	Labels                      map[string]string         `json:"labels,omitempty"`
	Taints                      []corev1.Taint            `json:"taints,omitempty"`
	UserData                    []UserDataStage           `json:"userData,omitempty"`
	ExistingRoleName            string                    `json:"roleName,omitempty"`
	ExistingInstanceProfileName string                    `json:"instanceProfileName,omitempty"`
	ManagedPolicies             []string                  `json:"managedPolicies,omitempty"`
	MetricsCollection           []string                  `json:"metricsCollection,omitempty"`
	LifecycleHooks              []LifecycleHookSpec       `json:"lifecycleHooks,omitempty"`
	MixedInstancesPolicy        *MixedInstancesPolicySpec `json:"mixedInstancesPolicy,omitempty"`
}

type MixedInstancesPolicySpec struct {
	Strategy      *string             `json:"strategy,omitempty"`
	SpotPools     *int64              `json:"spotPools,omitempty"`
	BaseCapacity  *int64              `json:"baseCapacity,omitempty"`
	SpotRatio     *intstr.IntOrString `json:"spotRatio,omitempty"`
	InstancePool  *string             `json:"instancePool,omitempty"`
	InstanceTypes []*InstanceTypeSpec `json:"instanceTypes,omitempty"`
}

type InstanceTypeSpec struct {
	Type   string `json:"type"`
	Weight int64  `json:"weight,omitempty"`
}

type LifecycleHookSpec struct {
	Name             string `json:"name"`
	Lifecycle        string `json:"lifecycle"`
	DefaultResult    string `json:"defaultResult,omitempty"`
	HeartbeatTimeout int64  `json:"heartbeatTimeout,omitempty"`
	NotificationArn  string `json:"notificationArn,omitempty"`
	Metadata         string `json:"metadata,omitempty"`
	RoleArn          string `json:"roleArn,omitempty"`
}

type UserDataStage struct {
	Name  string `json:"name,omitempty"`
	Stage string `json:"stage"`
	Data  string `json:"data"`
}

type NodeVolume struct {
	Name                string                  `json:"name"`
	Type                string                  `json:"type"`
	Size                int64                   `json:"size"`
	Iops                int64                   `json:"iops,omitempty"`
	DeleteOnTermination *bool                   `json:"deleteOnTermination,omitempty"`
	Encrypted           *bool                   `json:"encrypted,omitempty"`
	SnapshotID          string                  `json:"snapshotId,omitempty"`
	MountOptions        *NodeVolumeMountOptions `json:"mountOptions,omitempty"`
}

type NodeVolumeMountOptions struct {
	FileSystem  string `json:"fileSystem,omitempty"`
	Mount       string `json:"mount,omitempty"`
	Persistance *bool  `json:"persistance,omitempty"`
}

type EKSFargateSpec struct {
	ClusterName         string                `json:"clusterName"`
	PodExecutionRoleArn string                `json:"podExecutionRoleArn,omitempty"`
	Subnets             []string              `json:"subnets,omitempty"`
	Selectors           []EKSFargateSelectors `json:"selectors"`
	Tags                []map[string]string   `json:"tags,omitempty"`
}

type EKSManagedConfiguration struct {
	EksClusterName     string              `json:"clusterName,omitempty"`
	VolSize            int64               `json:"volSize,omitempty"`
	InstanceType       string              `json:"instanceType,omitempty"`
	NodeLabels         map[string]string   `json:"nodeLabels,omitempty"`
	NodeRole           string              `json:"nodeRole,omitempty"`
	NodeSecurityGroups []string            `json:"securityGroups,omitempty"`
	KeyPairName        string              `json:"keyPairName,omitempty"`
	Tags               []map[string]string `json:"tags,omitempty"`
	Subnets            []string            `json:"subnets,omitempty"`
	AmiType            string              `json:"amiType,omitempty"`
	ReleaseVersion     string              `json:"releaseVersion,omitempty"`
	Version            string              `json:"version,omitempty"`
}

type EKSFargateSelectors struct {
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// InstanceGroupStatus defines the schema of resource Status
type InstanceGroupStatus struct {
	CurrentState                  string                   `json:"currentState,omitempty"`
	CurrentMin                    int                      `json:"currentMin,omitempty"`
	CurrentMax                    int                      `json:"currentMax,omitempty"`
	ActiveLaunchConfigurationName string                   `json:"activeLaunchConfigurationName,omitempty"`
	ActiveLaunchTemplateName      string                   `json:"activeLaunchTemplateName,omitempty"`
	LatestTemplateVersion         string                   `json:"latestTemplateVersion,omitempty"`
	ActiveScalingGroupName        string                   `json:"activeScalingGroupName,omitempty"`
	NodesArn                      string                   `json:"nodesInstanceRoleArn,omitempty"`
	StrategyResourceName          string                   `json:"strategyResourceName,omitempty"`
	UsingSpotRecommendation       bool                     `json:"usingSpotRecommendation,omitempty"`
	Lifecycle                     string                   `json:"lifecycle,omitempty"`
	ConfigHash                    string                   `json:"configMD5,omitempty"`
	Conditions                    []InstanceGroupCondition `json:"conditions,omitempty"`
	Provisioner                   string                   `json:"provisioner,omitempty"`
	Strategy                      string                   `json:"strategy,omitempty"`
}

type InstanceGroupConditionType string

func NewInstanceGroupCondition(cType InstanceGroupConditionType, status corev1.ConditionStatus) InstanceGroupCondition {
	return InstanceGroupCondition{
		Type:   cType,
		Status: status,
	}
}

// InstanceGroupConditions describes the conditions of the InstanceGroup
type InstanceGroupCondition struct {
	Type   InstanceGroupConditionType `json:"type,omitempty"`
	Status corev1.ConditionStatus     `json:"status,omitempty"`
}

func (ig *InstanceGroup) GetEKSConfiguration() *EKSConfiguration {
	return ig.Spec.EKSSpec.EKSConfiguration
}
func (ig *InstanceGroup) GetEKSSpec() *EKSSpec {
	return ig.Spec.EKSSpec
}
func (ig *InstanceGroup) NamespacedName() string {
	return fmt.Sprintf("%v/%v", ig.GetNamespace(), ig.GetName())
}
func (ig *InstanceGroup) GetStatus() *InstanceGroupStatus {
	return &ig.Status
}
func (ig *InstanceGroup) GetUpgradeStrategy() *AwsUpgradeStrategy {
	return &ig.Spec.AwsUpgradeStrategy
}
func (ig *InstanceGroup) SetUpgradeStrategy(strategy AwsUpgradeStrategy) {
	ig.Spec.AwsUpgradeStrategy = strategy
}

func (s *EKSSpec) Validate() error {
	if s.EKSConfiguration == nil {
		return errors.Errorf("validation failed, 'configuration' is a required field")
	}

	if s.Type != LaunchConfiguration && s.Type != LaunchTemplate {
		s.Type = LaunchConfiguration
	}

	return nil
}

func (c *EKSConfiguration) Validate() error {
	if common.StringEmpty(c.EksClusterName) {
		return errors.Errorf("validation failed, 'clusterName' is a required parameter")
	}
	if common.SliceEmpty(c.Subnets) {
		return errors.Errorf("validation failed, 'subnets' is a required parameter")
	}
	if common.SliceEmpty(c.NodeSecurityGroups) {
		return errors.Errorf("validation failed, 'securityGroups' is a required parameter")
	}
	for _, m := range c.MetricsCollection {
		metrics := make([]string, 0)
		if strings.EqualFold(m, "all") {
			continue
		}
		if common.ContainsString(awsprovider.DefaultAutoscalingMetrics, m) {
			metrics = append(metrics, m)
		}
		c.MetricsCollection = metrics
	}

	for _, m := range c.SuspendedProcesses {
		processes := make([]string, 0)
		if strings.EqualFold(m, "all") {
			continue
		}
		if common.ContainsString(awsprovider.DefaultSuspendProcesses, m) {
			processes = append(processes, m)
		}
		c.SuspendedProcesses = processes
	}

	hooks := []LifecycleHookSpec{}
	for _, h := range c.LifecycleHooks {
		if h.HeartbeatTimeout == 0 {
			h.HeartbeatTimeout = LifecycleHookDefaultHeartbeatTimeout
		}
		if common.StringEmpty(h.DefaultResult) {
			h.DefaultResult = LifecycleHookResultAbandon
		}
		if common.ContainsEqualFold(LifecycleHookAllowedDefaultResult, h.DefaultResult) {
			h.DefaultResult = strings.ToUpper(h.DefaultResult)
		} else {
			h.DefaultResult = LifecycleHookResultAbandon
		}
		if !common.ContainsEqualFold(LifecycleHookAllowedTransitions, h.Lifecycle) {
			return errors.Errorf("validation failed, 'lifecycle' is a required parameter and must be in %+v", LifecycleHookAllowedTransitions)
		}
		if strings.EqualFold(h.Lifecycle, LifecycleHookTransitionLaunch) {
			h.Lifecycle = awsprovider.LifecycleHookTransitionLaunch
		} else if strings.EqualFold(h.Lifecycle, LifecycleHookTransitionTerminate) {
			h.Lifecycle = awsprovider.LifecycleHookTransitionTerminate
		}
		if common.StringEmpty(h.Name) {
			return errors.Errorf("validation failed, 'name' is a required parameter")
		}
		if !common.StringEmpty(h.NotificationArn) && !strings.HasPrefix(h.NotificationArn, awsprovider.ARNPrefix) {
			return errors.Errorf("validation failed, 'notificationArn' must be a valid IAM role ARN")
		}
		if !common.StringEmpty(h.RoleArn) && !strings.HasPrefix(h.NotificationArn, awsprovider.ARNPrefix) {
			return errors.Errorf("validation failed, 'roleArn' must be a valid IAM role ARN")
		}
		hooks = append(hooks, h)
	}
	c.SetLifecycleHooks(hooks)

	if common.StringEmpty(c.Image) {
		return errors.Errorf("validation failed, 'image' is a required parameter")
	}
	if common.StringEmpty(c.InstanceType) {
		return errors.Errorf("validation failed, 'instanceType' is a required parameter")
	}
	if common.StringEmpty(c.KeyPairName) {
		return errors.Errorf("validation failed, 'keyPair' is a required parameter")
	}

	for _, v := range c.Volumes {
		if !common.ContainsEqualFold(awsprovider.AllowedVolumeTypes, v.Type) {
			return errors.Errorf("validation failed, volume type '%v' is unsuppoeted", v.Type)
		}

		if v.Iops != 0 && !strings.EqualFold(v.Type, "io1") {
			log.Info("cannot apply IOPS configuration for volumeType, only type 'io1' supported", "volumeType", v.Type)
		}

		if v.SnapshotID != "" {
			if v.Size > 0 {
				return errors.Errorf("validation failed, 'volume.snapshotId' and 'volume.size' are mutually exclusive")
			}
		}
		if v.Iops != 0 && v.Iops < 100 {
			return errors.Errorf("validation failed, volume IOPS must be min 100")
		}
	}

	if len(c.Volumes) == 0 {
		c.Volumes = []NodeVolume{
			{
				Name: "/dev/xvda",
				Type: "gp2",
				Size: 32,
			},
		}
	}

	if c.MixedInstancesPolicy != nil {
		if err := c.MixedInstancesPolicy.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (m *MixedInstancesPolicySpec) Validate() error {
	if m.Strategy == nil {
		defaultStrategy := LaunchTemplateStrategyCapacityOptimized
		m.Strategy = &defaultStrategy
	}
	if !common.ContainsEqualFold(AllowedMixedPolicyStrategies, *m.Strategy) {
		return errors.Errorf("validation failed, mixedInstancesPolicy.Strategy must either be LowestPrice or CapacityOptimized, got '%v'", *m.Strategy)
	}
	if m.SpotPools != nil {
		if !strings.EqualFold(*m.Strategy, LaunchTemplateStrategyLowestPrice) {
			return errors.Errorf("validation failed, can only use spotPools with LowestPrice strategy")
		}
	}
	if m.InstanceTypes != nil {
		for _, t := range m.InstanceTypes {
			if t.Weight == 0 {
				t.Weight = 1
			}
		}
	} else if m.InstancePool == nil {
		return errors.Errorf("validation failed, must provide either instancePool or instanceTypes when using mixedInstancesPolicy")
	}

	if m.SpotRatio == nil {
		// default is 100% on-demand
		defaultRatio := intstr.FromInt(100)
		m.SpotRatio = &defaultRatio
	}
	return nil
}

func (ig *InstanceGroup) Validate() error {
	s := ig.Spec

	if !common.ContainsEqualFold(Provisioners, s.Provisioner) {
		return errors.Errorf("validation failed, provisioner '%v' is invalid", s.Provisioner)
	}

	if strings.EqualFold(s.Provisioner, EKSFargateProvisionerName) {
		if err := s.EKSFargateSpec.Validate(); err != nil {
			return err
		}
		if !strings.EqualFold(s.AwsUpgradeStrategy.Type, ManagedStrategyName) {
			return errors.Errorf("validation failed, strategy '%v' is invalid for the eks-fargate provisioner", s.AwsUpgradeStrategy.Type)
		}
	}

	if strings.EqualFold(s.Provisioner, EKSProvisionerName) {
		config := ig.GetEKSConfiguration()
		spec := ig.GetEKSSpec()

		if err := spec.Validate(); err != nil {
			return err
		}

		if err := config.Validate(); err != nil {
			return err
		}
	}

	if s.AwsUpgradeStrategy.Type == "" {
		s.AwsUpgradeStrategy.Type = RollingUpdateStrategyName
	}

	if !common.ContainsEqualFold(Strategies, s.AwsUpgradeStrategy.Type) {
		return errors.Errorf("validation failed, strategy '%v' is invalid", s.AwsUpgradeStrategy.Type)
	}

	if strings.EqualFold(s.AwsUpgradeStrategy.Type, CRDStrategyName) && s.AwsUpgradeStrategy.CRDType == nil {
		return errors.Errorf("validation failed, strategy.crd is required")
	}

	if strings.EqualFold(s.AwsUpgradeStrategy.Type, CRDStrategyName) && s.AwsUpgradeStrategy.CRDType != nil {
		if err := s.AwsUpgradeStrategy.CRDType.Validate(); err != nil {
			return err
		}
	}

	if strings.EqualFold(s.AwsUpgradeStrategy.Type, RollingUpdateStrategyName) && s.AwsUpgradeStrategy.RollingUpdateType == nil {
		s.AwsUpgradeStrategy.RollingUpdateType = DefaultRollingUpdateStrategy
	}

	return nil
}
func (c *EKSConfiguration) GetRoleName() string {
	return c.ExistingRoleName
}
func (c *EKSConfiguration) GetMixedInstancesPolicy() *MixedInstancesPolicySpec {
	return c.MixedInstancesPolicy
}
func (c *EKSConfiguration) GetLifecycleHooks() []LifecycleHookSpec {
	return c.LifecycleHooks
}
func (c *EKSConfiguration) SetLifecycleHooks(hooks []LifecycleHookSpec) {
	c.LifecycleHooks = hooks
}
func (h LifecycleHookSpec) ExistInSlice(hooks []LifecycleHookSpec) bool {
	for _, hook := range hooks {
		if reflect.DeepEqual(hook, h) {
			return true
		}
	}
	return false
}
func (c *EKSConfiguration) GetInstanceProfileName() string {
	return c.ExistingInstanceProfileName
}
func (c *EKSConfiguration) HasExistingRole() bool {
	return c.ExistingRoleName != ""
}
func (c *EKSConfiguration) SetRoleName(role string) {
	c.ExistingRoleName = role
}
func (c *EKSConfiguration) SetInstanceProfileName(profile string) {
	c.ExistingInstanceProfileName = profile
}
func (c *EKSConfiguration) GetClusterName() string {
	return c.EksClusterName
}
func (c *EKSConfiguration) SetClusterName(name string) {
	c.EksClusterName = name
}
func (c *EKSConfiguration) GetLabels() map[string]string {
	return c.Labels
}
func (c *EKSConfiguration) SetLabels(labels map[string]string) {
	c.Labels = labels
}
func (c *EKSConfiguration) GetTaints() []corev1.Taint {
	return c.Taints
}
func (c *EKSConfiguration) SetTaints(taints []corev1.Taint) {
	c.Taints = taints
}
func (c *EKSConfiguration) GetManagedPolicies() []string {
	return c.ManagedPolicies
}
func (c *EKSConfiguration) GetUserData() []UserDataStage {
	return c.UserData
}
func (c *EKSConfiguration) SetManagedPolicies(policies []string) {
	c.ManagedPolicies = policies
}
func (c *EKSConfiguration) GetMetricsCollection() []string {
	return c.MetricsCollection
}
func (c *EKSConfiguration) SetMetricsCollection(metrics []string) {
	c.MetricsCollection = metrics
}
func (c *EKSConfiguration) GetVolumes() []NodeVolume {
	return c.Volumes
}
func (c *EKSConfiguration) GetBootstrapArguments() string {
	return c.BootstrapArguments
}
func (c *EKSConfiguration) GetSecurityGroups() []string {
	if c.NodeSecurityGroups == nil {
		return []string{}
	}
	return c.NodeSecurityGroups
}
func (c *EKSConfiguration) GetTags() []map[string]string {
	if c.Tags == nil {
		return []map[string]string{}
	}
	return c.Tags
}
func (c *EKSConfiguration) SetTags(tags []map[string]string) {
	c.Tags = tags
}
func (c *EKSConfiguration) GetSubnets() []string {
	if c.Subnets == nil {
		return []string{}
	}
	return c.Subnets
}
func (c *EKSConfiguration) GetSuspendProcesses() []string {
	if c.SuspendedProcesses == nil {
		return []string{}
	}
	return c.SuspendedProcesses
}
func (c *EKSConfiguration) GetSpotPrice() string {
	return c.SpotPrice
}
func (c *EKSConfiguration) SetSpotPrice(price string) {
	c.SpotPrice = price
}
func (c *EKSConfiguration) SetSubnets(subnets []string) {
	c.Subnets = subnets
}
func (c *EKSConfiguration) SetSuspendProcesses(suspendProcesses []string) {
	c.SuspendedProcesses = suspendProcesses
}
func (spec *EKSSpec) GetMaxSize() int64 {
	return spec.MaxSize
}
func (spec *EKSSpec) GetMinSize() int64 {
	return spec.MinSize
}
func (spec *EKSSpec) GetType() ScalingConfigurationType {
	return spec.Type
}

func (conf *EKSManagedConfiguration) SetSubnets(subnets []string) {
	conf.Subnets = subnets
}

func (conf *EKSManagedConfiguration) SetClusterName(name string) {
	conf.EksClusterName = name
}

func (conf *EKSManagedConfiguration) GetLabels() map[string]string {
	return conf.NodeLabels
}

func (ig *InstanceGroup) GetEKSManagedConfiguration() *EKSManagedConfiguration {
	return ig.Spec.EKSManagedSpec.EKSManagedConfiguration
}

func (ig *InstanceGroup) GetEKSManagedSpec() *EKSManagedSpec {
	return ig.Spec.EKSManagedSpec
}

func (spec *EKSManagedSpec) GetMaxSize() int64 {
	return spec.MaxSize
}

func (spec *EKSManagedSpec) GetMinSize() int64 {
	return spec.MinSize
}

func (s *AwsUpgradeStrategy) GetRollingUpdateType() *RollingUpdateStrategy {
	return s.RollingUpdateType
}

func (s *AwsUpgradeStrategy) SetRollingUpdateType(ru *RollingUpdateStrategy) {
	s.RollingUpdateType = ru
}

func (s *AwsUpgradeStrategy) GetCRDType() *CRDUpdateStrategy {
	return s.CRDType
}

func (s *AwsUpgradeStrategy) SetCRDType(crd *CRDUpdateStrategy) {
	s.CRDType = crd
}

func (c *CRDUpdateStrategy) Validate() error {
	if c.GetSpec() == "" {
		return errors.New("spec is empty")
	}

	allowedPolicies := []string{ReplaceConcurrencyPolicy, AllowConcurrencyPolicy, ForbidConcurrencyPolicy}

	if !common.ContainsEqualFold(allowedPolicies, c.ConcurrencyPolicy) {
		c.SetConcurrencyPolicy(ForbidConcurrencyPolicy)
	}

	if c.GetCRDName() == "" {
		return errors.New("crdName is empty")
	}

	if c.GetStatusJSONPath() == "" {
		return errors.New("statusJSONPath is empty")
	}

	if c.GetStatusSuccessString() == "" {
		return errors.New("statusSuccessString is empty")
	}

	if c.GetStatusFailureString() == "" {
		return errors.New("statusFailureString is empty")
	}
	return nil
}

func (c *CRDUpdateStrategy) GetSpec() string {
	return c.Spec
}

func (c *CRDUpdateStrategy) SetSpec(body string) {
	c.Spec = body
}

func (c *CRDUpdateStrategy) GetCRDName() string {
	return c.CRDName
}

func (c *CRDUpdateStrategy) SetCRDName(name string) {
	c.CRDName = name
}

func (c *CRDUpdateStrategy) GetConcurrencyPolicy() string {
	return c.ConcurrencyPolicy
}

func (c *CRDUpdateStrategy) SetConcurrencyPolicy(policy string) {
	c.ConcurrencyPolicy = policy
}

func (c *CRDUpdateStrategy) GetStatusJSONPath() string {
	return c.StatusJSONPath
}

func (c *CRDUpdateStrategy) SetStatusJSONPath(path string) {
	c.StatusJSONPath = path
}

func (c *CRDUpdateStrategy) GetStatusSuccessString() string {
	return c.StatusSuccessString
}

func (c *CRDUpdateStrategy) SetStatusSuccessString(str string) {
	c.StatusSuccessString = str
}

func (c *CRDUpdateStrategy) GetStatusFailureString() string {
	return c.StatusFailureString
}

func (c *CRDUpdateStrategy) SetStatusFailureString(str string) {
	c.StatusFailureString = str
}

func (status *InstanceGroupStatus) GetActiveLaunchConfigurationName() string {
	return status.ActiveLaunchConfigurationName
}

func (status *InstanceGroupStatus) GetActiveLaunchTemplateName() string {
	return status.ActiveLaunchTemplateName
}

func (status *InstanceGroupStatus) SetActiveLaunchConfigurationName(name string) {
	status.ActiveLaunchConfigurationName = name
}

func (status *InstanceGroupStatus) SetActiveLaunchTemplateName(name string) {
	status.ActiveLaunchTemplateName = name
}

func (status *InstanceGroupStatus) SetLatestTemplateVersion(version string) {
	status.LatestTemplateVersion = version
}

func (status *InstanceGroupStatus) GetConfigHash() string {
	return status.ConfigHash
}

func (status *InstanceGroupStatus) SetConfigHash(hash string) {
	status.ConfigHash = hash
}

func (status *InstanceGroupStatus) GetNodesReadyCondition() corev1.ConditionStatus {
	for _, c := range status.Conditions {
		if c.Type == NodesReady {
			return c.Status
		}
	}
	return corev1.ConditionFalse
}

func (status *InstanceGroupStatus) GetActiveScalingGroupName() string {
	return status.ActiveScalingGroupName
}

func (status *InstanceGroupStatus) SetActiveScalingGroupName(name string) {
	status.ActiveScalingGroupName = name
}

func (status *InstanceGroupStatus) SetStrategy(strategy string) {
	status.Strategy = strategy
}

func (status *InstanceGroupStatus) SetProvisioner(provisioner string) {
	status.Provisioner = provisioner
}

func (status *InstanceGroupStatus) GetNodesArn() string {
	return status.NodesArn
}

func (status *InstanceGroupStatus) SetNodesArn(arn string) {
	status.NodesArn = arn
}

func (status *InstanceGroupStatus) GetStrategyResourceName() string {
	return status.StrategyResourceName
}

func (status *InstanceGroupStatus) SetStrategyResourceName(name string) {
	status.StrategyResourceName = name
}

func (status *InstanceGroupStatus) GetCurrentMin() int {
	return status.CurrentMin
}

func (status *InstanceGroupStatus) SetCurrentMin(min int) {
	status.CurrentMin = min
}

func (status *InstanceGroupStatus) GetCurrentMax() int {
	return status.CurrentMax
}

func (status *InstanceGroupStatus) SetCurrentMax(max int) {
	status.CurrentMax = max
}

func (status *InstanceGroupStatus) GetUsingSpotRecommendation() bool {
	return status.UsingSpotRecommendation
}

func (status *InstanceGroupStatus) SetUsingSpotRecommendation(condition bool) {
	status.UsingSpotRecommendation = condition
}

func (status *InstanceGroupStatus) GetLifecycle() string {
	return status.Lifecycle
}

func (status *InstanceGroupStatus) SetLifecycle(phase string) {
	status.Lifecycle = phase
}

func (status *InstanceGroupStatus) GetConditions() []InstanceGroupCondition {
	return status.Conditions
}

func (status *InstanceGroupStatus) SetConditions(conditions []InstanceGroupCondition) {
	status.Conditions = conditions
}

func (strategy *AwsUpgradeStrategy) GetType() string {
	return strategy.Type
}

func (strategy *AwsUpgradeStrategy) SetType(strategyType string) {
	strategy.Type = strategyType
}

func (ig *InstanceGroup) GetState() ReconcileState {
	return ReconcileState(ig.Status.CurrentState)
}

func (ig *InstanceGroup) SetState(s ReconcileState) {
	log.Info("state transition occured",
		"instancegroup", ig.GetName(),
		"state", s,
		"previousState", ig.Status.CurrentState,
	)
	ig.Status.CurrentState = string(s)
}

func init() {
	SchemeBuilder.Register(&InstanceGroup{}, &InstanceGroupList{})
}

func (ig *InstanceGroup) GetEKSFargateSpec() *EKSFargateSpec {
	return ig.Spec.EKSFargateSpec
}

func (spec *EKSFargateSpec) Validate() error {
	return nil
}

func (spec *EKSFargateSpec) GetClusterName() string {
	return spec.ClusterName
}

func (spec *EKSFargateSpec) SetClusterName(name string) {
	spec.ClusterName = name
}

func (spec *EKSFargateSpec) GetPodExecutionRoleArn() string {
	return spec.PodExecutionRoleArn
}

func (spec *EKSFargateSpec) SetPodExecutionRoleArn(arn string) {
	spec.PodExecutionRoleArn = arn
}

func (spec *EKSFargateSpec) GetSubnets() []string {
	return spec.Subnets
}

func (spec *EKSFargateSpec) SetSubnets(subnets []string) {
	spec.Subnets = subnets
}

func (spec *EKSFargateSpec) GetSelectors() []EKSFargateSelectors {
	return spec.Selectors
}

func (spec *EKSFargateSpec) SetSelectors(selectors []EKSFargateSelectors) {
	spec.Selectors = selectors
}

func (spec *EKSFargateSpec) GetTags() []map[string]string {
	return spec.Tags
}

func (spec *EKSFargateSpec) SetTags(tags []map[string]string) {
	spec.Tags = tags
}
