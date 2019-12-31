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

type EKSCFSpec struct {
	MaxSize            int32              `json:"maxSize"`
	MinSize            int32              `json:"minSize"`
	EKSCFConfiguration EKSCFConfiguration `json:"configuration"`
}

// EKSCFConfiguration defines the context of an AWS Instance Group using EKSCF
type EKSCFConfiguration struct {
	EksClusterName              string              `json:"clusterName,omitempty"`
	KeyPairName                 string              `json:"keyPairName"`
	Image                       string              `json:"image"`
	InstanceType                string              `json:"instanceType"`
	NodeSecurityGroups          []string            `json:"securityGroups"`
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
