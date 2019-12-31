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

package eksfargate

import (
	//	"github.com/aws/aws-sdk-go/aws"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
)

var (
	log = logrus.New()
)

// New constructs a new instance group provisioner of EKS Cloudformation type
func New(instanceGroup *v1alpha1.InstanceGroup, k common.KubernetesClientSet, w awsprovider.AwsWorker) (EksFargateInstanceGroupContext, error) {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	var specConfig = &instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration

	vpcID, err := w.DeriveEksVpcID(specConfig.GetClusterName())
	if err != nil {
		return EksFargateInstanceGroupContext{}, err
	}

	ctx := EksFargateInstanceGroupContext{
		InstanceGroup:    instanceGroup,
		KubernetesClient: k,
		VpcID:            vpcID,
	}

	instanceGroup.SetState(v1alpha1.ReconcileInit)

	err = ctx.processParameters()
	if err != nil {
		log.Errorf("failed to parse cloudformation parameters: %v", err)
		return EksFargateInstanceGroupContext{}, err
	}

	return ctx, nil
}

func (ctx *EksFargateInstanceGroupContext) Create() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) Update() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) UpgradeNodes() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) Delete() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) IsProvisioned() bool {
	return false
}

func (ctx *EksFargateInstanceGroupContext) IsReady() bool {
	return false
}

func (ctx *EksFargateInstanceGroupContext) IsUpgradeNeeded() bool {
	return false
}

func (ctx *EksFargateInstanceGroupContext) StateDiscovery() {
}

func (ctx *EksFargateInstanceGroupContext) discoverInstanceGroups() {
}

func (ctx *EksFargateInstanceGroupContext) CloudDiscovery() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) BootstrapNodes() error {
	return nil
}

func (ctx *EksFargateInstanceGroupContext) processParameters() error {
	//instanceGroup := ctx.GetInstanceGroup()
	//spec := &instanceGroup.Spec

	return nil
}
