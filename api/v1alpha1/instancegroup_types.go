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

// InstanceGroupSpec defines the schema of resource Spec
type InstanceGroupSpec struct {
	Provisioner        string             `json:"provisioner"`
	EKSCFSpec          *EKSCFSpec         `json:"eks-cf,omitempty"`
	EKSFargateSpec     *EKSFargateSpec    `json:"eks-fargate,omitempty"`
	AwsUpgradeStrategy AwsUpgradeStrategy `json:"strategy"`
}

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
	Type               string                 `json:"type"`
	CRDType            CRDUpgradeStrategy     `json:"crd,omitempty"`
	RollingUpgradeType RollingUpgradeStrategy `json:"rollingUpdate,omitempty"`
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

func (s *AwsUpgradeStrategy) GetRollingUpgradeStrategy() RollingUpgradeStrategy {
	return s.RollingUpgradeType
}

func (s *AwsUpgradeStrategy) GetCRDType() CRDUpgradeStrategy {
	return s.CRDType
}

func (s *AwsUpgradeStrategy) SetCRDType(crd CRDUpgradeStrategy) {
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
