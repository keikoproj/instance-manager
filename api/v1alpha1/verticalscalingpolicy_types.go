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

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerticalScalingPolicySpec defines the desired state of VerticalScalingPolicy
type VerticalScalingPolicySpec struct {
	InstanceFamily string                       `json:"instanceFamily,omitempty"`
	Resources      *corev1.ResourceRequirements `json:"resources"`
	Targets        []*corev1.ObjectReference    `json:"scaleTargetsRef"`
	Behavior       *BehaviorSpec                `json:"behavior"`
}

type BehaviorSpec struct {
	ScaleDown *ScalingSpec `json:"scaleDown,omitempty"`
	ScaleUp   *ScalingSpec `json:"scaleUp,omitempty"`
}

type ScalingSpec struct {
	StabilizationWindowSeconds int           `json:"stabilizationWindowSeconds,omitempty"`
	Policies                   []*PolicySpec `json:"policies,omitempty"`
}

type PolicySpec struct {
	Type          UtilizationType `json:"type,omitempty"`
	Value         int             `json:"value,omitempty"`
	PeriodSeconds int             `json:"periodSeconds,omitempty"`
}

// VerticalScalingPolicyStatus defines the observed state of VerticalScalingPolicy
type VerticalScalingPolicyStatus struct {
	CurrentState string `json:"currentState,omitempty"`
	// store last reconcile time to check with stabilizationWindow whether to perform next scale up/down
	TargetStatuses map[string]*TargetStatus `json:"targetStatuses,omitempty"`
}

type TargetStatus struct {
	State               string                  `json:"state,omitempty"`
	LastTransitionTime  metav1.Time             `json:"lastTransitionTime,omitempty"`
	DesiredInstanceType string                  `json:"desiredInstanceType,omitempty"`
	Conditions          []*UtilizationCondition `json:"conditions,omitempty"`
}

type UtilizationType string

const (
	CPUUtilizationPercent        UtilizationType = "CPUUtilizationPercentage"
	MemoryUtilizationPercent     UtilizationType = "MemoryUtilizationPercentage"
	NodesCountUtilizationPercent UtilizationType = "NodesCountUtilizationPercentage"

	CPUBelowScaleDownThreshold    UtilizationType = "CPUBelowScaleDownThreshold"
	MemoryBelowScaleDownThreshold UtilizationType = "MemoryBelowScaleDownThreshold"

	CPUAboveScaleUpThreshold        UtilizationType = "CPUAboveScaleUpThreshold"
	MemoryAboveScaleUpThreshold     UtilizationType = "MemoryAboveScaleUpThreshold"
	NodesCountAboveScaleUpThreshold UtilizationType = "NodesCountAboveScaleUpThreshold"
)

// NodeCondition contains condition information for a node.
type UtilizationCondition struct {
	// Type of utilization condition.
	Type UtilizationType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=UtilizationType"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// Last time we got an update on a given condition.
	// +optional
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime,omitempty" protobuf:"bytes,3,opt,name=lastHeartbeatTime"`
	// Last time the condition transit from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// (brief) reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`
	// Human readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VerticalScalingPolicy is the Schema for the verticalscalingpolicies API
type VerticalScalingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   *VerticalScalingPolicySpec   `json:"spec,omitempty"`
	Status *VerticalScalingPolicyStatus `json:"status,omitempty"`
}

func (v *VerticalScalingPolicy) InstanceFamily() (string, bool) {
	return v.Spec.InstanceFamily, v.Spec.InstanceFamily != ""
}

//+kubebuilder:object:root=true

// VerticalScalingPolicyList contains a list of VerticalScalingPolicy
type VerticalScalingPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticalScalingPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticalScalingPolicy{}, &VerticalScalingPolicyList{})
}

func (scalingSpec *ScalingSpec) GetPolicy(name UtilizationType) *PolicySpec {
	for _, policy := range scalingSpec.Policies {
		if policy.Type == name {
			return policy
		}
	}

	return nil
}

func (status *VerticalScalingPolicyStatus) GetCondition(igName string, t UtilizationType) *UtilizationCondition {
	for _, condition := range status.TargetStatuses[igName].Conditions {
		if condition.Type == t {
			return condition
		}
	}
	return &UtilizationCondition{}
}

func (status *VerticalScalingPolicyStatus) SetCondition(igName string, condition *UtilizationCondition) {
	for _, c := range status.TargetStatuses[igName].Conditions {
		if condition.Type == c.Type {
			c = condition
			break
		}
	}
}

func (status *VerticalScalingPolicyStatus) SetConditions(igName string, conditions []*UtilizationCondition) {
	status.TargetStatuses[igName].Conditions = conditions
}

func (vsp *VerticalScalingPolicy) Validate() error {
	s := vsp.Spec

	if s.Behavior == nil {
		return errors.Errorf("validation failed, behavior not provided in spec")
	}

	scaleUpSpec := vsp.getScaleUpSpec()
	if scaleUpSpec != nil {
		if err := scaleUpSpec.Validate([]UtilizationType{CPUUtilizationPercent, MemoryUtilizationPercent, NodesCountUtilizationPercent}); err != nil {
			return err
		}
	}

	scaleDownSpec := vsp.getScaleDownSpec()
	if scaleDownSpec != nil {
		if err := scaleDownSpec.Validate([]UtilizationType{CPUUtilizationPercent, MemoryUtilizationPercent}); err != nil {
			return err
		}
	}
	return nil
}

func (s *ScalingSpec) Validate(validPolicyTypes []UtilizationType) error {
	if s.Policies == nil {
		return errors.Errorf("validation failed, no policies defined for scaling behavior in spec")
	}

	for _, policy := range s.Policies {
		if !isValidUtilizationType(validPolicyTypes, policy.Type) {
			return errors.Errorf("validation failed, invalid policy type %s in scaling behaviors, valid types are: %s", policy.Type, validPolicyTypes)
		}
		if strings.Contains(string(policy.Type), "Percent") {
			if policy.Value < 0 || policy.Value > 100 {
				return errors.Errorf("validation failed, invalid policy percentage value %d for policy type %s in scaling behaviors, value must be from 0-100", policy.Value, policy.Type)
			}
		}

	}
}

func isValidUtilizationType(validTypes []UtilizationType, typeName UtilizationType) bool {
	for _, validType := range validTypes {
		if typeName == validType {
			return true
		}
	}
	return false
}

func (s *VerticalScalingPolicy) getScaleUpSpec() *ScalingSpec {
	return s.Spec.Behavior.ScaleUp
}

func (s *VerticalScalingPolicy) getScaleDownSpec() *ScalingSpec {
	return s.Spec.Behavior.ScaleDown
}
