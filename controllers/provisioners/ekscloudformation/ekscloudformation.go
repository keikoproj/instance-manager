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

package ekscloudformation

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/orkaproj/instance-manager/api/v1alpha1"
	"github.com/orkaproj/instance-manager/controllers/common"
	awsprovider "github.com/orkaproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
)

var (
	log                       = logrus.New()
	tagClusterName            = "instancegroups.orkaproj.io/ClusterName"
	tagInstanceGroupName      = "instancegroups.orkaproj.io/InstanceGroup"
	tagClusterNamespace       = "instancegroups.orkaproj.io/Namespace"
	outputLaunchConfiguration = "LaunchConfigName"
	outputScalingGroupName    = "AsgName"
	outputGroupARN            = "NodeInstanceRole"
	groupVersionResource      = schema.GroupVersionResource{
		Group:    "instancemgr.orkaproj.io",
		Version:  "v1alpha1",
		Resource: "instancegroups",
	}
)

const (
	OngoingStateString           = "OngoingState"
	FiniteStateString            = "FiniteState"
	FiniteDeletedString          = "FiniteDeleted"
	UpdateRecoverableErrorString = "UpdateRecoverableError"
	UnrecoverableErrorString     = "UnrecoverableError"
)

// New constructs a new instance group provisioner of EKS Cloudformation type
func New(instanceGroup *v1alpha1.InstanceGroup, k common.KubernetesClientSet, w awsprovider.AwsWorker) (EksCfInstanceGroupContext, error) {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	var specConfig = &instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration

	vpcID, err := w.DeriveEksVpcID(specConfig.GetClusterName())
	if err != nil {
		return EksCfInstanceGroupContext{}, err
	}

	ctx := EksCfInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: k,
		AwsWorker:        w,
		VpcID:            vpcID,
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)

	err = ctx.processParameters()
	if err != nil {
		log.Errorf("failed to parse cloudformation parameters: %v", err)
		return EksCfInstanceGroupContext{}, err
	}

	return ctx, nil
}

func (ctx *EksCfInstanceGroupContext) Create() error {
	var err error
	instanceGroup := ctx.GetInstanceGroup()
	err = ctx.AwsWorker.CreateCloudformationStack()
	if err != nil {
		log.Errorf("failed to submit CreateStack: %v", err)
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileModifying)
	ctx.CloudDiscovery()
	return nil
}

func (ctx *EksCfInstanceGroupContext) Update() error {
	var err error
	instanceGroup := ctx.GetInstanceGroup()

	err, updateNeeded := ctx.AwsWorker.UpdateCloudformationStack()
	if err != nil {
		log.Errorf("failed to submit UpdateStack: %v", err)
		return err
	}
	if updateNeeded {
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
	} else {
		if ctx.IsUpgradeNeeded() {
			instanceGroup.SetState(v1alpha1.ReconcileInitUpgrade)
		} else {
			instanceGroup.SetState(v1alpha1.ReconcileModified)
		}
	}
	ctx.CloudDiscovery()
	return nil
}

func (ctx *EksCfInstanceGroupContext) UpgradeNodes() error {
	instanceGroup := ctx.GetInstanceGroup()
	upgradeStrategy := &instanceGroup.Spec.AwsUpgradeStrategy

	switch strings.ToLower(upgradeStrategy.GetType()) {
	case "crd":
		err := ctx.processCRDStrategy()
		if err != nil {
			log.Errorf("failed to process CRD strategy")
			return err
		}
	case "rollingupdate":
		log.Infof("upgrade strategy is set to '%v', will use cloudformation to rotate nodes", upgradeStrategy.GetType())
	default:
		err := fmt.Errorf("'%v' is not an implemented upgrade type, will not process upgrade", upgradeStrategy.GetType())
		return err
	}
	return nil
}

func (ctx *EksCfInstanceGroupContext) Delete() error {
	var err error
	instanceGroup := ctx.GetInstanceGroup()

	err = ctx.updateAuthConfigMap()
	if err != nil {
		log.Errorf("failed to remove role from aws-auth configmap: %v", err)
		return err
	}

	err = ctx.AwsWorker.DeleteCloudformationStack()
	if err != nil {
		log.Errorf("failed to submit DeleteStack: %v", err)
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileDeleting)
	ctx.CloudDiscovery()
	return nil
}

func (ctx *EksCfInstanceGroupContext) IsProvisioned() bool {
	return ctx.AwsWorker.CloudformationStackExists()
}

func (ctx *EksCfInstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileModified || instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
		return true
	}
	return false
}

func (ctx *EksCfInstanceGroupContext) IsUpgradeNeeded() bool {
	discovery := ctx.GetDiscoveredState()
	selfGroup := discovery.GetSelfGroup()
	drifted, err := ctx.AwsWorker.DetectScalingGroupDrift(selfGroup.ScalingGroupName)
	if err != nil {
		log.Errorln("failed to detect if upgrade is needed")
		return false
	}
	if drifted {
		return true
	}
	return false
}

func (ctx *EksCfInstanceGroupContext) StateDiscovery() {
	var stackStatus string
	instanceGroup := ctx.GetInstanceGroup()
	provisioned := ctx.IsProvisioned()
	state := ctx.GetDiscoveredState()

	if state.SelfStack != nil {
		stackStatus = aws.StringValue(state.SelfStack.StackStatus)
	}

	if instanceGroup.GetState() == v1alpha1.ReconcileInit {
		if ctx.InstanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			// resource is not being deleted
			if provisioned {
				// stack Exists
				if awsprovider.IsStackInConditionState(stackStatus, OngoingStateString) {
					// stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
				} else if awsprovider.IsStackInConditionState(stackStatus, FiniteStateString) {
					// stack is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if awsprovider.IsStackInConditionState(stackStatus, UpdateRecoverableErrorString) {
					// stack is in update-recoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if awsprovider.IsStackInConditionState(stackStatus, UnrecoverableErrorString) {
					// stack is in unrecoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				// stack does not exist
				instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			// resource is being deleted
			if provisioned {
				if awsprovider.IsStackInConditionState(stackStatus, OngoingStateString) {
					// stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if awsprovider.IsStackInConditionState(stackStatus, FiniteStateString) {
					// stack is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if awsprovider.IsStackInConditionState(stackStatus, FiniteDeletedString) {
					// stack is in a finite-deleted state
					instanceGroup.SetState(v1alpha1.ReconcileDeleted)
				} else if awsprovider.IsStackInConditionState(stackStatus, UnrecoverableErrorString) {
					// stack is in unrecoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				// Stack does not exist
				instanceGroup.SetState(v1alpha1.ReconcileDeleted)
			}
		}
	}
}

func (ctx *EksCfInstanceGroupContext) discoverInstanceGroups() {
	//var groups *DiscoveredInstanceGroups
	groups := DiscoveredInstanceGroups{
		Items: make([]DiscoveredInstanceGroup, 0),
	}
	instanceGroup := ctx.GetInstanceGroup()
	spec := &instanceGroup.Spec
	provisionerConfig := &spec.EKSCFSpec
	specConfig := &provisionerConfig.EKSCFConfiguration
	state := ctx.GetDiscoveredState()
	stacks := state.GetCloudformationStacks()

	for _, stack := range stacks {
		var group DiscoveredInstanceGroup
		group.ClusterName = aws.StringValue(stack.StackName)
		group.StackName = aws.StringValue(stack.StackName)

		for _, tag := range stack.Tags {
			key := aws.StringValue(tag.Key)
			value := aws.StringValue(tag.Value)
			switch key {
			case tagClusterName:
				group.ClusterName = value
				if value == specConfig.GetClusterName() {
					group.IsClusterMember = true
				}
			case tagClusterNamespace:
				group.Namespace = value
			case tagInstanceGroupName:
				group.Name = value
			}
		}
		for _, output := range stack.Outputs {
			key := aws.StringValue(output.OutputKey)
			value := aws.StringValue(output.OutputValue)
			switch key {
			case outputGroupARN:
				group.ARN = value
			case outputLaunchConfiguration:
				group.LaunchConfigName = value
			case outputScalingGroupName:
				group.ScalingGroupName = value
			}
		}

		if group.Namespace != "" && group.Name != "" {
			groupDeleting, err := ctx.isResourceDeleting(groupVersionResource, group.Namespace, group.Name)
			if err != nil {
				log.Errorf("failed to determine whether %v/%v is being deleted: %v", group.Namespace, group.Name, err)
			}
			if groupDeleting {
				group.IsClusterMember = false
			}
		}

		if group.IsClusterMember {
			groups.AddGroup(group)
		}
		if group.StackName == ctx.AwsWorker.StackName {
			state.SetSelfGroup(&group)
		}
	}
	state.SetInstanceGroups(groups)
}

func (ctx *EksCfInstanceGroupContext) CloudDiscovery() error {
	discovery := &DiscoveredState{}
	instanceGroup := ctx.GetInstanceGroup()
	status := &instanceGroup.Status

	stacksOutput, err := ctx.AwsWorker.DescribeCloudformationStacks()
	if err != nil {
		log.Errorf("failed to DescribeStacks: %v", err)
		return err
	}

	asgOutput, err := ctx.AwsWorker.DescribeAutoscalingGroups()
	if err != nil {
		log.Errorf("failed to DescribeAutoscalingGroups: %v", err)
		return err
	}

	launchConfigOutput, err := ctx.AwsWorker.DescribeAutoscalingLaunchConfigs()
	if err != nil {
		log.Errorf("failed to DescribeAutoscalingLaunchConfigs: %v", err)
		return err
	}

	discovery.SetScalingGroups(asgOutput.AutoScalingGroups)
	discovery.SetCloudformationStacks(stacksOutput.Stacks)
	discovery.SetLaunchConfigurations(launchConfigOutput.LaunchConfigurations)
	ctx.SetDiscoveredState(discovery)
	ctx.discoverInstanceGroups()
	discoveredInstanceGroups := discovery.GetInstanceGroups()

	for _, group := range discoveredInstanceGroups.Items {
		if group.StackName == ctx.AwsWorker.StackName {
			status.SetActiveLaunchConfigurationName(group.LaunchConfigName)
			status.SetActiveScalingGroupName(group.ScalingGroupName)
			status.SetNodesArn(group.ARN)
		}
	}

	for _, scalingGroup := range discovery.GetScalingGroups() {
		if aws.StringValue(scalingGroup.AutoScalingGroupName) == status.GetActiveScalingGroupName() {
			status.SetCurrentMax(int(aws.Int64Value(scalingGroup.MaxSize)))
			status.SetCurrentMin(int(aws.Int64Value(scalingGroup.MinSize)))
			break
		}
	}

	for _, stack := range discovery.GetCloudformationStacks() {
		if aws.StringValue(stack.StackName) == ctx.AwsWorker.StackName {
			ctx.DiscoveredState.SetSelfStack(stack)
			log.Infof("stack state: %v", *stack.StackStatus)
		}
	}

	return nil
}

func (ctx *EksCfInstanceGroupContext) BootstrapNodes() error {
	err := ctx.updateAuthConfigMap()
	if err != nil {
		log.Errorln("failed to update bootstrap config map")
		return err
	}
	return nil
}

func (ctx *EksCfInstanceGroupContext) parseTags() {
	var tags map[string]string
	var cloudformationTags []*cloudformation.Tag

	instanceGroup := ctx.GetInstanceGroup()
	spec := &instanceGroup.Spec
	meta := &instanceGroup.ObjectMeta
	provisionerConfig := &spec.EKSCFSpec
	specConfig := &provisionerConfig.EKSCFConfiguration

	tags = map[string]string{
		tagClusterName:       specConfig.GetClusterName(),
		tagInstanceGroupName: meta.GetName(),
		tagClusterNamespace:  meta.GetNamespace(),
	}

	for _, tagSet := range specConfig.GetTags() {
		var resourceTagKey, resourceTagValue string
		for k, v := range tagSet {
			if strings.ToLower(k) == "key" {
				resourceTagKey = v
			} else if strings.ToLower(k) == "value" {
				resourceTagValue = v
			}
		}
		tags[resourceTagKey] = resourceTagValue
	}

	for k, v := range tags {
		tag := &cloudformation.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		}
		cloudformationTags = append(cloudformationTags, tag)
	}
	ctx.AwsWorker.StackTags = cloudformationTags
}

func (ctx *EksCfInstanceGroupContext) processParameters() error {
	instanceGroup := ctx.GetInstanceGroup()
	spec := &instanceGroup.Spec
	meta := &instanceGroup.ObjectMeta
	provisionerConfig := &spec.EKSCFSpec
	specConfig := &provisionerConfig.EKSCFConfiguration

	roleLabel := fmt.Sprintf("node-role.kubernetes.io/%v=\"\"", meta.GetName())
	kubeletArgs := fmt.Sprintf("--node-labels=%v", roleLabel)

	var bootstrapArgs string
	if specConfig.GetBootstrapArgs() != "" {
		bootstrapArgs = (fmt.Sprintf("--kubelet-extra-args '%v %v'", kubeletArgs, specConfig.GetBootstrapArgs()))
	} else {
		bootstrapArgs = fmt.Sprintf("--kubelet-extra-args '%v'", kubeletArgs)
	}

	if specConfig.GetVolSize() == 0 {
		specConfig.SetVolSize(32)
	}

	rawParameters := map[string]string{
		"KeyName":                     specConfig.GetKeyName(),
		"NodeImageId":                 specConfig.GetImage(),
		"NodeInstanceType":            specConfig.GetInstanceType(),
		"ClusterName":                 specConfig.GetClusterName(),
		"NodeVolumeSize":              fmt.Sprint(specConfig.GetVolSize()),
		"NodeAutoScalingGroupMinSize": fmt.Sprint(provisionerConfig.GetMinSize()),
		"NodeAutoScalingGroupMaxSize": fmt.Sprint(provisionerConfig.GetMaxSize()),
		"NodeSecurityGroups":          common.ConcatonateList(specConfig.GetSecurityGroups(), ","),
		"Subnets":                     common.ConcatonateList(specConfig.GetSubnets(), ","),
		"BootstrapArguments":          bootstrapArgs,
		"NodeGroupName":               ctx.AwsWorker.StackName,
		"VpcId":                       ctx.VpcID,
	}

	var parameters []*cloudformation.Parameter
	for k, v := range rawParameters {
		p := &cloudformation.Parameter{
			ParameterKey:   aws.String(k),
			ParameterValue: aws.String(v),
		}
		parameters = append(parameters, p)
	}
	ctx.AwsWorker.StackParameters = parameters
	ctx.parseTags()
	return nil
}
