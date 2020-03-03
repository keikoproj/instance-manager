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

	log "github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
)

var (
	GroupVersionResource = schema.GroupVersionResource{
		Group:    "instancemgr.keikoproj.io",
		Version:  "v1alpha1",
		Resource: "instancegroups",
	}
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
	Type               string                  `json:"type"`
	CRDType            *CRDUpgradeStrategy     `json:"crd,omitempty"`
	RollingUpgradeType *RollingUpgradeStrategy `json:"rollingUpdate,omitempty"`
}

type RollingUpgradeStrategy struct {
	MaxBatchSize                  int      `json:"maxBatchSize,omitempty"`
	MinInstancesInService         int      `json:"minInstancesInService,omitempty"`
	MinSuccessfulInstancesPercent int      `json:"minSuccessfulInstancesPercent,omitempty"`
	PauseTime                     string   `json:"pauseTime,omitempty"`
	SuspendProcesses              []string `json:"suspendProcesses,omitempty"`
	WaitOnResourceSignals         bool     `json:"waitOnResourceSignals,omitempty"`
}

func (s *RollingUpgradeStrategy) GetMaxBatchSize() int {
	return s.MaxBatchSize
}

func (s *RollingUpgradeStrategy) SetMaxBatchSize(value int) {
	s.MaxBatchSize = value
}

func (s *RollingUpgradeStrategy) GetMinInstancesInService() int {
	return s.MinInstancesInService
}

func (s *RollingUpgradeStrategy) SetMinInstancesInService(value int) {
	s.MinInstancesInService = value
}

func (s *RollingUpgradeStrategy) GetMinSuccessfulInstancesPercent() int {
	return s.MinSuccessfulInstancesPercent
}

func (s *RollingUpgradeStrategy) SetMinSuccessfulInstancesPercent(value int) {
	s.MinSuccessfulInstancesPercent = value
}

func (s *RollingUpgradeStrategy) GetPauseTime() string {
	return s.PauseTime
}

func (s *RollingUpgradeStrategy) SetPauseTime(pauseTime string) {
	s.PauseTime = pauseTime
}

func (s *RollingUpgradeStrategy) GetWaitOnResourceSignals() bool {
	return s.WaitOnResourceSignals
}

func (s *RollingUpgradeStrategy) SetWaitOnResourceSignals(wait bool) {
	s.WaitOnResourceSignals = wait
}

func (s *RollingUpgradeStrategy) GetSuspendProcesses() []string {
	if s.SuspendProcesses == nil {
		s.SuspendProcesses = make([]string, 0)
	}
	return s.SuspendProcesses
}

func (s *RollingUpgradeStrategy) SetSuspendProcesses(processes []string) {
	if s.SuspendProcesses == nil {
		s.SuspendProcesses = make([]string, 0)
	}
	s.SuspendProcesses = processes
}

type CRDUpgradeStrategy struct {
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
	EKSCFSpec          *EKSCFSpec         `json:"eks-cf,omitempty"`
	EKSManagedSpec     *EKSManagedSpec    `json:"eks-managed,omitempty"`
	AwsUpgradeStrategy AwsUpgradeStrategy `json:"strategy"`
}

type EKSManagedSpec struct {
	MaxSize                 int64                    `json:"maxSize"`
	MinSize                 int64                    `json:"minSize"`
	EKSManagedConfiguration *EKSManagedConfiguration `json:"configuration"`
}

type EKSCFSpec struct {
	MaxSize            int32               `json:"maxSize,omitempty"`
	MinSize            int32               `json:"minSize,omitempty"`
	EKSCFConfiguration *EKSCFConfiguration `json:"configuration,omitempty"`
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

// EKSCFConfiguration defines the context of an AWS Instance Group using EKSCF
type EKSCFConfiguration struct {
	EksClusterName              string              `json:"clusterName,omitempty"`
	KeyPairName                 string              `json:"keyPairName,omitempty"`
	Image                       string              `json:"image,omitempty"`
	InstanceType                string              `json:"instanceType,omitempty"`
	NodeSecurityGroups          []string            `json:"securityGroups,omitempty"`
	VolSize                     int32               `json:"volSize,omitempty"`
	Subnets                     []string            `json:"subnets,omitempty"`
	BootstrapArguments          string              `json:"bootstrapArguments,omitempty"`
	SpotPrice                   string              `json:"spotPrice,omitempty"`
	Tags                        []map[string]string `json:"tags,omitempty"`
	ExistingRoleName            string              `json:"roleName,omitempty"`
	ExistingInstanceProfileName string              `json:"instanceProfileName,omitempty"`
	ManagedPolicies             []string            `json:"managedPolicies,omitempty"`
	MetricsCollection           []string            `json:"metricsCollection,omitempty"`
}

// InstanceGroupStatus defines the schema of resource Status
type InstanceGroupStatus struct {
	StackName                     string `json:"stackName,omitempty"`
	CurrentState                  string `json:"currentState,omitempty"`
	CurrentMin                    int    `json:"currentMin,omitempty"`
	CurrentMax                    int    `json:"currentMax,omitempty"`
	ActiveLaunchConfigurationName string `json:"activeLaunchConfigurationName,omitempty"`
	ActiveScalingGroupName        string `json:"activeScalingGroupName,omitempty"`
	NodesArn                      string `json:"nodesInstanceRoleArn,omitempty"`
	StrategyResourceName          string `json:"strategyResourceName,omitempty"`
	UsingSpotRecommendation       bool   `json:"usingSpotRecommendation,omitempty"`
	Lifecycle                     string `json:"lifecycle,omitempty"`
}

func (conf *EKSManagedConfiguration) SetSubnets(subnets []string)  { conf.Subnets = subnets }
func (conf *EKSManagedConfiguration) SetClusterName(name string)   { conf.EksClusterName = name }
func (conf *EKSManagedConfiguration) GetLabels() map[string]string { return conf.NodeLabels }

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

func (s *AwsUpgradeStrategy) GetRollingUpgradeStrategy() *RollingUpgradeStrategy {
	return s.RollingUpgradeType
}

func (s *AwsUpgradeStrategy) GetCRDType() *CRDUpgradeStrategy {
	return s.CRDType
}

func (s *AwsUpgradeStrategy) SetCRDType(crd *CRDUpgradeStrategy) {
	s.CRDType = crd
}

func (c *CRDUpgradeStrategy) GetSpec() string {
	return c.Spec
}

func (c *CRDUpgradeStrategy) SetSpec(body string) {
	c.Spec = body
}

func (c *CRDUpgradeStrategy) GetCRDName() string {
	return c.CRDName
}

func (c *CRDUpgradeStrategy) SetCRDName(name string) {
	c.CRDName = name
}

func (c *CRDUpgradeStrategy) GetConcurrencyPolicy() string {
	return c.ConcurrencyPolicy
}

func (c *CRDUpgradeStrategy) SetConcurrencyPolicy(policy string) {
	c.ConcurrencyPolicy = policy
}

func (c *CRDUpgradeStrategy) GetStatusJSONPath() string {
	return c.StatusJSONPath
}

func (c *CRDUpgradeStrategy) SetStatusJSONPath(path string) {
	c.StatusJSONPath = path
}

func (c *CRDUpgradeStrategy) GetStatusSuccessString() string {
	return c.StatusSuccessString
}

func (c *CRDUpgradeStrategy) SetStatusSuccessString(str string) {
	c.StatusSuccessString = str
}

func (c *CRDUpgradeStrategy) GetStatusFailureString() string {
	return c.StatusFailureString
}

func (c *CRDUpgradeStrategy) SetStatusFailureString(str string) {
	c.StatusFailureString = str
}

func (status *InstanceGroupStatus) GetActiveLaunchConfigurationName() string {
	return status.ActiveLaunchConfigurationName
}

func (status *InstanceGroupStatus) SetActiveLaunchConfigurationName(name string) {
	status.ActiveLaunchConfigurationName = name
}

func (status *InstanceGroupStatus) GetStackName() string {
	return status.StackName
}

func (status *InstanceGroupStatus) SetStackName(name string) {
	status.StackName = name
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

func (strategy *AwsUpgradeStrategy) GetType() string {
	return strategy.Type
}

func (strategy *AwsUpgradeStrategy) SetType(strategyType string) {
	strategy.Type = strategyType
}

func (spec *EKSCFSpec) GetMinSize() int32 {
	return spec.MinSize
}

func (spec *EKSCFSpec) SetMinSize(size int32) {
	spec.MinSize = size
}

func (spec *EKSCFSpec) GetMaxSize() int32 {
	return spec.MaxSize
}

func (spec *EKSCFSpec) SetMaxSize(size int32) {
	spec.MaxSize = size
}

func (conf *EKSCFConfiguration) GetKeyName() string {
	return conf.KeyPairName
}

func (conf *EKSCFConfiguration) SetKeyName(keypairName string) {
	conf.KeyPairName = keypairName
}

func (conf *EKSCFConfiguration) SetSpotPrice(price string) {
	conf.SpotPrice = price
}

func (conf *EKSCFConfiguration) GetSpotPrice() string {
	return conf.SpotPrice
}

func (conf *EKSCFConfiguration) GetImage() string {
	return conf.Image
}

func (conf *EKSCFConfiguration) SetImage(image string) {
	conf.Image = image
}

func (conf *EKSCFConfiguration) GetInstanceType() string {
	return conf.InstanceType
}

func (conf *EKSCFConfiguration) setInstanceType(instanceType string) {
	conf.InstanceType = instanceType
}

func (conf *EKSCFConfiguration) GetSubnets() []string {
	return conf.Subnets
}

func (conf *EKSCFConfiguration) SetSubnets(subnets []string) {
	conf.Subnets = subnets
}

func (conf *EKSCFConfiguration) GetSecurityGroups() []string {
	return conf.NodeSecurityGroups
}

func (conf *EKSCFConfiguration) SetSecurityGroups(securityGroups []string) {
	conf.NodeSecurityGroups = securityGroups
}

func (conf *EKSCFConfiguration) GetVolSize() int32 {
	return conf.VolSize
}

func (conf *EKSCFConfiguration) SetVolSize(s int32) {
	conf.VolSize = s
}

func (conf *EKSCFConfiguration) GetClusterName() string {
	return conf.EksClusterName
}

func (conf *EKSCFConfiguration) SetClusterName(clusterName string) {
	conf.EksClusterName = clusterName
}

func (conf *EKSCFConfiguration) GetBootstrapArgs() string {
	return conf.BootstrapArguments
}

func (conf *EKSCFConfiguration) SetBootstrapArgs(args string) {
	conf.BootstrapArguments = args
}

func (conf *EKSCFConfiguration) GetRoleName() string {
	return conf.ExistingRoleName
}

func (conf *EKSCFConfiguration) SetRoleName(role string) {
	conf.ExistingRoleName = role
}

func (conf *EKSCFConfiguration) GetInstanceProfileName() string {
	return conf.ExistingInstanceProfileName
}

func (conf *EKSCFConfiguration) SetInstanceProfileName(profile string) {
	conf.ExistingInstanceProfileName = profile
}

func (conf *EKSCFConfiguration) GetTags() []map[string]string {
	return conf.Tags
}

func (conf *EKSCFConfiguration) SetTags(tags []map[string]string) {
	conf.Tags = tags
}

func (conf *EKSCFConfiguration) GetMetricsCollection() []string {
	return conf.MetricsCollection
}

func (conf *EKSCFConfiguration) SetMetricsCollection(metricsCollection []string) {
	conf.MetricsCollection = metricsCollection
}

func (ig *InstanceGroup) GetState() ReconcileState {
	return ReconcileState(ig.Status.CurrentState)
}

func (ig *InstanceGroup) SetState(s ReconcileState) {
	ig.Status.CurrentState = fmt.Sprintf("%v", s)
	log.Printf("state transitioned to: %v", s)
}

func init() {
	SchemeBuilder.Register(&InstanceGroup{}, &InstanceGroupList{})
}
