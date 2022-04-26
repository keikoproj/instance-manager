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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerticalScalingPolicySpec defines the desired state of VerticalScalingPolicy
type VerticalScalingPolicySpec struct {
	InstanceFamily string                       `json:"instanceFamily,omitempty"`
	Resources      *corev1.ResourceRequirements `json:"resources"`
	Target         *corev1.ObjectReference      `json:"scaleTargetRef"`
	Behavior       *BehaviorSpec                `json:"behavior"`
}

type BehaviorSpec struct {
	//TODO
}

// VerticalScalingPolicyStatus defines the observed state of VerticalScalingPolicy
type VerticalScalingPolicyStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VerticalScalingPolicy is the Schema for the verticalscalingpolicies API
type VerticalScalingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticalScalingPolicySpec   `json:"spec,omitempty"`
	Status VerticalScalingPolicyStatus `json:"status,omitempty"`
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
