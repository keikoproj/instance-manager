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

package aws

import (
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/keikoproj/instance-manager/controllers/common"
)

func (w *AwsWorker) GetAutoScalingBasicBlockDevice(name, volType, snapshot string, volSize, iops int64, delete, encrypt *bool) *autoscaling.BlockDeviceMapping {
	device := &autoscaling.BlockDeviceMapping{
		DeviceName: aws.String(name),
		Ebs: &autoscaling.Ebs{
			VolumeType: aws.String(volType),
		},
	}
	if delete != nil {
		device.Ebs.DeleteOnTermination = delete
	} else {
		device.Ebs.DeleteOnTermination = aws.Bool(true)
	}
	if encrypt != nil {
		device.Ebs.Encrypted = encrypt
	}
	if iops != 0 && strings.EqualFold(volType, "io1") {
		device.Ebs.Iops = aws.Int64(iops)
	}
	if volSize != 0 {
		device.Ebs.VolumeSize = aws.Int64(volSize)
	}
	if !common.StringEmpty(snapshot) {
		device.Ebs.SnapshotId = aws.String(snapshot)
	}

	return device
}

func (w *AwsWorker) GetLaunchTemplateBlockDeviceRequest(name, volType, snapshot string, volSize, iops int64, delete, encrypt *bool) *ec2.LaunchTemplateBlockDeviceMappingRequest {
	device := &ec2.LaunchTemplateBlockDeviceMappingRequest{
		DeviceName: aws.String(name),
		Ebs: &ec2.LaunchTemplateEbsBlockDeviceRequest{
			VolumeType: aws.String(volType),
		},
	}
	if delete != nil {
		device.Ebs.DeleteOnTermination = delete
	} else {
		device.Ebs.DeleteOnTermination = aws.Bool(true)
	}
	if encrypt != nil {
		device.Ebs.Encrypted = encrypt
	}
	if iops != 0 && strings.EqualFold(volType, "io1") {
		device.Ebs.Iops = aws.Int64(iops)
	}
	if volSize != 0 {
		device.Ebs.VolumeSize = aws.Int64(volSize)
	}
	if !common.StringEmpty(snapshot) {
		device.Ebs.SnapshotId = aws.String(snapshot)
	}

	return device
}

func (w *AwsWorker) GetLaunchTemplateBlockDevice(name, volType, snapshot string, volSize, iops int64, delete, encrypt *bool) *ec2.LaunchTemplateBlockDeviceMapping {
	device := &ec2.LaunchTemplateBlockDeviceMapping{
		DeviceName: aws.String(name),
		Ebs: &ec2.LaunchTemplateEbsBlockDevice{
			VolumeType: aws.String(volType),
		},
	}
	if delete != nil {
		device.Ebs.DeleteOnTermination = delete
	} else {
		device.Ebs.DeleteOnTermination = aws.Bool(true)
	}
	if encrypt != nil {
		device.Ebs.Encrypted = encrypt
	}
	if iops != 0 && strings.EqualFold(volType, "io1") {
		device.Ebs.Iops = aws.Int64(iops)
	}
	if volSize != 0 {
		device.Ebs.VolumeSize = aws.Int64(volSize)
	}
	if !common.StringEmpty(snapshot) {
		device.Ebs.SnapshotId = aws.String(snapshot)
	}

	return device
}

func (w *AwsWorker) LaunchTemplatePlacementRequest(availabilityZone, hostResourceGroupArn, tenancy string) *ec2.LaunchTemplatePlacementRequest {
	placement := &ec2.LaunchTemplatePlacementRequest{}

	if !common.StringEmpty(availabilityZone) {
		placement.AvailabilityZone = aws.String(availabilityZone)
	}

	if !common.StringEmpty(hostResourceGroupArn) {
		placement.HostResourceGroupArn = aws.String(hostResourceGroupArn)
	}

	if !common.StringEmpty(tenancy) {
		placement.Tenancy = aws.String(tenancy)
	}

	return placement
}

func (w *AwsWorker) LaunchTemplatePlacement(availabilityZone, hostResourceGroupArn, tenancy string) *ec2.LaunchTemplatePlacement {
	placement := &ec2.LaunchTemplatePlacement{}

	if !common.StringEmpty(availabilityZone) {
		placement.AvailabilityZone = aws.String(availabilityZone)
	}

	if !common.StringEmpty(hostResourceGroupArn) {
		placement.HostResourceGroupArn = aws.String(hostResourceGroupArn)
	}

	if !common.StringEmpty(tenancy) {
		placement.Tenancy = aws.String(tenancy)
	}

	return placement
}

func (w *AwsWorker) LaunchTemplateLicenseConfigurationRequest(input []string) []*ec2.LaunchTemplateLicenseConfigurationRequest {
	var licenses []*ec2.LaunchTemplateLicenseConfigurationRequest
	for _, v := range input {
		licenses = append(licenses, &ec2.LaunchTemplateLicenseConfigurationRequest{
			LicenseConfigurationArn: aws.String(v),
		})
	}
	return licenses
}

func (w *AwsWorker) LaunchTemplateLicenseConfiguration(input []string) []*ec2.LaunchTemplateLicenseConfiguration {
	var licenses []*ec2.LaunchTemplateLicenseConfiguration
	for _, v := range input {
		licenses = append(licenses, &ec2.LaunchTemplateLicenseConfiguration{
			LicenseConfigurationArn: aws.String(v),
		})
	}
	return licenses
}

func (w *AwsWorker) NewTag(key, val, resource string) *autoscaling.Tag {
	return &autoscaling.Tag{
		Key:               aws.String(key),
		Value:             aws.String(val),
		PropagateAtLaunch: aws.Bool(true),
		ResourceId:        aws.String(resource),
		ResourceType:      aws.String("auto-scaling-group"),
	}
}

func (w *AwsWorker) GetLabelsUpdatePayload(existing, new map[string]string) (*eks.UpdateLabelsPayload, bool) {

	var (
		removeLabels    = make([]string, 0)
		addUpdateLabels = make(map[string]string)
	)

	payload := &eks.UpdateLabelsPayload{}
	for k, v := range new {
		// handle new labels
		if _, ok := existing[k]; !ok {
			addUpdateLabels[k] = v
		}

		// handle label value updates
		if val, ok := existing[k]; ok && val != v {
			addUpdateLabels[k] = v
		}
	}

	for k := range existing {
		// handle removals
		if _, ok := new[k]; !ok {
			removeLabels = append(removeLabels, k)
		}
	}

	if len(addUpdateLabels) > 0 {
		payload.AddOrUpdateLabels = aws.StringMap(addUpdateLabels)
	}

	if len(removeLabels) > 0 {
		payload.RemoveLabels = aws.StringSlice(removeLabels)
	}

	if payload.RemoveLabels == nil && payload.AddOrUpdateLabels == nil {
		return payload, false
	}

	return payload, true
}
