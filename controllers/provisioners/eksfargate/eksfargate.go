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
	"github.com/aws/aws-sdk-go/service/eks"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	aws "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
	"sort"
	"strings"
)

var log = logrus.New()

const (
	OngoingStateString             = "OngoingState"
	FiniteStateString              = "FiniteState"
	FiniteDeletedString            = "FiniteDeleted"
	UpdateRecoverableErrorString   = "UpdateRecoverableError"
	UnrecoverableErrorString       = "UnrecoverableError"
	UnrecoverableDeleteErrorString = "UnrecoverableDeleteError"
)

func New(instanceGroup *v1alpha1.InstanceGroup, worker *aws.AwsFargateWorker) (*InstanceGroupContext, error) {
	worker.RetryLimit = 15
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
			va := new(string)
			*va = v
			tags[k] = va
		}
	}
	return tags
}

// Convienence function to convert from json to API.
func CreateFargateSelectors(selectors []*v1alpha1.EKSFargateSelectors) []*eks.FargateProfileSelector {
	var eksSelectors []*eks.FargateProfileSelector
	for _, selector := range selectors {
		m := make(map[string]*string)
		for k, v := range selector.Labels {
			vv := new(string)
			*vv = v
			m[k] = vv
		}
		eksSelectors = append(eksSelectors, &eks.FargateProfileSelector{Namespace: selector.Namespace, Labels: m})
	}
	return eksSelectors
}

func (ctx *InstanceGroupContext) Create() error {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	// See if any profiles are being deleted?
	createable, err := ctx.CanCreateAndDelete()
	if err != nil {
		return err
	}
	// Can't create the profile since other profiles are deleting.
	if !createable {
		return nil
	}

	instanceGroup := ctx.GetInstanceGroup()
	var arn *string
	if *instanceGroup.Spec.EKSFargateSpec.GetPodExecutionRoleArn() == "" {
		arn, err = ctx.AwsFargateWorker.CreateDefaultRolePolicy()
		if err != nil {
			log.Errorf("Creation of default role policy failed: %v", err)
			return err
		}
		// Save the role name of the default role we created.
		ctx.GetInstanceGroup().Status.SetFargateRoleName(*ctx.AwsFargateWorker.RoleName)
		log.Infof("Create() - Generated roleName: %s", *ctx.AwsFargateWorker.RoleName)
	} else {
		arn = instanceGroup.Spec.EKSFargateSpec.GetPodExecutionRoleArn()
	}
	log.Infof("Create() - Creating profile with arn: %s", *arn)
	err = ctx.AwsFargateWorker.CreateProfile(arn)
	if err != nil {
		log.Errorf("Creation of the fargate profile for cluster %v and name %v failed: %v",
			instanceGroup.Spec.EKSFargateSpec.GetClusterName(),
			instanceGroup.Spec.EKSFargateSpec.GetProfileName(),
			err)
	} else {
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
	}
	return err
}
func (ctx *InstanceGroupContext) CloudDiscovery() error {
	return nil
}
func (ctx *InstanceGroupContext) Delete() error {
	var err error
	worker := ctx.AwsFargateWorker
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.Status.GetFargateRoleName() != "" {
		err = worker.DeleteDefaultRolePolicy()
		if err != nil {
			log.Errorf("Delete() - Delete of default role policy failed: %v", err)
		}
	}
	err = worker.DeleteProfile()
	if err != nil {
		log.Errorf("Delete() - Delete of profile failed: %v", err)
	} else {
		instanceGroup.SetState(v1alpha1.ReconcileDeleting)
	}

	return err
}
func (ctx *InstanceGroupContext) Update() error {
	// No update is required
	updateNeeded := ctx.HasChanged()
	instanceGroup := ctx.GetInstanceGroup()
	if updateNeeded {
		log.Infof("Update() - initiating a Delete due to needed updates()")
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
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileInit {
		state := ctx.AwsFargateWorker.GetState()

		if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			if state.IsProvisioned() {
				// Role exists and the Profile exists in some form.
				if aws.IsProfileInConditionState(*state.GetProfileState(), OngoingStateString) {
					// stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), FiniteStateString) {
					// stack is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), UpdateRecoverableErrorString) {
					// stack is in update-recoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else {
					// stack is in unrecoverable error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			if state.IsProvisioned() {
				if aws.IsProfileInConditionState(*state.GetProfileState(), OngoingStateString) {
					// deleting stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), FiniteStateString) {
					// deleting stack is in a finite state
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if aws.IsProfileInConditionState(*state.GetProfileState(), UpdateRecoverableErrorString) {
					// deleting stack is in an update recoverable state
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else if aws.IsStackInConditionState(*state.GetProfileState(), FiniteDeletedString) {
					// deleting stack is in a finite-deleted state
					instanceGroup.SetState(v1alpha1.ReconcileDeleted)
				} else if aws.IsStackInConditionState(*state.GetProfileState(), UnrecoverableDeleteErrorString) {
					// deleting stack is in a unrecoverable delete error state
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				} else {
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				instanceGroup.SetState(v1alpha1.ReconcileDeleted)
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

func equalTags(tag1 map[string]*string, tag2 map[string]*string) bool {
	return equalStringMap(tag1, tag2)
}
func equalSubnetSlices(a1 []*string, a2 []*string) bool {
	if len(a1) != len(a2) {
		return false
	}

	oldSubnetsSorted := []string{}
	for _, v := range a1 {
		oldSubnetsSorted = append(oldSubnetsSorted, *v)
	}
	newSubnetsSorted := []string{}
	for _, v := range a2 {
		newSubnetsSorted = append(newSubnetsSorted, *v)
	}
	sort.Strings(oldSubnetsSorted)
	sort.Strings(newSubnetsSorted)
	for i, _ := range newSubnetsSorted {
		if newSubnetsSorted[i] != oldSubnetsSorted[i] {
			return false
		}
	}
	return true
}
func equalStringMap(m1 map[string]*string, m2 map[string]*string) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v1 := range m1 {
		if v2, ok := m2[k]; ok {
			if *v1 != *v2 {
				return false
			}
		} else {
			return false
		}

	}

	return true
}
func equalArns(a1 *string, a2 *string) bool {
	if a1 == nil && a2 == nil {
		return true
	}
	if a1 == nil || a2 == nil {
		return false
	}
	aa1 := strings.Split(*a1, ":")
	aa2 := strings.Split(*a2, ":")
	if len(aa1) != len(aa2) {
		return false
	}
	for i := 0; i < 4; i++ {
		if aa1[i] != aa2[i] {
			return false
		}
	}

	//skip the 4th element

	if aa1[5] != aa2[5] {
		return false
	}
	return true
}
func equalSelector(selector1 *eks.FargateProfileSelector, selector2 *eks.FargateProfileSelector) bool {
	labels1 := selector1.Labels
	labels2 := selector2.Labels

	b := equalStringMap(labels1, labels2)
	if !b {
		return false
	}
	if isEmpty(selector1.Namespace) && !isEmpty(selector2.Namespace) ||
		!isEmpty(selector1.Namespace) && isEmpty(selector2.Namespace) ||
		(selector1.Namespace != nil && selector2.Namespace != nil && *selector1.Namespace != *selector2.Namespace) {
		return false
	}

	return true
}
func equalSelectors(selectors1 []*eks.FargateProfileSelector, selectors2 []*eks.FargateProfileSelector) bool {
	if len(selectors1) != len(selectors2) {
		return false
	}
	var match bool
	for i, _ := range selectors1 {
		match = false
		for j, _ := range selectors2 {
			if equalSelector(selectors1[i], selectors2[j]) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}
func isEmpty(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}
func nilOrValue(s *string) string {
	if s == nil {
		return ""
	} else {
		return *s
	}
}

func (ctx *InstanceGroupContext) HasChanged() bool {
	log.Info("HasChanged - Checking for changes.")
	state := ctx.AwsFargateWorker.GetState()
	if !equalTags(state.Profile.Tags, CreateFargateTags(ctx.GetInstanceGroup().Spec.EKSFargateSpec.Tags)) {
		log.Info("HasChanged - Tags have changed.")
		return true
	}
	if !equalArns(state.Profile.PodExecutionRoleArn, ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetPodExecutionRoleArn()) {
		if strings.TrimSpace(*ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetPodExecutionRoleArn()) != "" ||
			state.Profile.PodExecutionRoleArn == nil ||
			!strings.HasSuffix(*state.Profile.PodExecutionRoleArn, *ctx.AwsFargateWorker.CreateDefaultRoleName()) {
			log.Infof("HasChanged - Arns have changed. old %v and new %v",
				nilOrValue(state.Profile.PodExecutionRoleArn),
				nilOrValue(ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetPodExecutionRoleArn()))
			return true
		}
	}
	if !equalSubnetSlices(ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetSubnets(), state.Profile.Subnets) {
		log.Infof("HasChanged - Subnets have changed. old %v and new %v",
			state.Profile.Subnets,
			ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetSubnets())
		return true
	}
	if !equalSelectors(CreateFargateSelectors(ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetSelectors()), state.Profile.Selectors) {
		log.Infof("HasChanged - Selectors have changed. old %v and new %v",
			state.Profile.Selectors,
			ctx.GetInstanceGroup().Spec.EKSFargateSpec.GetSelectors())
		return true
	}
	log.Info("HasChanged - No changes detected")
	return false
}

func (ctx *InstanceGroupContext) CanCreateAndDelete() (bool, error) {
	var profileNames []*string
	var err error
	var profiles []*eks.FargateProfile

	profileNames, err = ctx.AwsFargateWorker.ListAllProfiles()
	if err == nil {
		profiles, err = ctx.AwsFargateWorker.DescribeAllProfiles(profileNames)
		if err == nil && !aws.IsDeleting(profiles) {
			return true, nil
		}
	}
	return false, err
}
