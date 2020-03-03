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

type EKSFargateSpec struct {
	ProfileName         *string                `json:"fargateProfileName"`
	ClusterName         *string                `json:"clusterName"`
	PodExecutionRoleArn *string                `json:"podExecutionRoleArn,omitempty"`
	Subnets             []*string              `json:"subnets"`
	Selectors           []*EKSFargateSelectors `json:"selectors,omitempty"`
	Tags                []map[string]string    `json:"tags,omitempty"`
}

type EKSFargateSelectors struct {
	Namespace *string           `json:"namespace"`
	Labels    map[string]string `json:"labels"`
}

func (spec *EKSFargateSpec) GetProfileName() *string {
	return spec.ProfileName
}

func (spec *EKSFargateSpec) SetProfileName(name *string) {
	spec.ProfileName = name
}

func (spec *EKSFargateSpec) GetClusterName() *string {
	return spec.ClusterName
}

func (spec *EKSFargateSpec) SetClusterName(name *string) {
	spec.ClusterName = name
}

func (spec *EKSFargateSpec) GetPodExecutionRoleArn() *string {
	return spec.PodExecutionRoleArn
}

func (spec *EKSFargateSpec) SetPodExecutionRoleArn(arn *string) {
	spec.PodExecutionRoleArn = arn
}

func (spec *EKSFargateSpec) GetSubnets() []*string {
	return spec.Subnets
}

func (spec *EKSFargateSpec) SetSubnets(subnets []*string) {
	spec.Subnets = subnets
}

func (spec *EKSFargateSpec) GetSelectors() []*EKSFargateSelectors {
	return spec.Selectors
}

func (spec *EKSFargateSpec) SetSelectors(selectors []*EKSFargateSelectors) {
	spec.Selectors = selectors
}

func (spec *EKSFargateSpec) GetTags() []map[string]string {
	return spec.Tags
}

func (spec *EKSFargateSpec) SetTags(tags []map[string]string) {
	spec.Tags = tags
}
