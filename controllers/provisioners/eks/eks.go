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
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

const (
	IAMPolicyPrefix = "arn:aws:iam::aws:policy"
	RoleLabelsFmt   = "node-role.kubernetes.io/%s=\"\",node.kubernetes.io/role=%s"
)

var (
	TagClusterName            = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName      = "instancegroups.keikoproj.io/InstanceGroup"
	TagInstanceGroupNamespace = "instancegroups.keikoproj.io/Namespace"
	TagName                   = "Name"
	DefaultManagedPolicies    = []string{"AmazonEKSWorkerNodePolicy", "AmazonEKS_CNI_Policy", "AmazonEC2ContainerRegistryReadOnly"}
)

func init() {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

// New constructs a new instance group provisioner of EKS type
func New(instanceGroup *v1alpha1.InstanceGroup, k common.KubernetesClientSet, w awsprovider.AwsWorker) *EksInstanceGroupContext {

	ctx := &EksInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: k,
		AwsWorker:        w,
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)
	return ctx
}

func (ctx *EksInstanceGroupContext) Update() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// make sure our managed role exists if instance group has not provided one
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group role")
	}

	// create new launchconfig if it has drifted
	if ctx.LaunchConfigurationDrifted() {
		err := ctx.CreateLaunchConfiguration()
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	// update scaling group
	err = ctx.UpdateScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to update scaling group")
	}

	instanceGroup.SetState(v1alpha1.ReconcileModified)

	err = ctx.CloudDiscovery()
	if err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) Delete() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		role          = state.GetRole()
		roleARN       = aws.StringValue(role.Arn)
	)

	instanceGroup.SetState(v1alpha1.ReconcileDeleting)
	// delete scaling group
	err := ctx.DeleteScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to delete scaling group")
	}

	// delete launchconfig
	err = ctx.DeleteLaunchConfiguration()
	if err != nil {
		return errors.Wrap(err, "failed to delete launch configuration")
	}

	// delete the managed IAM role if one was created
	err = ctx.DeleteManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to delete scaling group role")
	}

	// remove IAM role from aws-auth configmap
	err = common.RemoveAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN})
	if err != nil {
		return errors.Wrap(err, "failed to remove ARN from aws-auth")
	}

	instanceGroup.SetState(v1alpha1.ReconcileDeleted)

	err = ctx.CloudDiscovery()
	if err != nil {
		return err
	}

	return nil
}

func (ctx *EksInstanceGroupContext) Create() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
	)

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	// no need to create a role if one is already provided
	err := ctx.CreateManagedRole()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group role")
	}

	// create launchconfig
	if !state.HasLaunchConfiguration() {
		err := ctx.CreateLaunchConfiguration()
		if err != nil {
			return errors.Wrap(err, "failed to create launch configuration")
		}
	}

	// create scaling group
	err = ctx.CreateScalingGroup()
	if err != nil {
		return errors.Wrap(err, "failed to create scaling group")
	}

	err = ctx.CloudDiscovery()
	if err != nil {
		return err
	}

	instanceGroup.SetState(v1alpha1.ReconcileModified)
	return nil
}

func (ctx *EksInstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileModified {
		return true
	}
	return false
}

func (ctx *EksInstanceGroupContext) BootstrapNodes() error {
	var (
		state   = ctx.GetDiscoveredState()
		role    = state.GetRole()
		roleARN = aws.StringValue(role.Arn)
	)

	err := common.UpsertAuthConfigMap(ctx.KubernetesClient.Kubernetes, []string{roleARN})
	if err != nil {
		return err
	}
	return nil
}

func (ctx *EksInstanceGroupContext) UpgradeNodes() error {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		strategy      = ctx.GetUpgradeStrategy()
	)

	switch strings.ToLower(strategy.GetType()) {
	case "crd":
		crdStrategy := strategy.GetCRDType()
		if err := crdStrategy.Validate(); err != nil {
			instanceGroup.SetState(v1alpha1.ReconcileErr)
			return errors.Wrap(err, "failed to validate strategy spec")
		}

		err := kubeprovider.ProcessCRDStrategy(ctx.KubernetesClient.KubeDynamic, instanceGroup)
		if err != nil {
			return errors.Wrap(err, "failed to process CRD strategy")
		}
	default:
		return errors.Errorf("'%v' is not an implemented upgrade type, will not process upgrade", strategy.GetType())
	}
	return nil
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
