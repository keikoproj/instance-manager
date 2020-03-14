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
	aws "github.com/keikoproj/instance-manager/controllers/providers/aws"
	provisioners "github.com/keikoproj/instance-manager/controllers/provisioners"
	log "github.com/sirupsen/logrus"
)

func New(instanceGroup *v1alpha1.InstanceGroup, worker *aws.AwsFargateWorker) (*InstanceGroupContext, error) {
	ctx := InstanceGroupContext{
		InstanceGroup:    instanceGroup,
		AwsFargateWorker: worker,
	}
	instanceGroup.SetState(v1alpha1.ReconcileInit)
	return &ctx, nil
}
func CreateFargateTags(tagArray []map[string]string) map[string]*string {
	tags := make(map[string]*string)
	for _, t := range tagArray {
		for k, v := range t {
			tags[k] = &v
		}
	}
	return tags
}
func CreateFargateSelectors(selectors []*v1alpha1.EKSFargateSelectors) []*eks.FargateProfileSelector {
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

func (ctx *InstanceGroupContext) Create() error {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	//ig := ctx.GetInstanceGroup()
	worker := ctx.AwsFargateWorker

	log.Infof("Fargate cluster: %s and profile: %s not found\n", *worker.ClusterName, *worker.ProfileName)
	ig := ctx.GetInstanceGroup()
	var err error
	var arn *string
	if *ig.Spec.EKSFargateSpec.GetPodExecutionRoleArn() == "" {
		arn, err = ctx.AwsFargateWorker.CreateDefaultRolePolicy()
		if err != nil {
			log.Errorf("Creation of default role policy failed: %v", err)
			return err
		}
		// Save the role name of the default role we created.
		ctx.GetInstanceGroup().Status.SetFargateRoleName(*ctx.AwsFargateWorker.RoleName)
	} else {
		arn = ig.Spec.EKSFargateSpec.GetPodExecutionRoleArn()
	}
	log.Infof("Create() - Creating profile with arn: %s", *arn)
	err = ctx.AwsFargateWorker.CreateProfile(arn)
	if err != nil {
		log.Errorf("Creation of the fargate profile failed: %v", err)
	}
	return err
}
func (ctx *InstanceGroupContext) CloudDiscovery() error {
	return nil
}
func (ctx *InstanceGroupContext) Delete() error {
	var err error
	worker := ctx.AwsFargateWorker
	ig := ctx.GetInstanceGroup()
	log.Infof("Delete() PodExecutionRoleArn: <%v>", *ig.Spec.EKSFargateSpec.GetPodExecutionRoleArn())
	log.Infof("Delete() RoleName: <%v>", ig.Status.GetFargateRoleName())
	if *ig.Spec.EKSFargateSpec.GetPodExecutionRoleArn() == "" {
		err = worker.DeleteDefaultRolePolicy()
		if err != nil {
			log.Errorf("Delete() - Delete of default role policy failed: %v", err)
		}
	}
	err = worker.DeleteProfile()
	if err != nil {
		log.Errorf("Delete() - Delete of profile failed: %v", err)
	}

	return err
}
func (ctx *InstanceGroupContext) Update() error {
	// No update is required
	updateNeeded, err := ctx.HasChanged()
	if err != nil {
		return err
	}
	log.Infof("Update - Tags have changed: %v", updateNeeded)
	instanceGroup := ctx.GetInstanceGroup()
	if updateNeeded {
		log.Infof("Update() - initiating a Delete()")
		ctx.Delete()
	} else {
		instanceGroup.SetState(v1alpha1.ReconcileModified)
	}
	return nil
}
func (ctx *InstanceGroupContext) UpgradeNodes() error {
	return nil
}
func (ctx *InstanceGroupContext) BootstrapNodes() error {
	return nil
}
func (ctx *InstanceGroupContext) IsReady() bool {
	state := ctx.AwsFargateWorker.GetState()
	return *state.GetProfileState() == "ACTIVE"
}
func (ctx *InstanceGroupContext) IsUpgradeNeeded() bool {
	return false
}
func (ctx *InstanceGroupContext) StateDiscovery() {
	ig := ctx.GetInstanceGroup()
	if ig.GetState() == v1alpha1.ReconcileInit {
		state := ctx.AwsFargateWorker.GetState()

		if ig.ObjectMeta.DeletionTimestamp.IsZero() {
			if state.IsProvisioned() {
				// Role exists and the Profile exists in some form.
				if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.OngoingStateString) {
					// stack is in an ongoing state
					ig.SetState(v1alpha1.ReconcileModifying)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.FiniteStateString) {
					// stack is in a finite state
					ig.SetState(v1alpha1.ReconcileInitUpdate)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.UpdateRecoverableErrorString) {
					// stack is in update-recoverable error state
					ig.SetState(v1alpha1.ReconcileInitUpdate)
				} else {
					// stack is in unrecoverable error state
					ig.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				ig.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			if state.IsProvisioned() {
				if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.OngoingStateString) {
					// deleting stack is in an ongoing state
					ig.SetState(v1alpha1.ReconcileDeleting)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.FiniteStateString) {
					// deleting stack is in a finite state
					ig.SetState(v1alpha1.ReconcileInitDelete)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), provisioners.UpdateRecoverableErrorString) {
					// deleting stack is in an update recoverable state
					ig.SetState(v1alpha1.ReconcileInitDelete)
				} else if aws.IsStackInConditionState(*state.GetProfileState(), provisioners.FiniteDeletedString) {
					// deleting stack is in a finite-deleted state
					ig.SetState(v1alpha1.ReconcileDeleted)
				} else if aws.IsStackInConditionState(*state.GetProfileState(), provisioners.UnrecoverableDeleteErrorString) {
					// deleting stack is in a unrecoverable delete error state
					ig.SetState(v1alpha1.ReconcileErr)
				} else if aws.IsStackInConditionState(*state.GetProfileState(), provisioners.UnrecoverableErrorString) {
					// deleting stack is in a unrecoverable error state - allow it to delete
					ig.SetState(v1alpha1.ReconcileInitDelete)
				}
			} else {
				ig.SetState(v1alpha1.ReconcileDeleted)
			}
		}
	}
}
func (ctx *InstanceGroupContext) SetState(state v1alpha1.ReconcileState) {
	ctx.GetInstanceGroup().SetState(state)
}
func (ctx *InstanceGroupContext) GetState() v1alpha1.ReconcileState {
	return ctx.GetInstanceGroup().GetState()
}

func (ctx *InstanceGroupContext) HasChanged() (bool, error) {
	state := ctx.AwsFargateWorker.GetState()
	fpTagsOld := state.Profile.Tags
	fpTagsNew := CreateFargateTags(ctx.GetInstanceGroup().Spec.EKSFargateSpec.Tags)
	if len(fpTagsOld) != len(fpTagsNew) {
		return true, nil
	}
	for k, v := range fpTagsNew {
		if value, ok := fpTagsOld[k]; ok {
			if *v != *value {
				return true, nil
			}
		} else {
			return true, nil
		}
	}
	return false, nil
}
