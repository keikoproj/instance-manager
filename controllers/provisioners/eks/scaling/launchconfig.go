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
	"strings"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/pkg/errors"
)

type LaunchConfiguration struct {
	awsprovider.AwsWorker
	OwnerName      string
	TargetResource *autoscaling.LaunchConfiguration
	ResourceList   []*autoscaling.LaunchConfiguration
}

var (
	DefaultConfigVersionRetention int = 2
)

func NewLaunchConfiguration(ownerName string, w awsprovider.AwsWorker, input *DiscoverConfigurationInput) (*LaunchConfiguration, error) {
	lc := &LaunchConfiguration{}
	lc.AwsWorker = w
	lc.OwnerName = ownerName
	if err := lc.Discover(input); err != nil {
		return lc, errors.Wrap(err, "discovery failed")
	}
	return lc, nil
}

func (lc *LaunchConfiguration) Discover(input *DiscoverConfigurationInput) error {
	launchConfigurations, err := lc.DescribeAutoscalingLaunchConfigs()
	if err != nil {
		return errors.Wrap(err, "failed to describe autoscaling launch configurations")
	}
	lc.ResourceList = launchConfigurations

	if input.ScalingGroup == nil {
		return nil
	}
	targetName := aws.StringValue(input.ScalingGroup.LaunchConfigurationName)

	for _, config := range launchConfigurations {
		name := aws.StringValue(config.LaunchConfigurationName)
		if strings.EqualFold(name, targetName) {
			lc.TargetResource = config
		}
	}

	return nil
}

func (lc *LaunchConfiguration) Create(input *CreateConfigurationInput) error {
	devices := lc.blockDeviceList(input.Volumes)
	opts := &autoscaling.CreateLaunchConfigurationInput{
		LaunchConfigurationName: aws.String(input.Name),
		IamInstanceProfile:      aws.String(input.IamInstanceProfileArn),
		ImageId:                 aws.String(input.ImageId),
		InstanceType:            aws.String(input.InstanceType),
		KeyName:                 aws.String(input.KeyName),
		SecurityGroups:          aws.StringSlice(input.SecurityGroups),
		UserData:                aws.String(input.UserData),
		BlockDeviceMappings:     devices,
	}

	if !common.StringEmpty(input.SpotPrice) {
		opts.SpotPrice = aws.String(input.SpotPrice)
	}

	if err := lc.CreateLaunchConfig(opts); err != nil {
		return err
	}

	return nil
}

func (lc *LaunchConfiguration) Delete(input *DeleteConfigurationInput) error {
	if input.RetainVersions == 0 {
		input.RetainVersions = DefaultConfigVersionRetention
	}

	prefixedConfigs := getPrefixedConfigurations(lc.ResourceList, input.Prefix)
	sortedConfigs := sortConfigurations(prefixedConfigs)

	var deletable []*autoscaling.LaunchConfiguration
	if len(sortedConfigs) > input.RetainVersions {
		d := len(sortedConfigs) - input.RetainVersions
		deletable = sortedConfigs[:d]
	}

	if input.DeleteAll {
		deletable = prefixedConfigs
	}

	for _, d := range deletable {
		name := aws.StringValue(d.LaunchConfigurationName)
		if !input.DeleteAll && strings.EqualFold(name, input.Name) {
			continue
		}

		log.Info("deleting launch configuration", "instancegroup", lc.OwnerName, "name", name)

		if err := lc.DeleteLaunchConfig(name); err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if common.ContainsEqualFoldSubstring(awsErr.Message(), awsprovider.LaunchConfigurationNotFoundErrorMessage) {
					log.Info("launch configuration not found", "instancegroup", lc.OwnerName, "name", name)
					continue
				}
			}
			return errors.Wrap(err, "failed to delete launch configuration")
		}
	}

	return nil
}

func (lc *LaunchConfiguration) Drifted(input *CreateConfigurationInput) bool {
	var (
		existingConfig = lc.TargetResource
		drift          bool
	)

	if existingConfig == nil {
		log.Info("detected drift", "reason", "launchconfig does not exist", "instancegroup", lc.OwnerName)
		return true
	}

	if aws.StringValue(existingConfig.ImageId) != input.ImageId {
		log.Info("detected drift", "reason", "image-id has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.ImageId),
			"newValue", input.ImageId,
		)
		drift = true
	}

	if aws.StringValue(existingConfig.InstanceType) != input.InstanceType {
		log.Info("detected drift", "reason", "instance-type has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.InstanceType),
			"newValue", input.InstanceType,
		)
		drift = true
	}

	if aws.StringValue(existingConfig.IamInstanceProfile) != input.IamInstanceProfileArn {
		log.Info("detected drift", "reason", "instance-profile has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.IamInstanceProfile),
			"newValue", input.IamInstanceProfileArn,
		)
		drift = true
	}

	if !common.StringSliceEquals(aws.StringValueSlice(existingConfig.SecurityGroups), input.SecurityGroups) {
		log.Info("detected drift", "reason", "security-groups has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValueSlice(existingConfig.SecurityGroups),
			"newValue", input.SecurityGroups,
		)
		drift = true
	}

	if aws.StringValue(existingConfig.SpotPrice) != input.SpotPrice {
		log.Info("detected drift", "reason", "spot-price has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.SpotPrice),
			"newValue", input.SpotPrice,
		)
		drift = true
	}

	if aws.StringValue(existingConfig.KeyName) != input.KeyName {
		log.Info("detected drift", "reason", "key-pair has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.KeyName),
			"newValue", input.KeyName,
		)
		drift = true
	}

	if aws.StringValue(existingConfig.UserData) != input.UserData {
		log.Info("detected drift", "reason", "user-data has changed", "instancegroup", lc.OwnerName,
			"previousValue", aws.StringValue(existingConfig.UserData),
			"newValue", input.UserData,
		)
		drift = true
	}

	devices := lc.blockDeviceList(input.Volumes)
	if !reflect.DeepEqual(existingConfig.BlockDeviceMappings, devices) {
		log.Info("detected drift", "reason", "volumes have changed", "instancegroup", lc.OwnerName,
			"previousValue", existingConfig.BlockDeviceMappings,
			"newValue", devices,
		)
		drift = true
	}

	if !drift {
		log.Info("drift not detected", "instancegroup", lc.OwnerName)
	}

	return drift
}

func (lc *LaunchConfiguration) Provisioned() bool {
	return lc.TargetResource != nil
}

func (lc *LaunchConfiguration) Resource() interface{} {
	return lc.TargetResource
}

func (lc *LaunchConfiguration) Name() string {
	if lc.TargetResource == nil {
		return ""
	}
	return aws.StringValue(lc.TargetResource.LaunchConfigurationName)
}

func (lc *LaunchConfiguration) RotationNeeded(input *DiscoverConfigurationInput) bool {
	if len(input.ScalingGroup.Instances) == 0 {
		return false
	}

	configName := lc.Name()
	for _, instance := range input.ScalingGroup.Instances {
		if aws.StringValue(instance.LaunchConfigurationName) != configName {
			return true
		}
	}
	return false
}

func (lc *LaunchConfiguration) blockDeviceList(volumes []v1alpha1.NodeVolume) []*autoscaling.BlockDeviceMapping {
	var devices []*autoscaling.BlockDeviceMapping
	for _, v := range volumes {
		devices = append(devices, lc.GetAutoScalingBasicBlockDevice(v.Name, v.Type, v.SnapshotID, v.Size, v.Iops, v.DeleteOnTermination, v.Encrypted))
	}

	return devices
}

func getPrefixedConfigurations(configs []*autoscaling.LaunchConfiguration, prefix string) []*autoscaling.LaunchConfiguration {
	prefixed := []*autoscaling.LaunchConfiguration{}
	for _, lc := range configs {
		name := aws.StringValue(lc.LaunchConfigurationName)
		if strings.HasPrefix(name, prefix) {
			prefixed = append(prefixed, lc)
		}
	}
	return prefixed
}

func sortConfigurations(configs []*autoscaling.LaunchConfiguration) []*autoscaling.LaunchConfiguration {
	// sort matching launch configs by created time
	sort.Slice(configs, func(i, j int) bool {
		ti := configs[i].CreatedTime
		tj := configs[j].CreatedTime
		if tj == nil {
			return true
		}
		if ti == nil {
			return false
		}
		return ti.UnixNano() < tj.UnixNano()
	})

	return configs
}
