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
	"github.com/keikoproj/instance-manager/controllers/provisioners"
)

const ProvisionerName = "eks-fargate"

const LastAppliedConfigurationKey = "kubectl.kubernetes.io/last-applied-configuration"

const (
	PendingRolePolicyAttach = "pendingPolicyCreation"
	PendingRoleCreation     = "pendingRoleCreation"
)

var (
	NonRetryableStates = []v1alpha1.ReconcileState{v1alpha1.ReconcileErr, v1alpha1.ReconcileReady, v1alpha1.ReconcileDeleted}
)

const (
	OngoingStateString             = "OngoingState"
	FiniteStateString              = "FiniteState"
	FiniteDeletedString            = "FiniteDeleted"
	UpdateRecoverableErrorString   = "UpdateRecoverableError"
	UnrecoverableErrorString       = "UnrecoverableError"
	UnrecoverableDeleteErrorString = "UnrecoverableDeleteError"
)

func New(p provisioners.ProvisionerInput) *InstanceGroupContext {
	ctx := &InstanceGroupContext{
		InstanceGroup: p.InstanceGroup,
		AwsWorker:     p.AwsWorker,
		Log:           p.Log.WithName("eks-fargate"),
	}

	instanceGroup := ctx.GetInstanceGroup()

	instanceGroup.SetState(v1alpha1.ReconcileInit)
	ctx.processParameters()

	return ctx
}

func (ctx *InstanceGroupContext) processParameters() {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSFargateSpec()
		params        = make(map[string]interface{})
	)

	params["ClusterName"] = spec.GetClusterName()
	params["ProfileName"] = spec.GetProfileName()
	params["Selectors"] = CreateFargateSelectors(spec.GetSelectors())
	params["Subnets"] = CreateFargateSubnets(spec.GetSubnets())
	params["Tags"] = CreateFargateTags(spec.GetTags())
	ctx.AwsWorker.Parameters = params
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
func IsRetryable(instanceGroup *v1alpha1.InstanceGroup) bool {
	for _, state := range NonRetryableStates {
		if state == instanceGroup.GetState() {
			return false
		}
	}
	return true
}
func (ctx *InstanceGroupContext) Create() error {
	var arn string
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	if instanceGroup.GetEKSFargateSpec().GetPodExecutionRoleArn() == "" {
		created, err := ctx.AwsWorker.CreateDefaultRole()
		if created || err != nil {
			ctx.Log.Info("Creating default role")
			return err
		}
		role, err := ctx.AwsWorker.GetDefaultRole()
		if err != nil {
			ctx.Log.Info("Failed to get default role", "error", err)
			return err
		}
		arn = *role.Arn

		err = ctx.AwsWorker.AttachDefaultPolicyToDefaultRole()
		if err != nil {
			ctx.Log.Info("Failed to get attach policy to role", "error", err)
			return err
		}
		ctx.Log.Info("Attached default policy to role")

	} else {
		arn = spec.GetPodExecutionRoleArn()
	}

	ctx.Log.Info("Creating a profile.", "arn", arn)
	tryAgain, err := ctx.AwsWorker.CreateProfile(arn)
	if tryAgain {
		ctx.Log.Info("Resource inuse on Create.")
		return nil
	}
	if err != nil {
		ctx.Log.Info("Creation of the fargate profile failed", "cluster",
			spec.GetClusterName(),
			"profile",
			spec.GetProfileName(),
			"error", err)
	} else {
		instanceGroup.SetState(v1alpha1.ReconcileModifying)
	}

	return err
}
func (ctx *InstanceGroupContext) CloudDiscovery() error {
	profile, err := ctx.AwsWorker.DescribeProfile()
	if err != nil {
		profile = &eks.FargateProfile{
			Status: nil,
		}
	}
	if profile.Status == nil {
		ctx.DiscoveredState.ProfileStatus = aws.FargateProfileStatusMissing
	} else {
		ctx.DiscoveredState.ProfileStatus = *profile.Status
	}
	return nil
}
func (ctx *InstanceGroupContext) Delete() error {
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()

	worker := ctx.AwsWorker
	if spec.GetPodExecutionRoleArn() == "" {
		found, err := worker.DetachDefaultPolicyFromDefaultRole()
		if found || err != nil {
			ctx.Log.Info("Detaching the default policy", "error", err)
			return err
		}

		found, err = ctx.AwsWorker.DeleteDefaultRole()
		if found || err != nil {
			ctx.Log.Info("Deleting the default role", "error", err)
			return err
		}
	}
	ctx.Log.Info("Deleting the profile")
	tryAgain, err := worker.DeleteProfile()
	if tryAgain {
		ctx.Log.Info("Resource inuse on Delete.")
		return nil
	}
	if err != nil {
		ctx.Log.Info("Deletion of the fargate profile.", "cluster",
			spec.GetClusterName(),
			"profile",
			spec.GetProfileName(),
			"error", err)
		return err
	}
	instanceGroup.SetState(v1alpha1.ReconcileDeleting)

	return err
}

func (ctx *InstanceGroupContext) Update() error {
	// No update is required
	ctx.Log.Info("Running update")
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
	if instanceGroup.GetState() == v1alpha1.ReconcileInit {

		if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			if ctx.GetDiscoveredState().IsProvisioned() {
				// Role exists and the Profile exists in some form (creating)
				if aws.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), OngoingStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
					// Role exists and the Profile exists (active)
				} else if aws.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), FiniteStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if aws.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), UpdateRecoverableErrorString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
				} else {
					// Profile already exists so return an error
					instanceGroup.SetState(v1alpha1.ReconcileErr)
				}
			} else {
				instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
			}
		} else {
			if ctx.GetDiscoveredState().IsProvisioned() {
				if aws.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), OngoingStateString) {
					// deleting stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if aws.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), FiniteStateString) {
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
