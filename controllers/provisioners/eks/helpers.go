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
	"bytes"
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/semver"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (ctx *EksInstanceGroupContext) ResolveSubnets() []string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		resolved      = make([]string, 0)
	)

	for _, s := range configuration.GetSubnets() {
		if strings.HasPrefix(s, "subnet-") {
			resolved = append(resolved, s)
			continue
		}

		sn, err := ctx.AwsWorker.SubnetByName(s, state.GetVPCId())
		if err != nil {
			ctx.Log.Error(err, "failed to resolve subnet id by name", "subnet", s)
			continue
		}
		if sn == nil {
			ctx.Log.Error(errors.New("subnet not found"), "failed to resolve subnet by name", "subnet", s)
			continue
		}
		resolved = append(resolved, aws.StringValue(sn.SubnetId))
	}

	return resolved
}

func (ctx *EksInstanceGroupContext) ResolveSecurityGroups() []string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		state         = ctx.GetDiscoveredState()
		resolved      = make([]string, 0)
	)

	for _, g := range configuration.GetSecurityGroups() {
		if strings.HasPrefix(g, "sg-") {
			resolved = append(resolved, g)
			continue
		}

		sg, err := ctx.AwsWorker.SecurityGroupByName(g, state.GetVPCId())
		if err != nil {
			ctx.Log.Error(err, "failed to resolve security group by name", "security-group", g)
			continue
		}
		if sg == nil {
			ctx.Log.Error(errors.New("security group not found"), "failed to resolve security group by name", "security-group", g)
			continue
		}
		resolved = append(resolved, aws.StringValue(sg.GroupId))
	}

	return resolved
}

func (ctx *EksInstanceGroupContext) GetBasicUserData(clusterName, args string, kubeletExtraArgs string, payload UserDataPayload, mounts []MountOpts) string {
	osFamily := ctx.GetOsFamily()
	var UserDataTemplate string
	if strings.EqualFold(osFamily, OsFamilyWindows) {
		UserDataTemplate = `
<powershell>
  [string]$EKSBinDir = "$env:ProgramFiles\Amazon\EKS"
  [string]$EKSBootstrapScriptName = 'Start-EKSBootstrap.ps1'
  [string]$EKSBootstrapScriptFile = "$EKSBinDir\$EKSBootstrapScriptName"
  & $EKSBootstrapScriptFile -EKSClusterName {{ .ClusterName }} -KubeletExtraArgs '{{ .KubeletExtraArgs }}' 3>&1 4>&1 5>&1 6>&1
</powershell>
		`
	} else {
		UserDataTemplate = `#!/bin/bash
{{range $pre := .PreBootstrap}}{{$pre}}{{end}}
{{- range .MountOptions}}
mkfs.{{ .FileSystem | ToLower }} {{ .Device }}
mkdir {{ .Mount }}
mount {{ .Device }} {{ .Mount }}
mount
{{- if .Persistance}}
echo "{{ .Device}}    {{ .Mount }}    {{ .FileSystem | ToLower }}    defaults    0    2" >> /etc/fstab
{{- end}}
{{- end}}
set -o xtrace
/etc/eks/bootstrap.sh {{ .ClusterName }} {{ .Arguments }}
set +o xtrace
{{range $post := .PostBootstrap}}{{$post}}{{end}}`
	}
	data := EKSUserData{
		ClusterName:      clusterName,
		KubeletExtraArgs: kubeletExtraArgs,
		Arguments:        args,
		PreBootstrap:     payload.PreBootstrap,
		PostBootstrap:    payload.PostBootstrap,
		MountOptions:     mounts,
	}
	out := &bytes.Buffer{}
	tmpl := template.New("userData").Funcs(template.FuncMap{
		"ToLower": strings.ToLower,
	})
	var err error
	if tmpl, err = tmpl.Parse(UserDataTemplate); err != nil {
		ctx.Log.Error(err, "failed to parse userData template")
	}
	tmpl.Execute(out, data)
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

func (ctx *EksInstanceGroupContext) GetUserDataStages() UserDataPayload {

	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		userData      = configuration.GetUserData()
	)

	payload := UserDataPayload{}

	for _, stage := range userData {
		switch {
		case strings.EqualFold(stage.Stage, v1alpha1.PreBootstrapStage):
			data, err := common.GetDecodedString(stage.Data)
			if err != nil {
				ctx.Log.Error(err, "failed to decode base64 stage data", "stage", stage.Stage, "data", stage.Data)
			}
			payload.PreBootstrap = append(payload.PreBootstrap, data)
		case strings.EqualFold(stage.Stage, v1alpha1.PostBootstrapStage):
			data, err := common.GetDecodedString(stage.Data)
			if err != nil {
				ctx.Log.Error(err, "failed to decode base64 stage data", "stage", stage.Stage, "data", stage.Data)
			}
			payload.PostBootstrap = append(payload.PostBootstrap, data)
		default:
			ctx.Log.Info("invalid userdata stage will not be rendered", "stage", stage.Stage, "data", stage.Data)
		}
	}
	return payload
}

func (ctx *EksInstanceGroupContext) GetMountOpts() []MountOpts {
	var (
		mountOpts     = make([]MountOpts, 0)
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		volumes       = configuration.GetVolumes()
	)

	for _, vol := range volumes {
		if vol.MountOptions == nil {
			continue
		}
		if !common.ContainsEqualFold(v1alpha1.AllowedFileSystemTypes, vol.MountOptions.FileSystem) {
			ctx.Log.Error(errors.New("file system type unsupported"), "file-system", vol.MountOptions.FileSystem, "allowed-values", v1alpha1.AllowedFileSystemTypes)
			continue
		}
		if common.StringEmpty(vol.MountOptions.Mount) || !strings.HasPrefix(vol.MountOptions.Mount, "/") {
			ctx.Log.Error(errors.New("provided mount path is invalid"), "volume", vol.Name, "mount", vol.MountOptions.Mount)
			continue
		}

		var persistance bool
		if vol.MountOptions.Persistance == nil {
			persistance = true
		}

		mountOpts = append(mountOpts, MountOpts{
			FileSystem:  vol.MountOptions.FileSystem,
			Device:      vol.Name,
			Mount:       vol.MountOptions.Mount,
			Persistance: persistance,
		})
	}
	return mountOpts
}

func (ctx *EksInstanceGroupContext) GetAddedTags(asgName string) []*autoscaling.Tag {
	var (
		tags          []*autoscaling.Tag
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		clusterName   = configuration.GetClusterName()
		annotations   = instanceGroup.GetAnnotations()
		labels        = configuration.GetLabels()
		taints        = configuration.GetTaints()
	)

	tags = append(tags, ctx.AwsWorker.NewTag("Name", asgName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(provisioners.TagKubernetesCluster, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(provisioners.TagClusterName, clusterName, asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(provisioners.TagInstanceGroupNamespace, instanceGroup.GetNamespace(), asgName))
	tags = append(tags, ctx.AwsWorker.NewTag(provisioners.TagInstanceGroupName, instanceGroup.GetName(), asgName))

	if annotations[ClusterAutoscalerEnabledAnnotation] == "true" {
		tags = append(tags, ctx.AwsWorker.NewTag(fmt.Sprintf("k8s.io/cluster-autoscaler/%v", clusterName), "owned", asgName))
		tags = append(tags, ctx.AwsWorker.NewTag("k8s.io/cluster-autoscaler/enabled", "true", asgName))

		for label, labelValue := range labels {
			tags = append(tags, ctx.AwsWorker.NewTag(fmt.Sprintf("k8s.io/cluster-autoscaler/node-template/label/%v", label), labelValue, asgName))
		}

		for _, taint := range taints {
			tagValue := fmt.Sprintf("%v:%v", taint.Value, taint.Effect)
			tag := ctx.AwsWorker.NewTag(fmt.Sprintf("k8s.io/cluster-autoscaler/node-template/taint/%s", taint.Key), tagValue, asgName)
			tags = append(tags, tag)
		}
	}

	// custom tags
	for _, tagSlice := range configuration.GetTags() {
		tags = append(tags, ctx.AwsWorker.NewTag(tagSlice["key"], tagSlice["value"], asgName))
	}
	return tags
}

func (ctx *EksInstanceGroupContext) GetRemovedTags(asgName string) []*autoscaling.Tag {
	var (
		removal      []*autoscaling.Tag
		state        = ctx.GetDiscoveredState()
		scalingGroup = state.GetScalingGroup()
		addedTags    = ctx.GetAddedTags(asgName)
	)

	for _, tag := range scalingGroup.Tags {
		var match bool
		for _, t := range addedTags {
			if aws.StringValue(t.Key) == aws.StringValue(tag.Key) {
				match = true
			}
		}
		if !match {
			matchedTag := ctx.AwsWorker.NewTag(aws.StringValue(tag.Key), aws.StringValue(tag.Value), asgName)
			removal = append(removal, matchedTag)
		}
	}

	return removal
}

func (ctx *EksInstanceGroupContext) UpdateScalingProcesses(asgName string) error {
	var (
		instanceGroup         = ctx.GetInstanceGroup()
		configuration         = instanceGroup.GetEKSConfiguration()
		state                 = ctx.GetDiscoveredState()
		scalingGroup          = state.GetScalingGroup()
		specSuspendProcesses  = configuration.GetSuspendProcesses()
		groupSuspendProcesses []string
	)

	// handle 'all' metrics provided
	if common.ContainsEqualFold(specSuspendProcesses, "all") {
		specSuspendProcesses = awsprovider.DefaultSuspendProcesses
	}

	for _, element := range scalingGroup.SuspendedProcesses {
		groupSuspendProcesses = append(groupSuspendProcesses, *element.ProcessName)
	}

	if suspend := common.Difference(specSuspendProcesses, groupSuspendProcesses); len(suspend) > 0 {
		if err := ctx.AwsWorker.SetSuspendProcesses(asgName, suspend); err != nil {
			return err
		}
		ctx.Log.Info("suspended scaling processes", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName, "processes", suspend)
	}

	if resume := common.Difference(groupSuspendProcesses, specSuspendProcesses); len(resume) > 0 {
		if err := ctx.AwsWorker.SetResumeProcesses(asgName, resume); err != nil {
			return err
		}
		ctx.Log.Info("resumed scaling processes", "instancegroup", instanceGroup.GetName(), "scalinggroup", asgName, "processes", resume)
	}

	return nil
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
		labelList      []string
		isOverride     bool
		instanceGroup  = ctx.GetInstanceGroup()
		annotations    = instanceGroup.GetAnnotations()
		configuration  = instanceGroup.GetEKSConfiguration()
		mixedInstances = configuration.GetMixedInstancesPolicy()
		customLabels   = configuration.GetLabels()
	)

	// get custom labels
	if len(customLabels) > 0 {
		for k, v := range customLabels {
			labelList = append(labelList, fmt.Sprintf("%v=%v", k, v))
		}
	}

	// allow override default labels
	if val, ok := annotations[OverrideDefaultLabelsAnnotationKey]; ok {
		isOverride = true
		overrideLabels := strings.Split(val, ",")
		for _, label := range overrideLabels {
			labelList = append(labelList, label)
		}
	}

	if !isOverride {
		// add default labels
		labelList = append(labelList, fmt.Sprintf(RoleNewLabelFmt, instanceGroup.GetName()))

		// add the old style role label if the cluster's k8s version is < 1.16
		clusterVersion := ctx.DiscoveredState.GetClusterVersion()
		ver, err := semver.NewVersion(clusterVersion)
		if err != nil {
			ctx.Log.Error(err, "Failed parsing the cluster's kubernetes version", "instancegroup", instanceGroup.GetName())
			labelList = append(labelList, fmt.Sprintf(RoleOldLabelFmt, instanceGroup.GetName()))
		} else {
			c, _ := semver.NewConstraint("< 1.16-0")
			if c.Check(ver) {
				labelList = append(labelList, fmt.Sprintf(RoleOldLabelFmt, instanceGroup.GetName()))
			}
		}
	}

	if mixedInstances != nil {
		ratio := common.IntOrStrValue(mixedInstances.SpotRatio)
		if ratio > 0 {
			labelList = append(labelList, fmt.Sprintf(InstanceMgrLabelFmt, "lifecycle", v1alpha1.LifecycleStateMixed))
		} else {
			labelList = append(labelList, fmt.Sprintf(InstanceMgrLabelFmt, "lifecycle", v1alpha1.LifecycleStateNormal))
		}
	} else if configuration.GetSpotPrice() != "" {
		labelList = append(labelList, fmt.Sprintf(InstanceMgrLabelFmt, "lifecycle", v1alpha1.LifecycleStateSpot))
	} else {
		labelList = append(labelList, fmt.Sprintf(InstanceMgrLabelFmt, "lifecycle", v1alpha1.LifecycleStateNormal))
	}

	sort.Strings(labelList)
	return labelList
}

func (ctx *EksInstanceGroupContext) GetBootstrapArgs() string {
	return fmt.Sprintf("--kubelet-extra-args '%v'", ctx.GetKubeletExtraArgs())
}

func (ctx *EksInstanceGroupContext) GetKubeletExtraArgs() string {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		bootstrapArgs = configuration.GetBootstrapArguments()
	)

	labelsFlag := fmt.Sprintf("--node-labels=%v", strings.Join(ctx.GetLabelList(), ","))
	taintsFlag := fmt.Sprintf("--register-with-taints=%v", strings.Join(ctx.GetTaintList(), ","))
	return fmt.Sprintf("%v %v %v", labelsFlag, taintsFlag, bootstrapArgs)
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

	// Ignore recommendations until instance group is provisioned
	if !state.IsProvisioned() {
		return nil
	}

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
			ctx.Log.Info("using manually configured spot price", "instancegroup", instanceGroup.GetName(), "spotPrice", configuration.GetSpotPrice())
		} else {
			// if recommendation was used, set flag to false
			status.SetUsingSpotRecommendation(false)
		}
		return nil
	}

	// set the recommendation given
	status.SetUsingSpotRecommendation(true)

	if recommendation.UseSpot {
		ctx.Log.Info("spot enabled with spot price recommendation", "instancegroup", instanceGroup.GetName(), "spotPrice", recommendation.SpotPrice)
		configuration.SetSpotPrice(recommendation.SpotPrice)
	} else {
		ctx.Log.Info("spot disabled due to recommendation", "instancegroup", instanceGroup.GetName())
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
			if key == provisioners.TagClusterName && strings.EqualFold(value, clusterName) {
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
			if key == provisioners.TagInstanceGroupName && value == instanceGroup.GetName() {
				nameMatch = true
			}
			if key == provisioners.TagInstanceGroupNamespace && value == instanceGroup.GetNamespace() {
				namespaceMatch = true
			}
		}
		if nameMatch && namespaceMatch {
			return group
		}
	}

	return nil
}

func (ctx *EksInstanceGroupContext) UpdateNodeReadyCondition() bool {
	var (
		state         = ctx.GetDiscoveredState()
		instanceGroup = ctx.GetInstanceGroup()
		status        = instanceGroup.GetStatus()
		scalingGroup  = state.GetScalingGroup()
		desiredCount  = int(aws.Int64Value(scalingGroup.DesiredCapacity))
		nodes         = state.GetClusterNodes()
	)

	if scalingGroup == nil {
		return false
	}

	ctx.Log.Info("waiting for node readiness conditions", "instancegroup", instanceGroup.GetName())
	if len(scalingGroup.Instances) != desiredCount {
		// if instances don't match desired, a scaling activity is in progress
		return false
	}

	instanceIds := make([]string, 0)
	for _, instance := range scalingGroup.Instances {
		instanceIds = append(instanceIds, aws.StringValue(instance.InstanceId))
	}

	instances := strings.Join(instanceIds, ",")

	var conditions []v1alpha1.InstanceGroupCondition
	ok, err := kubeprovider.IsDesiredNodesReady(nodes, instanceIds, desiredCount)
	if err != nil {
		ctx.Log.Error(err, "could not update node conditions", "instancegroup", instanceGroup.GetName())
		return false
	}
	if ok {
		if !state.IsNodesReady() {
			state.Publisher.Publish(kubeprovider.NodesReadyEvent, "instancegroup", instanceGroup.GetName(), "instances", instances)
		}
		ctx.Log.Info("desired nodes are ready", "instancegroup", instanceGroup.GetName(), "instances", instances)
		state.SetNodesReady(true)
		conditions = append(conditions, v1alpha1.NewInstanceGroupCondition(v1alpha1.NodesReady, corev1.ConditionTrue))
		status.SetConditions(conditions)
		return true
	}

	if state.IsNodesReady() {
		state.Publisher.Publish(kubeprovider.NodesNotReadyEvent, "instancegroup", instanceGroup.GetName(), "instances", instances)
	}
	ctx.Log.Info("desired nodes are not ready", "instancegroup", instanceGroup.GetName(), "instances", instances)
	state.SetNodesReady(false)
	conditions = append(conditions, v1alpha1.NewInstanceGroupCondition(v1alpha1.NodesReady, corev1.ConditionFalse))
	status.SetConditions(conditions)
	return false
}

func (ctx *EksInstanceGroupContext) GetEnabledMetrics() ([]string, bool) {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		configuration  = instanceGroup.GetEKSConfiguration()
		metrics        = configuration.GetMetricsCollection()
		state          = ctx.GetDiscoveredState()
		scalingGroup   = state.GetScalingGroup()
		enableMetrics  = make([]string, 0)
		enabledMetrics = make([]string, 0)
		desiredMetrics []string
	)

	// handle 'all' metrics provided
	if common.ContainsEqualFold(metrics, "all") {
		desiredMetrics = awsprovider.DefaultAutoscalingMetrics
	} else {
		desiredMetrics = metrics
	}

	// get all already enabled metrics
	for _, m := range scalingGroup.EnabledMetrics {
		enabledMetrics = append(enabledMetrics, aws.StringValue(m.Metric))

	}

	// add desired which are not enabled
	for _, m := range desiredMetrics {
		if !common.ContainsString(enabledMetrics, m) {
			enableMetrics = append(enableMetrics, m)
		}
	}

	if common.SliceEmpty(enableMetrics) {
		return enableMetrics, false
	}

	return enableMetrics, true
}

func (ctx *EksInstanceGroupContext) GetDisabledMetrics() ([]string, bool) {
	var (
		instanceGroup   = ctx.GetInstanceGroup()
		configuration   = instanceGroup.GetEKSConfiguration()
		metrics         = configuration.GetMetricsCollection()
		state           = ctx.GetDiscoveredState()
		scalingGroup    = state.GetScalingGroup()
		disabledMetrics = make([]string, 0)
		desiredMetrics  []string
	)

	// handle 'all' metrics provided
	if common.ContainsEqualFold(metrics, "all") {
		desiredMetrics = awsprovider.DefaultAutoscalingMetrics
	} else {
		desiredMetrics = metrics
	}

	// find metrics that need to be disabled
	for _, m := range scalingGroup.EnabledMetrics {
		metricName := aws.StringValue(m.Metric)
		if !common.ContainsString(desiredMetrics, metricName) {
			disabledMetrics = append(disabledMetrics, metricName)
		}
	}

	if common.SliceEmpty(disabledMetrics) {
		return disabledMetrics, false
	}

	return disabledMetrics, true
}

func (ctx *EksInstanceGroupContext) UpdateMetricsCollection(asgName string) error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)

	if metrics, ok := ctx.GetDisabledMetrics(); ok {
		if err := ctx.AwsWorker.DisableMetrics(asgName, metrics); err != nil {
			return errors.Wrapf(err, "failed to disable metrics %v", metrics)
		}
		ctx.Log.Info("disabled metrics collection", "instancegroup", instanceGroup.GetName(), "metrics", metrics)
	}

	if metrics, ok := ctx.GetEnabledMetrics(); ok {
		if err := ctx.AwsWorker.EnableMetrics(asgName, metrics); err != nil {
			return errors.Wrapf(err, "failed to enable metrics %v", metrics)
		}
		ctx.Log.Info("enabled metrics collection", "instancegroup", instanceGroup.GetName(), "metrics", metrics)
	}
	return nil
}

func (ctx *EksInstanceGroupContext) GetRemovedHooks() ([]string, bool) {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		configuration = instanceGroup.GetEKSConfiguration()
		desiredHooks  = configuration.GetLifecycleHooks()
	)

	existingHooks := []v1alpha1.LifecycleHookSpec{}
	for _, h := range state.LifecycleHooks {
		hook := v1alpha1.LifecycleHookSpec{
			Name:             aws.StringValue(h.LifecycleHookName),
			Lifecycle:        aws.StringValue(h.LifecycleTransition),
			DefaultResult:    aws.StringValue(h.DefaultResult),
			HeartbeatTimeout: aws.Int64Value(h.HeartbeatTimeout),
			NotificationArn:  aws.StringValue(h.NotificationTargetARN),
			Metadata:         aws.StringValue(h.NotificationMetadata),
			RoleArn:          aws.StringValue(h.RoleARN),
		}
		existingHooks = append(existingHooks, hook)
	}

	removeHooks := make([]string, 0)
	for _, e := range existingHooks {
		if !e.ExistInSlice(desiredHooks) {
			removeHooks = append(removeHooks, e.Name)
		}
	}

	if len(removeHooks) == 0 {
		return []string{}, false
	}

	return removeHooks, true
}

func (ctx *EksInstanceGroupContext) GetAddedHooks() ([]v1alpha1.LifecycleHookSpec, bool) {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		configuration = instanceGroup.GetEKSConfiguration()
		desiredHooks  = configuration.GetLifecycleHooks()
	)

	existingHooks := []v1alpha1.LifecycleHookSpec{}
	for _, h := range state.LifecycleHooks {
		hook := v1alpha1.LifecycleHookSpec{
			Name:             aws.StringValue(h.LifecycleHookName),
			Lifecycle:        aws.StringValue(h.LifecycleTransition),
			DefaultResult:    aws.StringValue(h.DefaultResult),
			HeartbeatTimeout: aws.Int64Value(h.HeartbeatTimeout),
			NotificationArn:  aws.StringValue(h.NotificationTargetARN),
			Metadata:         aws.StringValue(h.NotificationMetadata),
			RoleArn:          aws.StringValue(h.RoleARN),
		}
		existingHooks = append(existingHooks, hook)
	}

	addHooks := make([]v1alpha1.LifecycleHookSpec, 0)
	for _, d := range desiredHooks {
		if !d.ExistInSlice(existingHooks) {
			addHooks = append(addHooks, d)
		}
	}

	if len(addHooks) == 0 {
		return addHooks, false
	}

	return addHooks, true
}

func (ctx *EksInstanceGroupContext) UpdateLifecycleHooks(asgName string) error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)

	if hooks, ok := ctx.GetRemovedHooks(); ok {
		for _, hook := range hooks {
			if err := ctx.AwsWorker.DeleteLifecycleHook(asgName, hook); err != nil {
				return errors.Wrapf(err, "failed to remove lifecycle hook %v", hook)
			}
			ctx.Log.Info("deleting lifecycle hook", "instancegroup", instanceGroup.GetName(), "hook", hook)
		}
	}

	if hooks, ok := ctx.GetAddedHooks(); ok {
		for _, hook := range hooks {
			input := &autoscaling.PutLifecycleHookInput{
				AutoScalingGroupName: aws.String(asgName),
				LifecycleHookName:    aws.String(hook.Name),
				DefaultResult:        aws.String(hook.DefaultResult),
				HeartbeatTimeout:     aws.Int64(hook.HeartbeatTimeout),
				LifecycleTransition:  aws.String(hook.Lifecycle),
			}

			if !common.StringEmpty(hook.Metadata) {
				input.NotificationMetadata = aws.String(hook.Metadata)
			}

			if !common.StringEmpty(hook.RoleArn) {
				input.RoleARN = aws.String(hook.RoleArn)
			}

			if !common.StringEmpty(hook.NotificationArn) {
				input.NotificationTargetARN = aws.String(hook.NotificationArn)
			}

			if err := ctx.AwsWorker.CreateLifecycleHook(input); err != nil {
				return errors.Wrapf(err, "failed to add lifecycle hook %v", hook)
			}
			ctx.Log.Info("creating lifecycle hook", "instancegroup", instanceGroup.GetName(), "hook", hook)
		}
	}
	return nil
}

func (ctx *EksInstanceGroupContext) GetManagedPoliciesList(additionalPolicies []string) []string {
	managedPolicies := make([]string, 0)
	for _, name := range additionalPolicies {
		switch {
		case strings.HasPrefix(name, awsprovider.IAMPolicyPrefix):
			managedPolicies = append(managedPolicies, name)
		case strings.HasPrefix(name, awsprovider.IAMARNPrefix):
			managedPolicies = append(managedPolicies, name)
		default:
			managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
		}
	}

	for _, name := range DefaultManagedPolicies {
		managedPolicies = append(managedPolicies, fmt.Sprintf("%s/%s", awsprovider.IAMPolicyPrefix, name))
	}

	return managedPolicies
}

func (ctx *EksInstanceGroupContext) RemoveAuthRole(arn string) error {
	ctx.Lock()
	defer ctx.Unlock()

	var instanceGroup = ctx.GetInstanceGroup()
	var osFamily = ctx.GetOsFamily()
	var list = &unstructured.UnstructuredList{}
	var sharedGroups = make([]string, 0)

	list, err := ctx.KubernetesClient.KubeDynamic.Resource(v1alpha1.GroupVersionResource).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// find objects which share the same nodesInstanceRoleArn
	for _, obj := range list.Items {
		if val, ok, _ := unstructured.NestedString(obj.Object, "status", "nodesInstanceRoleArn"); ok {
			if strings.EqualFold(arn, val) {
				sharedGroups = append(sharedGroups, obj.GetName())
			}
		}
	}

	// If there are other instance groups using the same role we should not remove it from aws-auth
	if len(sharedGroups) > 1 {
		ctx.Log.Info(
			"skipping removal of auth role, is used by another instancegroup",
			"instancegroup", instanceGroup.GetName(),
			"arn", arn,
			"conflict", strings.Join(sharedGroups, ","),
		)
		return nil
	}

	return common.RemoveAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{arn}, []string{osFamily})
}

func (ctx *EksInstanceGroupContext) GetOverrides() []*autoscaling.LaunchTemplateOverrides {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		primaryType   = configuration.InstanceType
		mixedPolicy   = configuration.GetMixedInstancesPolicy()
		state         = ctx.GetDiscoveredState()
	)
	overrides := []*autoscaling.LaunchTemplateOverrides{}

	if mixedPolicy.InstanceTypes != nil {
		overrides = append(overrides, &autoscaling.LaunchTemplateOverrides{
			InstanceType:     aws.String(primaryType),
			WeightedCapacity: aws.String("1"),
		})
		for _, instance := range mixedPolicy.InstanceTypes {
			weightStr := strconv.FormatInt(instance.Weight, 10)
			overrides = append(overrides, &autoscaling.LaunchTemplateOverrides{
				InstanceType:     aws.String(instance.Type),
				WeightedCapacity: aws.String(weightStr),
			})
		}
		return overrides
	}

	switch {
	case strings.EqualFold(*mixedPolicy.InstancePool, string(SubFamilyFlexible)):
		if pool, ok := state.SubFamilyFlexible.GetInstancePool(primaryType); ok {
			for _, p := range pool {
				overrides = append(overrides, &autoscaling.LaunchTemplateOverrides{
					InstanceType:     aws.String(p.Type),
					WeightedCapacity: aws.String(p.Weight),
				})
			}
		}
	}

	return overrides
}

func (ctx *EksInstanceGroupContext) GetDesiredMixedInstancesPolicy(name string) *autoscaling.MixedInstancesPolicy {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		configuration = instanceGroup.GetEKSConfiguration()
		mixedPolicy   = configuration.GetMixedInstancesPolicy()
	)

	if mixedPolicy == nil {
		return nil
	}

	overrides := ctx.GetOverrides()

	var allocationStrategy string
	strategy := common.StringValue(mixedPolicy.Strategy)
	if strings.EqualFold(strategy, v1alpha1.LaunchTemplateStrategyCapacityOptimized) {
		allocationStrategy = awsprovider.LaunchTemplateStrategyCapacityOptimized
	}
	if strings.EqualFold(strategy, v1alpha1.LaunchTemplateStrategyLowestPrice) {
		allocationStrategy = awsprovider.LaunchTemplateStrategyLowestPrice
	}

	spotRatio := common.IntOrStrValue(mixedPolicy.SpotRatio)

	policy := &autoscaling.MixedInstancesPolicy{
		InstancesDistribution: &autoscaling.InstancesDistribution{
			OnDemandBaseCapacity:                mixedPolicy.BaseCapacity,
			SpotAllocationStrategy:              aws.String(allocationStrategy),
			SpotInstancePools:                   mixedPolicy.SpotPools,
			OnDemandPercentageAboveBaseCapacity: aws.Int64(int64(100 - spotRatio)),
		},
		LaunchTemplate: &autoscaling.LaunchTemplate{
			LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String(name),
				Version:            aws.String(awsprovider.LaunchTemplateLatestVersionKey),
			},
			Overrides: overrides,
		},
	}

	return policy
}
