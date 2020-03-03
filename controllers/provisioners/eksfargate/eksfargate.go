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
	"github.com/aws/aws-sdk-go/service/eks"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	log "github.com/sirupsen/logrus"
)

func New(instanceGroup *v1alpha1.InstanceGroup) (*EksFargateInstanceGroupContext, error) {
	ctx := EksFargateInstanceGroupContext{
		InstanceGroup: instanceGroup,
	}
	return &ctx, nil
}

func createFargateSelectors(selectors []*v1alpha1.EKSFargateSelectors) []*eks.FargateProfileSelector {
	var eksSelectors []*eks.FargateProfileSelector
	for _, selector := range selectors {
		m := make(map[string]*string)
		for k, v := range selector.Labels {
			m[k] = &v
		}
		eksSelectors = append(eksSelectors, &eks.FargateProfileSelector{Namespace: selector.Namespace, Labels: m})
	}
	return eksSelectors
}

func (ctx *EksFargateInstanceGroupContext) HandleRequest() error {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	spec := ctx.GetInstanceGroup().Spec.EKSFargateSpec
	worker := awsprovider.AwsFargateWorker{
		ClusterName:  spec.GetClusterName(),
		ProfileName:  spec.GetProfileName(),
		ExecutionArn: spec.GetPodExecutionRoleArn(),
		Selectors:    createFargateSelectors(spec.GetSelectors()),
	}

	ig := ctx.GetInstanceGroup()
	log.Infof("Fargate state: %s", ig.GetState())
	log.Infof("Fargate DeletionTimestamp: %v \n", ig.ObjectMeta.DeletionTimestamp)

	fargateProfile, fargateErr := worker.Describe()
	if fargateErr != nil {
		log.Infof("Fargate cluster: %s and profile: %s not found\n", *worker.ClusterName, *worker.ProfileName)
	} else {
		log.Infof("Fargate cluster: %s and profile: %s has status %s\n", *worker.ClusterName, *worker.ProfileName, *fargateProfile.Status)
	}
	// first call
	if ig.GetState() == "" {
		var err error = nil
		// cluster and profile do not exist
		if fargateErr != nil {
			//Not a delete
			if ig.ObjectMeta.DeletionTimestamp.IsZero() {
				ig.SetState(v1alpha1.ReconcileInit)
			} else {
				ig.SetState(v1alpha1.ReconcileErr)
			}
		} else {
			ig.SetState(v1alpha1.ReconcileErr)
		}
		return err
	}

	if ig.GetState() == v1alpha1.ReconcileInitCreate {
		var err error = nil
		if *fargateProfile.Status == "ACTIVE" {
			ig.SetState(v1alpha1.ReconcileReady)
		}
		return err
	}
	if ig.GetState() == v1alpha1.ReconcileDeleting {
		var err error = nil
		if fargateErr != nil {
			ig.SetState(v1alpha1.ReconcileDeleted)
		}
		return err
	}
	if ig.GetState() == v1alpha1.ReconcileInit {
		var err error = nil
		// Create request
		if ig.ObjectMeta.DeletionTimestamp.IsZero() {
			// profile does not exist
			log.Infof("Fargate creating cluster: %s and profile: %s \n", *worker.ClusterName, *worker.ProfileName)
			err = worker.Create()
			if err != nil {
				ig.SetState(v1alpha1.ReconcileErr)
			} else {
				ig.SetState(v1alpha1.ReconcileInitCreate)
			}
		}
		return err
	}

	if ig.GetState() == v1alpha1.ReconcileReady {
		var err error = nil
		// Delete request
		if !ig.ObjectMeta.DeletionTimestamp.IsZero() {
			err = worker.Delete()
			if err != nil {
				ig.SetState(v1alpha1.ReconcileErr)
			} else {
				ig.SetState(v1alpha1.ReconcileDeleting)
			}
		}
		return err
	}

	return nil
}
