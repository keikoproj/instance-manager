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

package controllers

import (
	"context"
	"os"
	"strings"

	v1alpha "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks"
	log "github.com/sirupsen/logrus"
)

func (r *InstanceGroupReconciler) ReconcileEKS(instanceGroup *v1alpha.InstanceGroup, finalizerName string) error {
	log.Infof("upgrade strategy: %v", strings.ToLower(instanceGroup.Spec.AwsUpgradeStrategy.Type))

	client, err := common.GetKubernetesClient()
	if err != nil {
		return err
	}

	dynClient, err := common.GetKubernetesDynamicClient()
	if err != nil {
		return err
	}

	awsRegion, err := aws.GetRegion()
	if err != nil {
		return err
	}

	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	if _, err := os.Stat(r.ControllerConfPath); os.IsNotExist(err) {
		log.Errorf("controller config file not found: %v", err)
		return err
	}

	controllerConfig, err := common.ReadFile(r.ControllerConfPath)
	if err != nil {
		return err
	}

	_, err = eks.LoadControllerConfiguration(instanceGroup, controllerConfig)
	if err != nil {
		log.Errorf("failed to load controller configuration: %v", err)
		return err
	}

	awsWorker := aws.AwsWorker{
		AsgClient: aws.GetAwsAsgClient(awsRegion),
		EksClient: aws.GetAwsEksClient(awsRegion),
	}

	ctx := eks.New(instanceGroup, kube, awsWorker)
	ctx.ControllerRegion = awsRegion

	err = HandleReconcileRequest(ctx)
	if err != nil {
		ctx.SetState(v1alpha.ReconcileErr)
		r.Update(context.Background(), ctx.GetInstanceGroup())
		return err
	}
	// Remove finalizer if deleted
	r.Finalize(instanceGroup, finalizerName)

	// Update resource with changes
	err = r.Update(context.Background(), ctx.GetInstanceGroup())
	if err != nil {
		return err
	}
	return nil
}
