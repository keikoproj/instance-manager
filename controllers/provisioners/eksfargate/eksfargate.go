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
	"errors"
	"github.com/aws/aws-sdk-go/service/eks"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	aws "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

const LastAppliedConfigurationKey = "kubectl.kubernetes.io/last-applied-configuration"

const (
	PendingRolePolicyAttach = "pendingPolicyCreation"
	PendingRoleCreation     = "pendingRoleCreation"
)

const (
	OngoingStateString             = "OngoingState"
	FiniteStateString              = "FiniteState"
	FiniteDeletedString            = "FiniteDeleted"
	UpdateRecoverableErrorString   = "UpdateRecoverableError"
	UnrecoverableErrorString       = "UnrecoverableError"
	UnrecoverableDeleteErrorString = "UnrecoverableDeleteError"
)

func New(instanceGroup *v1alpha1.InstanceGroup, worker aws.AwsWorker) (InstanceGroupContext, error) {
	ctx := InstanceGroupContext{
		InstanceGroup: instanceGroup,
		AwsWorker:     worker,
	}
	instanceGroup.SetState(v1alpha1.ReconcileInit)
	return ctx, nil
}
func CreateFargateSubnets(subnets []string) []*string {
	stringReferences := []*string{}
	for _, s := range subnets {
		temp := new(string)
		*temp = s
		stringReferences = append(stringReferences, temp)
	}
	return stringReferences
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
func CreateFargateSelectors(selectors []v1alpha1.EKSFargateSelectors) []*eks.FargateProfileSelector {
	var eksSelectors []*eks.FargateProfileSelector
	for _, selector := range selectors {
		m := make(map[string]*string)
		for k, v := range selector.Labels {
			vv := new(string)
			*vv = v
			m[k] = vv
		}
		eksSelectors = append(eksSelectors, &eks.FargateProfileSelector{Namespace: &selector.Namespace, Labels: m})
	}
	return eksSelectors
}

func (ctx *InstanceGroupContext) Create() error {
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	var arn string
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	if instanceGroup.GetEKSFargateSpec().GetPodExecutionRoleArn() == "" {
		commonInput := aws.CreateCommonInput{
			ClusterName: spec.GetClusterName(),
			ProfileName: spec.GetProfileName(),
		}
		created, err := ctx.AwsWorker.CreateDefaultRole(commonInput)
		if created || err != nil {
			log.Infof("Creating default role: %v", err)
			return err
		}
		role, err := ctx.AwsWorker.GetDefaultRole(commonInput)
		if err != nil {
			log.Errorf("Failed to get default role: %v", err)
			return err
		}
		arn = *role.Arn

		err = ctx.AwsWorker.AttachDefaultPolicyToDefaultRole(commonInput)
		if err != nil {
			log.Errorf("Failed to get attach policy to role: %v", err)
			return err
		}
		log.Info("Attached default policy to role")

	} else {
		arn = spec.GetPodExecutionRoleArn()
	}

	log.Infof("Creating a profile with %s", arn)
	createProfileInput := aws.CreateProfileInput{
		ClusterName: spec.GetClusterName(),
		ProfileName: spec.GetProfileName(),
		Arn:         arn,
		Selectors:   CreateFargateSelectors(spec.GetSelectors()),
		Tags:        CreateFargateTags(spec.GetTags()),
		Subnets:     CreateFargateSubnets(spec.GetSubnets()),
	}
	err := ctx.AwsWorker.CreateProfile(createProfileInput)
	if err != nil {
		log.Errorf("Creation of the fargate profile for cluster %v and name %v failed: %v",
			spec.GetClusterName(),
			spec.GetProfileName(),
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
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	// See if any profiles are being deleted?
	deleteable, err := ctx.CanDelete()
	if err != nil {
		return err
	}
	// Can't create the profile since other profiles are deleting.
	// No state set, so will reenter
	if !deleteable {
		log.Info("Delete delayed. Other profiles being deleted")
		return nil
	}
	worker := ctx.AwsWorker
	commonInput := aws.CreateCommonInput{
		ClusterName: spec.GetClusterName(),
		ProfileName: spec.GetProfileName(),
	}
	if spec.GetPodExecutionRoleArn() == "" {
		found, err := worker.DetachDefaultPolicyFromDefaultRole(commonInput)
		if found || err != nil {
			log.Infof("Detaching the default policy: %v", err)
			return err
		}

		found, err = ctx.AwsWorker.DeleteDefaultRole(commonInput)
		if found || err != nil {
			log.Infof("Deleting the default role: %v", err)
			return err
		}
	}
	log.Info("Deleting the profile")
	err = worker.DeleteProfile(commonInput)
	if err != nil {
		log.Errorf("Deletion of the fargate profile for cluster %v and name %v failed: %v",
			commonInput.ClusterName,
			commonInput.ProfileName,
			err)
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileDeleting)

	return err
}

func (ctx *InstanceGroupContext) Update() error {
	// No update is required
	log.Info("Running update")
	instanceGroup := ctx.GetInstanceGroup()
	annos := instanceGroup.GetObjectMeta().GetAnnotations()
	// If there is a last-applied-configuration then assume
	// this is an update and throw an exception
	if _, ok := annos[LastAppliedConfigurationKey]; ok {
		return errors.New("update not supported")
	}
	instanceGroup.SetState(v1alpha1.ReconcileModified)
	return nil
}
func (ctx *InstanceGroupContext) UpgradeNodes() error {
	return errors.New("upgrade not supported")
}
func (ctx *InstanceGroupContext) BootstrapNodes() error {
	return nil
}
func (ctx *InstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileModified || instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
		return true
	}
	return false
}
func (ctx *InstanceGroupContext) IsUpgradeNeeded() bool {
	return false
}
func (ctx *InstanceGroupContext) StateDiscovery() {
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	if instanceGroup.GetState() == v1alpha1.ReconcileInit {
		commonInput := aws.CreateCommonInput{
			ClusterName: spec.GetClusterName(),
			ProfileName: spec.GetProfileName(),
		}
		state := ctx.AwsWorker.GetState(commonInput)

		if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			if state.IsProvisioned() {
				// Role exists and the Profile exists in some form (creating)
				if aws.IsProfileInConditionState(state.GetProfileState(), OngoingStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
					// Role exists and the Profile exists (active)
				} else if aws.IsProfileInConditionState(state.GetProfileState(), FiniteStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if aws.IsProfileInConditionState(state.GetProfileState(), UpdateRecoverableErrorString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else {
					// Profile already exists so return an error
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			if state.IsProvisioned() {
				if aws.IsProfileInConditionState(state.GetProfileState(), OngoingStateString) {
					// deleting stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if aws.IsProfileInConditionState(state.GetProfileState(), FiniteStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
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

func (ctx *InstanceGroupContext) CanDelete() (bool, error) {
	var profileNames []string
	var err error
	var profiles []eks.FargateProfile

	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	commonInput := aws.CreateCommonInput{
		ClusterName: spec.GetClusterName(),
		ProfileName: spec.GetProfileName(),
	}
	profileNames, err = ctx.AwsWorker.ListAllProfiles(commonInput)
	if err == nil {
		profiles, err = ctx.AwsWorker.DescribeAllProfiles(commonInput, profileNames)
		if err == nil && !IsDeleting(profiles) {
			return true, nil
		}
	}
	return false, err
}
func IsDeleting(fargateProfiles []eks.FargateProfile) bool {
	for _, profile := range fargateProfiles {
		if *profile.Status == eks.FargateProfileStatusDeleting {
			return true
		}
	}
	return false
}
