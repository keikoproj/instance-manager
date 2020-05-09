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

	"k8s.io/apimachinery/pkg/api/errors"

	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
)

var (
	log                       = logrus.New()
	TagClusterName            = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName      = "instancegroups.keikoproj.io/InstanceGroup"
	TagClusterNamespace       = "instancegroups.keikoproj.io/Namespace"
	outputLaunchConfiguration = "LaunchConfigName"
	outputScalingGroupName    = "AsgName"
	outputGroupARN            = "NodeInstanceRole"
)

const (
	OngoingStateString             = "OngoingState"
	FiniteStateString              = "FiniteState"
	FiniteDeletedString            = "FiniteDeleted"
	UpdateRecoverableErrorString   = "UpdateRecoverableError"
	UnrecoverableErrorString       = "UnrecoverableError"
	UnrecoverableDeleteErrorString = "UnrecoverableDeleteError"
)

// New constructs a new instance group provisioner of EKS Cloudformation type
func New(instanceGroup *v1alpha1.InstanceGroup, k common.KubernetesClientSet, w awsprovider.AwsWorker) (*EksCfInstanceGroupContext, error) {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	var specConfig = instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration

	vpcID, err := w.DeriveEksVpcID(specConfig.GetClusterName())
	if err != nil {
		return &EksCfInstanceGroupContext{}, err
	}

	ctx := &EksCfInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: k,
		AwsWorker:        w,
		VpcID:            vpcID,
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)

	err = ctx.processParameters()
	if err != nil {
		log.Errorf("failed to parse cloudformation parameters: %v", err)
		return &EksCfInstanceGroupContext{}, err
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
	ctx.reloadDiscoveryCache()
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
	ctx.reloadDiscoveryCache()
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
	ctx.reloadDiscoveryCache()
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
		log.Errorf("failed to detect if upgrade is needed: %v", err)
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
					// deleting stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if awsprovider.IsStackInConditionState(stackStatus, FiniteStateString) {
					// deleting stack is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if awsprovider.IsStackInConditionState(stackStatus, UpdateRecoverableErrorString) {
					// deleting stack is in an update recoverable state
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if awsprovider.IsStackInConditionState(stackStatus, FiniteDeletedString) {
					// deleting stack is in a finite-deleted state
					instanceGroup.SetState(v1alpha1.ReconcileDeleted)
				} else if awsprovider.IsStackInConditionState(stackStatus, UnrecoverableDeleteErrorString) {
					// deleting stack is in a unrecoverable delete error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				} else if awsprovider.IsStackInConditionState(stackStatus, UnrecoverableErrorString) {
					// deleting stack is in a unrecoverable error state - allow it to delete
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
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
	provisionerConfig := spec.EKSCFSpec
	specConfig := provisionerConfig.EKSCFConfiguration
	state := ctx.GetDiscoveredState()
	stacks := state.GetCloudformationStacks()

	for _, stack := range stacks {
		var group DiscoveredInstanceGroup
		group.StackName = aws.StringValue(stack.StackName)
		for _, tag := range stack.Tags {
			key := aws.StringValue(tag.Key)
			value := aws.StringValue(tag.Value)
			switch key {
			case TagClusterName:
				group.ClusterName = value
				if value == specConfig.GetClusterName() {
					group.IsClusterMember = true
				}
			case TagClusterNamespace:
				group.Namespace = value
			case TagInstanceGroupName:
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
			var groupDeleting bool
			var groupDeleted bool
			groupDeleting, err := ctx.isResourceDeleting(v1alpha1.GroupVersionResource, group.Namespace, group.Name)
			if err != nil {
				if errors.IsNotFound(err) {
					groupDeleted = true
				} else {
					log.Errorf("failed to determine whether %v/%v is being deleted: %v", group.Namespace, group.Name, err)
				}
			}
			if groupDeleting || groupDeleted {
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
	err := ctx.reloadDiscoveryCache()
	if err != nil {
		return err
	}
	ctx.discoverSpotPrice()
	ctx.setRollingStrategyConfigurationDefaults()
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
	provisionerConfig := spec.EKSCFSpec
	specConfig := provisionerConfig.EKSCFConfiguration

	tags = map[string]string{
		TagClusterName:       specConfig.GetClusterName(),
		TagInstanceGroupName: meta.GetName(),
		TagClusterNamespace:  meta.GetNamespace(),
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

func (ctx *EksCfInstanceGroupContext) reloadDiscoveryCache() error {
	discovery := &DiscoveredState{}
	instanceGroup := ctx.GetInstanceGroup()
	status := &instanceGroup.Status
	stacksOutput, err := ctx.AwsWorker.DescribeCloudformationStacks()
	if err != nil {
		log.Errorf("failed to DescribeStacks: %v", err)
		return err
	}

	scalingGroups, err := ctx.AwsWorker.DescribeAutoscalingGroups()
	if err != nil {
		log.Errorf("failed to DescribeAutoscalingGroups: %v", err)
		return err
	}

	launchConfigOutput, err := ctx.AwsWorker.DescribeAutoscalingLaunchConfigs()
	if err != nil {
		log.Errorf("failed to DescribeAutoscalingLaunchConfigs: %v", err)
		return err
	}

	discovery.SetScalingGroups(scalingGroups)
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

	for _, launchConfig := range launchConfigOutput.LaunchConfigurations {
		launchConfigName := aws.StringValue(launchConfig.LaunchConfigurationName)
		launchSpotPrice := aws.StringValue(launchConfig.SpotPrice)
		knownLaunchConfig := status.GetActiveLaunchConfigurationName()
		if knownLaunchConfig == launchConfigName {
			if launchSpotPrice != "" {
				status.SetLifecycle("spot")
			} else {
				status.SetLifecycle("normal")
			}
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

func (ctx *EksCfInstanceGroupContext) processParameters() error {
	instanceGroup := ctx.GetInstanceGroup()
	spec := &instanceGroup.Spec
	meta := &instanceGroup.ObjectMeta
	provisionerConfig := spec.EKSCFSpec
	specConfig := provisionerConfig.EKSCFConfiguration

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

	existingRole, existingProfile := getExistingIAM(specConfig.GetRoleName(), specConfig.GetInstanceProfileName())

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
		"ManagedPolicyARNs":           getManagedPolicyARNs(specConfig.ManagedPolicies),
		"NodeAutoScalingGroupMetrics": getNodeAutoScalingGroupMetrics(specConfig.GetMetricsCollection()),
	}

	if existingRole != "" && existingProfile != "" {
		rawParameters["ExistingRoleName"] = existingRole
		rawParameters["ExistingInstanceProfileName"] = existingProfile
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

func getExistingIAM(role, profile string) (string, string) {
	var (
		rolePrefix                                    = ":role/"
		instanceProfilePrefix                         = ":instance-profile/"
		existingRoleName, existingInstanceProfileName string
	)

	if role == "" {
		return "", ""
	} else if strings.Contains(role, rolePrefix) {
		trim := strings.Split(role, rolePrefix)
		existingRoleName = trim[1]
	} else {
		existingRoleName = role
	}

	if profile == "" {
		// use role name if profile not provided
		existingInstanceProfileName = existingRoleName
	} else if strings.Contains(profile, instanceProfilePrefix) {
		trim := strings.Split(profile, instanceProfilePrefix)
		existingInstanceProfileName = trim[1]
	} else {
		existingInstanceProfileName = profile
	}

	return existingRoleName, existingInstanceProfileName
}

// getManagedPolicyARNs constructs managed policy arns
func getManagedPolicyARNs(pNames []string) string {
	// This is for Managed Policy ARN list.
	// First add the DEFAULT required policies to the list passed by user as part of custom resource
	var managedPolicyARNs []string
	requiredPolicies := []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
	policyPrefix := "arn:aws:iam::aws:policy/"
	for _, name := range requiredPolicies {
		managedPolicyARNs = append(managedPolicyARNs, fmt.Sprintf("%s%s", policyPrefix, name))
	}
	// Add the user supplied policy names if any
	if len(pNames) != 0 {
		for _, name := range pNames {
			// Append to list directly if user supplied entire ARN of policy
			// Notes some customized policies may have prefix like 'arn:aws:iam::{account_number}:policy/' instead of 'arn:aws:iam::aws:policy/'
			match, _ := regexp.MatchString("^arn:aws:iam::(aws|\\d{12}):policy/", name)
			if match {
				managedPolicyARNs = append(managedPolicyARNs, name)
			} else {
				managedPolicyARNs = append(managedPolicyARNs, fmt.Sprintf("%s%s", policyPrefix, name))
			}
		}
	}

	return common.ConcatonateList(managedPolicyARNs, ",")
}

func getNodeAutoScalingGroupMetrics(metrics []string) string {
	if len(metrics) == 0 || len(metrics) == 1 && metrics[0] == "all" {
		return ""
	}
	var resp []string
	for _, metric := range metrics {
		resp = append(resp, strings.Title(metric))
	}
	return common.ConcatonateList(resp, ",")
}
