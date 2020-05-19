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
	"strings"

	"github.com/keikoproj/instance-manager/controllers/common"
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

	LifecycleStateNormal      = "normal"
	LifecycleStateSpot        = "spot"
	CRDStrategyName           = "crd"
	RollingUpdateStrategyName = "rollingupdate"
	EKSProvisionerName        = "eks"
	EKSManagedProvisionerName = "eks-managed"

	NodesReady InstanceGroupConditionType = "NodesReady"
)

var (
	Strategies   = []string{"crd", "rollingupdate", "managed"}
	Provisioners = []string{"eks", "eks-managed"}

	DefaultRollingUpdateStrategy = &RollingUpdateStrategy{
		MaxUnavailable: &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 1,
		},
	}

	log = ctrl.Log.WithName("v1alpha1")
)

// InstanceGroup is the Schema for the instancegroups API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=instancegroups,scope=Namespaced,shortName=ig
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.currentState",description="current state of the instancegroup"
// +kubebuilder:printcolumn:name="Min",type="integer",JSONPath=".status.currentMin",description="currently set min instancegroup size"
// +kubebuilder:printcolumn:name="Max",type="integer",JSONPath=".status.currentMax",description="currently set max instancegroup size"
// +kubebuilder:printcolumn:name="Group Name",type="string",JSONPath=".status.activeScalingGroupName",description="instancegroup created scalinggroup name"
// +kubebuilder:printcolumn:name="Provisioner",type="string",JSONPath=".spec.provisioner",description="instance group provisioner"
// +kubebuilder:printcolumn:name="Strategy",type="string",JSONPath=".spec.strategy.type",description="instance group upgrade strategy"
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
	Type              string                 `json:"type"`
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
	Provisioner        string             `json:"provisioner"`
	EKSManagedSpec     *EKSManagedSpec    `json:"eks-managed,omitempty"`
	EKSSpec            *EKSSpec           `json:"eks,omitempty"`
	AwsUpgradeStrategy AwsUpgradeStrategy `json:"strategy"`
}

type EKSManagedSpec struct {
	MaxSize                 int64                    `json:"maxSize"`
	MinSize                 int64                    `json:"minSize"`
	EKSManagedConfiguration *EKSManagedConfiguration `json:"configuration"`
}

type EKSSpec struct {
	MaxSize          int64             `json:"maxSize"`
	MinSize          int64             `json:"minSize"`
	EKSConfiguration *EKSConfiguration `json:"configuration"`
}

type EKSConfiguration struct {
	EksClusterName              string              `json:"clusterName"`
	KeyPairName                 string              `json:"keyPairName"`
	Image                       string              `json:"image"`
	InstanceType                string              `json:"instanceType"`
	NodeSecurityGroups          []string            `json:"securityGroups,omitempty"`
	Volumes                     []NodeVolume        `json:"volumes,omitempty"`
	Subnets                     []string            `json:"subnets"`
	BootstrapArguments          string              `json:"bootstrapArguments,omitempty"`
	SpotPrice                   string              `json:"spotPrice,omitempty"`
	Tags                        []map[string]string `json:"tags,omitempty"`
	Labels                      map[string]string   `json:"labels,omitempty"`
	Taints                      []corev1.Taint      `json:"taints,omitempty"`
	ExistingRoleName            string              `json:"roleName,omitempty"`
	ExistingInstanceProfileName string              `json:"instanceProfileName,omitempty"`
	ManagedPolicies             []string            `json:"managedPolicies,omitempty"`
}

type NodeVolume struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	Size int64  `json:"size,omitempty"`
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

// InstanceGroupStatus defines the schema of resource Status
type InstanceGroupStatus struct {
	CurrentState                  string                   `json:"currentState,omitempty"`
	CurrentMin                    int                      `json:"currentMin,omitempty"`
	CurrentMax                    int                      `json:"currentMax,omitempty"`
	ActiveLaunchConfigurationName string                   `json:"activeLaunchConfigurationName,omitempty"`
	ActiveScalingGroupName        string                   `json:"activeScalingGroupName,omitempty"`
	NodesArn                      string                   `json:"nodesInstanceRoleArn,omitempty"`
	StrategyResourceName          string                   `json:"strategyResourceName,omitempty"`
	UsingSpotRecommendation       bool                     `json:"usingSpotRecommendation,omitempty"`
	Lifecycle                     string                   `json:"lifecycle,omitempty"`
	Conditions                    []InstanceGroupCondition `json:"conditions,omitempty"`
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
func (ig *InstanceGroup) GetStatus() *InstanceGroupStatus {
	return &ig.Status
}
func (ig *InstanceGroup) GetUpgradeStrategy() *AwsUpgradeStrategy {
	return &ig.Spec.AwsUpgradeStrategy
}
func (ig *InstanceGroup) SetUpgradeStrategy(strategy AwsUpgradeStrategy) {
	ig.Spec.AwsUpgradeStrategy = strategy
}
func (c *EKSConfiguration) Validate() error {
	if c.EksClusterName == "" {
		return errors.Errorf("validation failed, 'eksClusterName' is a required parameter")
	}
	if len(c.Subnets) == 0 {
		return errors.Errorf("validation failed, 'subnets' is a required parameter")
	}
	if len(c.NodeSecurityGroups) == 0 {
		return errors.Errorf("validation failed, 'securityGroups' is a required parameter")
	}
	if c.Image == "" {
		return errors.Errorf("validation failed, 'image' is a required parameter")
	}
	if c.InstanceType == "" {
		return errors.Errorf("validation failed, 'instanceType' is a required parameter")
	}
	if c.KeyPairName == "" {
		return errors.Errorf("validation failed, 'keyPair' is a required parameter")
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
	return nil
}
func (s *InstanceGroupSpec) Validate() error {
	if !common.ContainsEqualFold(Provisioners, s.Provisioner) {
		return errors.Errorf("validation failed, provisioner '%v' is invalid", s.Provisioner)
	}

	if strings.EqualFold(s.Provisioner, EKSProvisionerName) {
		if err := s.EKSSpec.EKSConfiguration.Validate(); err != nil {
			return err
		}
	}

	// TODO: Add validation for EKSManagedProvisioner

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
func (c *EKSConfiguration) SetManagedPolicies(policies []string) {
	c.ManagedPolicies = policies
}
func (c *EKSConfiguration) GetVolumes() []NodeVolume {
	return c.Volumes
}
func (c *EKSConfiguration) GetBootstrapArguments() string {
	return c.BootstrapArguments
}
func (c *EKSConfiguration) GetTags() []map[string]string {
	if c.Tags == nil {
		return []map[string]string{}
	}
	return c.Tags
}
func (c *EKSConfiguration) GetSubnets() []string {
	if c.Subnets == nil {
		return []string{}
	}
	return c.Subnets
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
func (spec *EKSSpec) GetMaxSize() int64 {
	return spec.MaxSize
}
func (spec *EKSSpec) GetMinSize() int64 {
	return spec.MinSize
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

	if strings.ToLower(c.ConcurrencyPolicy) != "forbid" && strings.ToLower(c.ConcurrencyPolicy) != "allow" {
		c.SetConcurrencyPolicy("forbid")
	}

	if strings.ToLower(c.ConcurrencyPolicy) == "" {
		c.SetConcurrencyPolicy("forbid")
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

func (status *InstanceGroupStatus) SetActiveLaunchConfigurationName(name string) {
	status.ActiveLaunchConfigurationName = name
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
