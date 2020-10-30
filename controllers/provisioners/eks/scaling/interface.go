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

package scaling

import (
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	log = ctrl.Log.WithName("scaling")
)

type Configuration interface {
	Name() string
	Resource() interface{}
	Create(input *CreateConfigurationInput) error
	Delete(input *DeleteConfigurationInput) error
	Discover(input *DiscoverConfigurationInput) error
	Drifted(input *CreateConfigurationInput) bool
	RotationNeeded(input *DiscoverConfigurationInput) bool
	Provisioned() bool
}

type DeleteConfigurationInput struct {
	Name           string
	Prefix         string
	DeleteAll      bool
	RetainVersions int
}

type DiscoverConfigurationInput struct {
	ScalingGroup *autoscaling.Group
}

type CreateConfigurationInput struct {
	Name                  string
	IamInstanceProfileArn string
	ImageId               string
	InstanceType          string
	KeyName               string
	SecurityGroups        []string
	Volumes               []v1alpha1.NodeVolume
	UserData              string
	SpotPrice             string
	LicenseSpecifications []string
	Placement             *LaunchTemplatePlacementInput
}

type LaunchTemplatePlacementInput struct {
	Affinity             string
	AvailabilityZone     string
	GroupName            string
	HostID               string
	HostResourceGroupArn string
	PartitionNumber      int64
	SpreadDomain         string
	Tenancy              string
}

func ConvertToLaunchTemplate(resource interface{}) *ec2.LaunchTemplate {
	if lt, ok := resource.(*ec2.LaunchTemplate); ok && lt != nil {
		return lt
	}
	return &ec2.LaunchTemplate{}
}

func ConvertToLaunchConfiguration(resource interface{}) *autoscaling.LaunchConfiguration {
	if lc, ok := resource.(*autoscaling.LaunchConfiguration); ok && lc != nil {
		return lc
	}
	return &autoscaling.LaunchConfiguration{}
}
