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

package eks

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (ctx *EksInstanceGroupContext) GetLaunchConfigurationInput() *autoscaling.CreateLaunchConfigurationInput {
	var (
		instanceGroup   = ctx.GetInstanceGroup()
		configuration   = instanceGroup.GetEKSConfiguration()
		clusterName     = configuration.GetClusterName()
		state           = ctx.GetDiscoveredState()
		instanceProfile = state.GetInstanceProfile()
		devices         = ctx.GetBlockDeviceList()
		args            = ctx.GetBootstrapArgs()
		userData        = ctx.AwsWorker.GetBasicUserData(clusterName, args)
	)

	name := fmt.Sprintf("%v-%v-%v-%v", clusterName, instanceGroup.GetNamespace(), instanceGroup.GetName(), common.GetTimeString())
	input := &autoscaling.CreateLaunchConfigurationInput{
		LaunchConfigurationName: aws.String(name),
		IamInstanceProfile:      instanceProfile.Arn,
		ImageId:                 aws.String(configuration.Image),
		InstanceType:            aws.String(configuration.InstanceType),
		KeyName:                 aws.String(configuration.KeyPairName),
		SecurityGroups:          aws.StringSlice(configuration.NodeSecurityGroups),
		BlockDeviceMappings:     devices,
		UserData:                aws.String(userData),
	}

	if configuration.SpotPrice != "" {
		input.SpotPrice = aws.String(configuration.SpotPrice)
	}

	return input
}

func (ctx *EksInstanceGroupContext) GetAddedTags() []*autoscaling.Tag {
	var (
		tags          []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		state         = ctx.GetDiscoveredState()
		scalingGroup  = state.GetScalingGroup()
		asgName       = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	tags = append(tags, ctx.AwsWorker.NewTag(TagName, asgName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagKubernetesCluster, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagClusterName, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupNamespace, instanceGroup.GetNamespace(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(TagInstanceGroupName, instanceGroup.GetName(), asgName))

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		tags = append(tags, ctx.AwsWorker.NewTag(tagSlice["key"], tagSlice["value"], asgName))
	}
	return tags
}

func (ctx *EksInstanceGroupContext) GetRemovedTags() []*autoscaling.Tag {
	var (
		existingTags []*autoscaling.Tag
		removal      []*autoscaling.Tag
		addedTags    = ctx.GetAddedTags()
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
		asgName      = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// get existing tags
	for _, tag := range scalingGroup.Tags {
		existingTags = append(existingTags, ctx.AwsWorker.NewTag(aws.StringValue(tag.Key), aws.StringValue(tag.Value), asgName))
	}

	// find removals against incoming tags
	for _, tag := range existingTags {
		var match bool
		for _, t := range addedTags {
			if reflect.DeepEqual(t, tag) {
				match = true
			}
		}
		if !match {
			removal = append(removal, tag)
		}
	}
	return removal
}

func (ctx *EksInstanceGroupContext) GetBlockDeviceList() []*autoscaling.BlockDeviceMapping {
	var (
		devices       []*autoscaling.BlockDeviceMapping
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
	)

	customVolumes := configuration.GetVolumes()
	if customVolumes != nil {
		for _, v := range customVolumes {
			devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice(v.Name, v.Type, v.Size))
		}
	} else {
		devices = append(devices, ctx.AwsWorker.GetBasicBlockDevice("/dev/xvda", "gp2", configuration.GetVolumeSize()))
	}
	return devices
}

func (ctx *EksInstanceGroupContext) GetTaintList() []string {
	var (
		taintList     []string
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		taints        = configuration.GetTaints()
	)

	if len(taints) > 0 {
		for _, t := range taints {
			taintList = append(taintList, fmt.Sprintf("%v=%v:%v", t.Key, t.Value, t.Effect))
		}
	}
	sort.Strings(taintList)
	return taintList
}

func (ctx *EksInstanceGroupContext) GetLabelList() []string {
	var (
		labelList     []string
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		customLabels  = configuration.GetLabels()
	)
	// get custom labels
	if len(customLabels) > 0 {
		for k, v := range customLabels {
			labelList = append(labelList, fmt.Sprintf("%v=%v", k, v))
		}
	}
	sort.Strings(labelList)

	// add role label
	for _, label := range RoleLabelsFmt {
		labelList = append(labelList, fmt.Sprintf(label, instanceGroup.GetName()))
	}
	return labelList
}

func (ctx *EksInstanceGroupContext) GetBootstrapArgs() string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		bootstrapArgs = configuration.GetBootstrapArguments()
	)
	labelsFlag := fmt.Sprintf("--node-labels=%v", strings.Join(ctx.GetLabelList(), ","))
	taintsFlag := fmt.Sprintf("--register-with-taints=%v", strings.Join(ctx.GetTaintList(), ","))
	return fmt.Sprintf("--kubelet-extra-args '%v %v %v'", labelsFlag, taintsFlag, bootstrapArgs)
}

func (ctx *EksInstanceGroupContext) RotationNeeded() bool {
	var (
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
	)

	for _, instance := range scalingGroup.Instances {
		if aws.StringValue(instance.LaunchConfigurationName) != state.GetActiveLaunchConfigurationName() {
			log.Info("upgrade required: scaling instances with different launch-config")
			return true
		}
	}
	return false
}

func (ctx *EksInstanceGroupContext) LaunchConfigurationDrifted() bool {
	var (
		state          = ctx.GetDiscoveredState()
		newConfig      = ctx.GetLaunchConfigurationInput()
		existingConfig = state.GetLaunchConfiguration()
		drift          bool
	)

	if state.LaunchConfiguration == nil {
		log.Info("detected drift in launch configuration: launch config does not exist")
		return true
	}

	if aws.StringValue(existingConfig.ImageId) != aws.StringValue(newConfig.ImageId) {
		log.Infof(
			"detected drift in launch configuration: image-id has changed, %s -> %s",
			aws.StringValue(existingConfig.ImageId),
			aws.StringValue(newConfig.ImageId),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.InstanceType) != aws.StringValue(newConfig.InstanceType) {
		log.Infof(
			"detected drift in launch configuration: instance-type has changed, %s -> %s",
			aws.StringValue(existingConfig.InstanceType),
			aws.StringValue(newConfig.InstanceType),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.IamInstanceProfile) != aws.StringValue(newConfig.IamInstanceProfile) {
		log.Infof(
			"detected drift in launch configuration: instance-profile has changed, %s -> %s",
			aws.StringValue(existingConfig.IamInstanceProfile),
			aws.StringValue(newConfig.IamInstanceProfile),
		)
		drift = true
	}

	if !common.StringSliceEquals(aws.StringValueSlice(existingConfig.SecurityGroups), aws.StringValueSlice(newConfig.SecurityGroups)) {
		log.Infof(
			"detected drift in launch configuration: security-groups have changed, %v -> %v",
			aws.StringValueSlice(existingConfig.SecurityGroups),
			aws.StringValueSlice(newConfig.SecurityGroups),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.SpotPrice) != aws.StringValue(newConfig.SpotPrice) {
		log.Infof(
			"detected drift in launch configuration: spot-price has changed, '%s' -> '%s'",
			aws.StringValue(existingConfig.SpotPrice),
			aws.StringValue(newConfig.SpotPrice),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.KeyName) != aws.StringValue(newConfig.KeyName) {
		log.Infof(
			"detected drift in launch configuration: key-pair-name has changed, %s -> %s",
			aws.StringValue(existingConfig.KeyName),
			aws.StringValue(newConfig.KeyName),
		)
		drift = true
	}

	if aws.StringValue(existingConfig.UserData) != aws.StringValue(newConfig.UserData) {
		log.Infof(
			"detected drift in launch configuration: user-data has changed, %s -> %s",
			aws.StringValue(existingConfig.UserData),
			aws.StringValue(newConfig.UserData),
		)
		drift = true
	}

	if !reflect.DeepEqual(existingConfig.BlockDeviceMappings, newConfig.BlockDeviceMappings) {
		log.Infof(
			"detected drift in launch configuration: block-device-mappings has changed;\n < %v\n---\n> %v",
			existingConfig.BlockDeviceMappings,
			newConfig.BlockDeviceMappings,
		)
		drift = true
	}

	log.Info("no drift detected")
	return drift
}

func (ctx *EksInstanceGroupContext) discoverSpotPrice() error {
	var (
		instanceGroup    = ctx.GetInstanceGroup()
		state            = ctx.GetDiscoveredState()
		status           = instanceGroup.GetStatus()
		configuration    = instanceGroup.GetEKSConfiguration()
		scalingGroup     = state.GetScalingGroup()
		scalingGroupName = aws.StringValue(scalingGroup.AutoScalingGroupName)
	)

	// get latest spot recommendations from events
	recommendation, err := kubeprovider.GetSpotRecommendation(ctx.KubernetesClient.Kubernetes, scalingGroupName)
	if err != nil {
		configuration.SetSpotPrice("")
		return err
	}

	// in the case there are no recommendations, which should turn of spot unless it's manually set
	if reflect.DeepEqual(recommendation, kubeprovider.SpotRecommendation{}) {
		// if it was not using a recommendation before and spec has a spot price it means it was manually configured
		if !status.GetUsingSpotRecommendation() && configuration.GetSpotPrice() != "" {
			log.Warnf("using manually configured spot price %v", configuration.GetSpotPrice())
		} else {
			// if recommendation was used, set flag to false
			status.SetUsingSpotRecommendation(false)
		}
		return nil
	}

	// set the recommendation given
	status.SetUsingSpotRecommendation(true)

	if recommendation.UseSpot {
		log.Infof("spot enabled with current bid price: %v", recommendation.SpotPrice)
		configuration.SetSpotPrice(recommendation.SpotPrice)
	} else {
		log.Infoln("spot disabled by recommendation")
		configuration.SetSpotPrice("")
	}
	return nil
}

func (ctx *EksInstanceGroupContext) findOwnedScalingGroups(groups []*autoscaling.Group) []*autoscaling.Group {
	var (
		filteredGroups = make([]*autoscaling.Group, 0)
		instanceGroup  = ctx.GetInstanceGroup()
		configuration  = instanceGroup.GetEKSConfiguration()
		clusterName    = configuration.GetClusterName()
	)

	for _, group := range groups {
		for _, tag := range group.Tags {
			var (
				key   = aws.StringValue(tag.Key)
				value = aws.StringValue(tag.Value)
			)
			// if group has the same cluster tag it's owned by the controller
			if key == TagClusterName && strings.ToLower(value) == strings.ToLower(clusterName) {
				filteredGroups = append(filteredGroups, group)
			}
		}
	}
	return filteredGroups
}

func (ctx *EksInstanceGroupContext) findTargetScalingGroup(groups []*autoscaling.Group) *autoscaling.Group {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		nameMatch      bool
		namespaceMatch bool
	)

	for _, group := range groups {
		for _, tag := range group.Tags {
			var (
				key   = aws.StringValue(tag.Key)
				value = aws.StringValue(tag.Value)
			)
			// must match both name and namespace tag
			if key == TagInstanceGroupName && value == instanceGroup.GetName() {
				nameMatch = true
			}
			if key == TagInstanceGroupNamespace && value == instanceGroup.GetNamespace() {
				namespaceMatch = true
			}
		}
		if nameMatch && namespaceMatch {
			return group
		}
	}

	return nil
}

func (ctx *EksInstanceGroupContext) NewRollingUpdateRequest() *kubeprovider.RollingUpdateRequest {
	var (
		needsUpdate        []string
		allInstances       []string
		instanceGroup      = ctx.GetInstanceGroup()
		scalingGroup       = ctx.GetDiscoveredState().GetScalingGroup()
		activeLaunchConfig = aws.StringValue(scalingGroup.LaunchConfigurationName)
		desiredCount       = int(aws.Int64Value(scalingGroup.DesiredCapacity))
		strategy           = instanceGroup.GetUpgradeStrategy().GetRollingUpdateType()
		maxUnavailable     = strategy.GetMaxUnavailable()
	)

	// Get all Autoscaling Instances that needs update
	for _, instance := range scalingGroup.Instances {
		allInstances = append(allInstances, aws.StringValue(instance.InstanceId))
		if aws.StringValue(instance.LaunchConfigurationName) != activeLaunchConfig {
			needsUpdate = append(needsUpdate, aws.StringValue(instance.InstanceId))
		}
	}
	allCount := len(allInstances)

	var unavailableInt int
	if maxUnavailable.Type == intstr.String {
		unavailableInt, _ = intstr.GetValueFromIntOrPercent(maxUnavailable, allCount, true)
		if unavailableInt == 0 {
			unavailableInt = 1
		}
	} else {
		unavailableInt = maxUnavailable.IntValue()
	}

	return &kubeprovider.RollingUpdateRequest{
		AwsWorker:       ctx.AwsWorker,
		Kubernetes:      ctx.KubernetesClient.Kubernetes,
		MaxUnavailable:  unavailableInt,
		DesiredCapacity: desiredCount,
		AllInstances:    allInstances,
		UpdateTargets:   needsUpdate,
	}
}

func (ctx *EksInstanceGroupContext) UpdateNodeReadyCondition() (bool, error) {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		status        = instanceGroup.GetStatus()
		scalingGroup  = state.GetScalingGroup()
	)
	log.Info("waiting for node readiness conditions")

	instanceIds := make([]string, 0)
	desiredCount := int(aws.Int64Value(scalingGroup.DesiredCapacity))
	for _, instance := range scalingGroup.Instances {
		instanceIds = append(instanceIds, aws.StringValue(instance.InstanceId))
	}

	var conditions []v1alpha1.InstanceGroupCondition
	ok, err := kubeprovider.IsDesiredNodesReady(ctx.KubernetesClient.Kubernetes, instanceIds, desiredCount)
	if err != nil {
		return false, err
	}

	if ok {
		conditions = append(conditions, v1alpha1.NewInstanceGroupCondition(v1alpha1.NodesReady, corev1.ConditionTrue))
		status.SetConditions(conditions)
		return true, nil
	}
	conditions = append(conditions, v1alpha1.NewInstanceGroupCondition(v1alpha1.NodesReady, corev1.ConditionFalse))
	status.SetConditions(conditions)
	return false, nil
}

func LoadControllerConfiguration(instanceGroup *v1alpha1.InstanceGroup, controllerConfig []byte) (EksDefaultConfiguration, error) {
	var (
		defaultConfig EksDefaultConfiguration
		configuration = instanceGroup.GetEKSConfiguration()
	)

	err := yaml.Unmarshal(controllerConfig, &defaultConfig)
	if err != nil {
		return defaultConfig, err
	}

	if len(defaultConfig.DefaultSubnets) != 0 {
		configuration.SetSubnets(defaultConfig.DefaultSubnets)
	}

	if defaultConfig.EksClusterName != "" {
		configuration.SetClusterName(defaultConfig.EksClusterName)
	}

	return defaultConfig, nil
}
