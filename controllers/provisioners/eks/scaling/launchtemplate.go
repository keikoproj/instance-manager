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
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/keikoproj/instance-manager/api/v1alpha1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/pkg/errors"
)

type LaunchTemplate struct {
	awsprovider.AwsWorker
	OwnerName      string
	TargetResource *ec2.LaunchTemplate
	TargetVersions []*ec2.LaunchTemplateVersion
	LatestVersion  *ec2.LaunchTemplateVersion
	ResourceList   []*ec2.LaunchTemplate
}

var (
	DefaultTemplateVersionRetention int = 10
)

func NewLaunchTemplate(ownerName string, w awsprovider.AwsWorker, input *DiscoverConfigurationInput) (*LaunchTemplate, error) {
	lt := &LaunchTemplate{}
	lt.AwsWorker = w
	lt.OwnerName = ownerName
	if err := lt.Discover(input); err != nil {
		return lt, errors.Wrap(err, "discovery failed")
	}
	return lt, nil
}

func (lt *LaunchTemplate) Discover(input *DiscoverConfigurationInput) error {
	launchTemplates, err := lt.DescribeLaunchTemplates()
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling launch templates")
	}
	lt.ResourceList = launchTemplates

	if input.ScalingGroup == nil {
		return nil
	}

	var targetName string
	if input.ScalingGroup.LaunchTemplate != nil {
		targetName = aws.StringValue(input.ScalingGroup.LaunchTemplate.LaunchTemplateName)
	} else if input.ScalingGroup.MixedInstancesPolicy != nil {
		targetName = aws.StringValue(input.ScalingGroup.MixedInstancesPolicy.LaunchTemplate.LaunchTemplateSpecification.LaunchTemplateName)
	}

	for _, config := range launchTemplates {
		name := aws.StringValue(config.LaunchTemplateName)
		if strings.EqualFold(name, targetName) {
			lt.TargetResource = config
			latest := aws.Int64Value(config.LatestVersionNumber)
			versions, err := lt.DescribeLaunchTemplateVersions(name)
			if err != nil {
				errors.Wrap(err, "failed to describe autoscaling launch template versions")
			}
			lt.TargetVersions = versions
			lt.LatestVersion = lt.getVersion(latest)
		}
	}

	return nil
}

func (lt *LaunchTemplate) Create(input *CreateConfigurationInput) error {
	devices := lt.blockDeviceListRequest(input.Volumes)
	templateData := &ec2.RequestLaunchTemplateData{
		IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileSpecificationRequest{
			Arn: aws.String(input.IamInstanceProfileArn),
		},
		ImageId:             aws.String(input.ImageId),
		InstanceType:        aws.String(input.InstanceType),
		KeyName:             aws.String(input.KeyName),
		SecurityGroupIds:    aws.StringSlice(input.SecurityGroups),
		UserData:            aws.String(input.UserData),
		BlockDeviceMappings: devices,
	}

	if lt.TargetResource == nil {
		if err := lt.CreateLaunchTemplate(&ec2.CreateLaunchTemplateInput{
			LaunchTemplateName: aws.String(input.Name),
			LaunchTemplateData: templateData,
		}); err != nil {
			return err
		}
	} else {
		var createdVersion int64
		var err error
		if createdVersion, err = lt.CreateLaunchTemplateVersion(&ec2.CreateLaunchTemplateVersionInput{
			LaunchTemplateName: aws.String(input.Name),
			LaunchTemplateData: templateData,
		}); err != nil {
			return err
		}

		v := strconv.FormatInt(createdVersion, 10)
		if err := lt.UpdateLaunchTemplateDefaultVersion(input.Name, v); err != nil {
			return err
		}
	}

	return nil
}

func (lt *LaunchTemplate) Delete(input *DeleteConfigurationInput) error {
	if input.RetainVersions == 0 {
		input.RetainVersions = DefaultConfigVersionRetention
	}

	if input.DeleteAll {
		templateName := lt.Name()
		if err := lt.DeleteLaunchTemplate(templateName); err != nil {
			return err
		}
		return nil
	}

	sortedVersions := sortVersions(lt.TargetVersions)

	var deletable []*ec2.LaunchTemplateVersion
	if len(sortedVersions) > input.RetainVersions {
		d := len(sortedVersions) - input.RetainVersions
		deletable = sortedVersions[:d]
	}

	deletableVersions := make([]string, 0)
	for _, d := range deletable {
		versionNumber := aws.Int64Value(d.VersionNumber)
		versionString := strconv.FormatInt(versionNumber, 10)
		deletableVersions = append(deletableVersions, versionString)
	}

	if len(deletableVersions) == 0 {
		return nil
	}

	log.Info("deleting launch template versions", "instancegroup", lt.OwnerName, "versions", deletableVersions)

	if err := lt.DeleteLaunchTemplateVersions(input.Name, deletableVersions); err != nil {
		return errors.Wrap(err, "failed to delete launch configuration")
	}

	return nil
}

func (lt *LaunchTemplate) Drifted(input *CreateConfigurationInput) bool {
	var (
		latestVersion = lt.LatestVersion
		drift         bool
	)

	if latestVersion == nil {
		log.Info("detected drift", "reason", "launchtemplate does not exist", "instancegroup", lt.OwnerName)
		return true
	}

	if aws.StringValue(latestVersion.LaunchTemplateData.ImageId) != input.ImageId {
		log.Info("detected drift", "reason", "image-id has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValue(latestVersion.LaunchTemplateData.ImageId),
			"newValue", input.ImageId,
		)
		drift = true
	}

	if aws.StringValue(latestVersion.LaunchTemplateData.InstanceType) != input.InstanceType {
		log.Info("detected drift", "reason", "instance-type has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValue(latestVersion.LaunchTemplateData.InstanceType),
			"newValue", input.InstanceType,
		)
		drift = true
	}

	if aws.StringValue(latestVersion.LaunchTemplateData.IamInstanceProfile.Arn) != input.IamInstanceProfileArn {
		log.Info("detected drift", "reason", "instance-profile has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValue(latestVersion.LaunchTemplateData.IamInstanceProfile.Arn),
			"newValue", input.IamInstanceProfileArn,
		)
		drift = true
	}

	if !common.StringSliceEquals(aws.StringValueSlice(latestVersion.LaunchTemplateData.SecurityGroupIds), input.SecurityGroups) {
		log.Info("detected drift", "reason", "security-groups has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValueSlice(latestVersion.LaunchTemplateData.SecurityGroups),
			"newValue", input.SecurityGroups,
		)
		drift = true
	}

	if aws.StringValue(latestVersion.LaunchTemplateData.KeyName) != input.KeyName {
		log.Info("detected drift", "reason", "key-pair has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValue(latestVersion.LaunchTemplateData.KeyName),
			"newValue", input.KeyName,
		)
		drift = true
	}

	if aws.StringValue(latestVersion.LaunchTemplateData.UserData) != input.UserData {
		log.Info("detected drift", "reason", "user-data has changed", "instancegroup", lt.OwnerName,
			"previousValue", aws.StringValue(latestVersion.LaunchTemplateData.UserData),
			"newValue", input.UserData,
		)
		drift = true
	}

	devices := lt.blockDeviceList(input.Volumes)
	if !reflect.DeepEqual(latestVersion.LaunchTemplateData.BlockDeviceMappings, devices) {
		log.Info("detected drift", "reason", "volumes have changed", "instancegroup", lt.OwnerName,
			"previousValue", latestVersion.LaunchTemplateData.BlockDeviceMappings,
			"newValue", devices,
		)
		drift = true
	}

	if !drift {
		log.Info("no drift detected", "instancegroup", lt.OwnerName)
	}

	return drift
}

func (lt *LaunchTemplate) Provisioned() bool {
	return lt.TargetResource != nil
}

func (lt *LaunchTemplate) Resource() interface{} {
	return lt.TargetResource
}

func (lt *LaunchTemplate) Name() string {
	if lt.TargetResource == nil {
		return ""
	}
	return aws.StringValue(lt.TargetResource.LaunchTemplateName)
}

func (lt *LaunchTemplate) RotationNeeded(input *DiscoverConfigurationInput) bool {
	if len(input.ScalingGroup.Instances) == 0 {
		return false
	}

	awsLatest := aws.Int64Value(lt.LatestVersion.VersionNumber)
	latestVersion := strconv.FormatInt(awsLatest, 10)
	configName := lt.Name()
	for _, instance := range input.ScalingGroup.Instances {
		if aws.StringValue(instance.LaunchTemplate.LaunchTemplateName) != configName {
			return true
		}
		currentVersion := aws.StringValue(instance.LaunchTemplate.Version)
		if currentVersion != latestVersion {
			return true
		}
	}
	return false
}

func (lt *LaunchTemplate) blockDeviceListRequest(volumes []v1alpha1.NodeVolume) []*ec2.LaunchTemplateBlockDeviceMappingRequest {
	var devices []*ec2.LaunchTemplateBlockDeviceMappingRequest
	for _, v := range volumes {
		devices = append(devices, lt.GetLaunchTemplateBlockDeviceRequest(v.Name, v.Type, v.SnapshotID, v.Size, v.Iops, v.DeleteOnTermination, v.Encrypted))
	}

	return devices
}

func (lt *LaunchTemplate) blockDeviceList(volumes []v1alpha1.NodeVolume) []*ec2.LaunchTemplateBlockDeviceMapping {
	var devices []*ec2.LaunchTemplateBlockDeviceMapping
	for _, v := range volumes {
		devices = append(devices, lt.GetLaunchTemplateBlockDevice(v.Name, v.Type, v.SnapshotID, v.Size, v.Iops, v.DeleteOnTermination, v.Encrypted))
	}

	return devices
}

func (lt *LaunchTemplate) getVersion(id int64) *ec2.LaunchTemplateVersion {
	for _, v := range lt.TargetVersions {
		n := aws.Int64Value(v.VersionNumber)
		if n == id {
			return v
		}
	}
	return nil
}

func sortVersions(versions []*ec2.LaunchTemplateVersion) []*ec2.LaunchTemplateVersion {
	// sort matching launch configs by created time
	sort.Slice(versions, func(i, j int) bool {
		ti := versions[i].CreateTime
		tj := versions[j].CreateTime
		if tj == nil {
			return true
		}
		if ti == nil {
			return false
		}
		return ti.UnixNano() < tj.UnixNano()
	})

	return versions
}
