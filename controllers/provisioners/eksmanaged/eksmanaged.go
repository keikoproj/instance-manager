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

package eksmanaged

import (
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

const (
	OngoingStateString             = "OngoingState"
	FiniteStateString              = "FiniteState"
	UnrecoverableErrorString       = "UnrecoverableError"
	UnrecoverableDeleteErrorString = "UnrecoverableDeleteError"
)

var (
	log                  = logrus.New()
	TagClusterName       = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName = "instancegroups.keikoproj.io/InstanceGroup"
	TagClusterNamespace  = "instancegroups.keikoproj.io/Namespace"
)

func (ctx *EksManagedInstanceGroupContext) CloudDiscovery() error {
	var (
		provisioned     = ctx.AwsWorker.IsNodeGroupExist()
		discoveredState = ctx.GetDiscoveredState()
		instanceGroup   = ctx.GetInstanceGroup()
		status          = &instanceGroup.Status
		groups          = []string{}
	)

	if provisioned {
		discoveredState.SetProvisioned(true)
		err, createdResource := ctx.AwsWorker.GetSelfNodeGroup()
		if err != nil {
			return err
		}
		currentStatus := aws.StringValue(createdResource.Status)
		discoveredState.SetSelfNodeGroup(createdResource)
		discoveredState.SetCurrentState(currentStatus)
		status.SetCurrentMax(int(aws.Int64Value(createdResource.ScalingConfig.MaxSize)))
		status.SetCurrentMin(int(aws.Int64Value(createdResource.ScalingConfig.MinSize)))
		status.SetLifecycle("normal")

		if createdResource.Resources == nil {
			return nil
		}

		for _, asg := range createdResource.Resources.AutoScalingGroups {
			groups = append(groups, aws.StringValue(asg.Name))
		}
		status.SetActiveScalingGroupName(strings.Join(groups, ","))

	} else {
		discoveredState.SetProvisioned(false)
	}
	return nil
}

func (ctx *EksManagedInstanceGroupContext) StateDiscovery() {
	var (
		instanceGroup   = ctx.GetInstanceGroup()
		discoveredState = ctx.GetDiscoveredState()
		provisioned     = discoveredState.IsProvisioned()
		nodeGroupState  = discoveredState.GetCurrentState()
	)

	if instanceGroup.GetState() == v1alpha1.ReconcileInit {
		if ctx.InstanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			// resource is not being deleted
			if provisioned {
				// nodegroup exists
				if awsprovider.IsNodeGroupInConditionState(nodeGroupState, OngoingStateString) {
					// nodegroup is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
				} else if awsprovider.IsNodeGroupInConditionState(nodeGroupState, FiniteStateString) {
					// nodegroup is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if awsprovider.IsNodeGroupInConditionState(nodeGroupState, UnrecoverableErrorString) {
					// nodegroup is in unrecoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				// nodegroup does not exist
				instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			// resource is being deleted
			if provisioned {
				if awsprovider.IsNodeGroupInConditionState(nodeGroupState, OngoingStateString) {
					// deleting nodegroup is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if awsprovider.IsNodeGroupInConditionState(nodeGroupState, UnrecoverableErrorString) {
					// deleting nodegroup is in a unrecoverable error state - allow it to delete
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if awsprovider.IsNodeGroupInConditionState(nodeGroupState, FiniteStateString) {
					// deleting nodegroup is in finite state, delete it
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if awsprovider.IsNodeGroupInConditionState(nodeGroupState, UnrecoverableDeleteErrorString) {
					// deleting nodegroup is in a unrecoverable delete error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				// Stack does not exist
				instanceGroup.SetState(v1alpha1.ReconcileDeleted)
			}
		}
	}
}

func (ctx *EksManagedInstanceGroupContext) Create() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)
	err := ctx.AwsWorker.CreateManagedNodeGroup()
	if err != nil {
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileModifying)
	return nil
}

func (ctx *EksManagedInstanceGroupContext) isUpdateNeeded() bool {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		selfNodeGroup  = ctx.DiscoveredState.GetSelfNodeGroup()
		existingLabels = instanceGroup.Spec.EKSManagedSpec.EKSManagedConfiguration.GetLabels()
		condition      bool
	)

	if instanceGroup.Spec.EKSManagedSpec.GetMinSize() != aws.Int64Value(selfNodeGroup.ScalingConfig.MinSize) {
		condition = true
	}

	if instanceGroup.Spec.EKSManagedSpec.GetMaxSize() != aws.Int64Value(selfNodeGroup.ScalingConfig.MaxSize) {
		condition = true
	}

	if !reflect.DeepEqual(existingLabels, aws.StringValueMap(selfNodeGroup.Labels)) {
		condition = true
	}
	return condition
}

func (ctx *EksManagedInstanceGroupContext) Update() error {
	var (
		instanceGroup  = ctx.GetInstanceGroup()
		nodeLabels     = instanceGroup.Spec.EKSManagedSpec.EKSManagedConfiguration.NodeLabels
		selfNodeGroup  = ctx.DiscoveredState.GetSelfNodeGroup()
		currentDesired = aws.Int64Value(selfNodeGroup.ScalingConfig.DesiredSize)
		requestedMin   = instanceGroup.Spec.EKSManagedSpec.MinSize
	)

	if currentDesired < requestedMin {
		currentDesired = requestedMin
	}

	labels := ctx.AwsWorker.GetLabelsUpdatePayload(aws.StringValueMap(selfNodeGroup.Labels), nodeLabels)

	if ctx.isUpdateNeeded() {
		err := ctx.AwsWorker.UpdateManagedNodeGroup(currentDesired, labels)
		if err != nil {
			return err
		}
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
	} else {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	}

	return nil
}

func (ctx *EksManagedInstanceGroupContext) Delete() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)
	err := ctx.AwsWorker.DeleteManagedNodeGroup()
	if err != nil {
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileDeleting)
	return nil
}

func (ctx *EksManagedInstanceGroupContext) UpgradeNodes() error {
	// upgrade not required
	return nil
}

func (ctx *EksManagedInstanceGroupContext) BootstrapNodes() error {
	// bootstrap not required
	return nil
}

func (ctx *EksManagedInstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileModified || instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
		return true
	}
	return false
}

func New(instanceGroup *v1alpha1.InstanceGroup, k common.KubernetesClientSet, w awsprovider.AwsWorker) (*EksManagedInstanceGroupContext, error) {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	ctx := &EksManagedInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: k,
		AwsWorker:        w,
		DiscoveredState:  &DiscoveredState{},
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)
	ctx.processParameters()

	return ctx, nil
}
func (ctx *EksManagedInstanceGroupContext) processParameters() {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSManagedSpec()
		configuration = instanceGroup.GetEKSManagedConfiguration()
		params        = make(map[string]interface{}, 0)
	)

	params["AmiType"] = configuration.AmiType
	params["ClusterName"] = configuration.EksClusterName
	params["DiskSize"] = int64(configuration.VolSize)
	params["InstanceTypes"] = []string{configuration.InstanceType}
	params["Labels"] = configuration.NodeLabels
	params["NodeRole"] = configuration.NodeRole
	params["NodegroupName"] = instanceGroup.GetName()
	params["ReleaseVersion"] = configuration.ReleaseVersion
	params["Version"] = configuration.Version
	params["Ec2SshKey"] = configuration.KeyPairName
	params["SourceSecurityGroups"] = configuration.NodeSecurityGroups
	params["Subnets"] = configuration.Subnets
	params["Tags"] = configuration.Tags
	params["MinSize"] = spec.GetMinSize()
	params["MaxSize"] = spec.GetMaxSize()
	ctx.AwsWorker.Parameters = params
}

func LoadControllerConfiguration(ig *v1alpha1.InstanceGroup, controllerConfig []byte) (EksManagedDefaultConfiguration, error) {
	var (
		defaultConfig EksManagedDefaultConfiguration
		specConfig    = ig.Spec.EKSManagedSpec.EKSManagedConfiguration
	)

	err := yaml.Unmarshal(controllerConfig, &defaultConfig)
	if err != nil {
		return defaultConfig, err
	}

	if len(defaultConfig.DefaultSubnets) != 0 {
		specConfig.SetSubnets(defaultConfig.DefaultSubnets)
	}

	if defaultConfig.EksClusterName != "" {
		specConfig.SetClusterName(defaultConfig.EksClusterName)
	}

	return defaultConfig, nil
}
