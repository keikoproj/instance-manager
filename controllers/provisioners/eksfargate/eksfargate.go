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

func hash(instancegroup_state string, profile_status string) string {
	return instancegroup_state + ":" + profile_status
}

type transition func(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error
type stateMachine map[string]transition

var sm = stateMachine{
	// IG state    Profile State
	hash("", "NONE"):     t1,
	hash("", "ACTIVE"):   e1,
	hash("", "CREATING"): e1,
	hash("", "DELETING"): e1,
	hash(string(v1alpha1.ReconcileInitCreate), "ACTIVE"): t2,
	hash(string(v1alpha1.ReconcileDeleting), "NONE"):     t3,
	hash(string(v1alpha1.ReconcileInit), "NONE"):         t4,
	hash(string(v1alpha1.ReconcileReady), "ACTIVE"):      t5,
}

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

func e1(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner e1\n")
	ig.SetState(v1alpha1.ReconcileErr)
	return nil
}
func t1(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner t1\n")
	var err error = nil
	// cluster and profile do not exist
	//Not a delete
	if ig.ObjectMeta.DeletionTimestamp.IsZero() {
		ig.SetState(v1alpha1.ReconcileInit)
	} else {
		ig.SetState(v1alpha1.ReconcileErr)
	}
	return err
}
func t2(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner t2\n")
	ig.SetState(v1alpha1.ReconcileReady)
	return nil
}
func t3(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner t3\n")
	ig.SetState(v1alpha1.ReconcileDeleted)
	return nil
}
func t4(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner t4\n")
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
func t5(ig *v1alpha1.InstanceGroup, worker awsprovider.AwsFargateWorker) error {
	log.Info("Running transistioner t5\n")
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

	fargateProfile, fargateErr := worker.Describe()

	var profileStatus = "NONE"
	if fargateErr != nil {
		log.Infof("Fargate cluster: %s and profile: %s not found\n", *worker.ClusterName, *worker.ProfileName)
	} else {
		log.Infof("Fargate cluster: %s and profile: %s status %s\n", *worker.ClusterName, *worker.ProfileName, *fargateProfile.Status)
		profileStatus = *fargateProfile.Status
	}

	log.Infof("Fargate DeletionTimestamp: %v \n", ig.ObjectMeta.DeletionTimestamp)
	log.Infof("Fargate instance group state: %s", ig.GetState())
	log.Infof("Fargate profile state: %s", profileStatus)

	if transitioner, ok := sm[hash(string(ig.GetState()), profileStatus)]; ok {
		return transitioner(ig, worker)
	}
	return nil

}
