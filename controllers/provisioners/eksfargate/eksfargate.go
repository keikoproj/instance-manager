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
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/pkg/errors"
)

const ProvisionerName = "eks-fargate"

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

func hash(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	i := h.Sum64()
	return strconv.FormatUint(i, 10)
}

func becauseErrorContains(err error, code string) bool {
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == code {
			return true
		}
	}
	return false
}

func New(p provisioners.ProvisionerInput) *FargateInstanceGroupContext {
	ctx := &FargateInstanceGroupContext{
		InstanceGroup: p.InstanceGroup,
		AwsWorker:     p.AwsWorker,
		Log:           p.Log.WithName("eks-fargate"),
	}

	instanceGroup := ctx.GetInstanceGroup()

	instanceGroup.SetState(v1alpha1.ReconcileInit)
	return ctx
}
func (ctx *FargateInstanceGroupContext) generateUniqueName() string {
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	name := fmt.Sprintf("%v-%v-%v", spec.GetClusterName(), instanceGroup.GetNamespace(), instanceGroup.GetName())
	return fmt.Sprintf("fargate-%v-%v", spec.GetClusterName(), hash(name))

}

func (ctx *FargateInstanceGroupContext) processParameters() {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		spec          = instanceGroup.GetEKSFargateSpec()
		params        = make(map[string]interface{})
	)

	params["ClusterName"] = spec.GetClusterName()
	params["ProfileName"] = ctx.generateUniqueName()
	params["Selectors"] = CreateFargateSelectors(spec.GetSelectors())
	params["Subnets"] = spec.GetSubnets()
	params["Tags"] = CreateFargateTags(spec.GetTags())
	params["DefaultRoleName"] = fmt.Sprintf("%v-role", ctx.generateUniqueName())
	ctx.AwsWorker.Parameters = params
}

func CreateFargateSelectors(selectors []v1alpha1.EKSFargateSelectors) []*eks.FargateProfileSelector {
	var eksSelectors []*eks.FargateProfileSelector
	for _, selector := range selectors {
		m := make(map[string]*string)
		for k, v := range selector.Labels {
			vv := new(string)
			*vv = v
			m[k] = vv
		}
		eksSelectors = append(eksSelectors,
			&eks.FargateProfileSelector{
				Namespace: &selector.Namespace,
				Labels:    m})
	}
	return eksSelectors
}
func CreateFargateTags(tagArray []map[string]string) map[string]*string {
	tags := make(map[string]*string)
	for _, t := range tagArray {
		for k, v := range t {
			vv := new(string)
			*vv = v
			tags[k] = vv
		}
	}
	return tags
}

func (ctx *FargateInstanceGroupContext) Create() error {
	var arn string
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()
	if instanceGroup.GetEKSFargateSpec().GetPodExecutionRoleArn() == "" {
		err := ctx.AwsWorker.CreateDefaultFargateRole()
		if err == nil {
			ctx.Log.Info("Created default role",
				"instancegroup",
				instanceGroup.NamespacedName())
			return nil
		}
		if !becauseErrorContains(err, iam.ErrCodeEntityAlreadyExistsException) {
			ctx.Log.Error(err,
				"Creation of the default role failed.",
				"instancegroup",
				instanceGroup.NamespacedName())
			return err
		}

		role, err := ctx.AwsWorker.GetDefaultFargateRole()
		if err != nil {
			ctx.Log.Error(err,
				"Failed to find the default role",
				"instancegroup",
				instanceGroup.NamespacedName())
			return err
		}
		arn = *role.Arn

		err = ctx.AwsWorker.AttachDefaultPolicyToDefaultRole()
		if err != nil {
			ctx.Log.Error(err,
				"Failed to attach the default policy to role",
				"instancegroup",
				instanceGroup.NamespacedName())
			return err
		}
		ctx.Log.Info("Attached default policy to role",
			"instancegroup",
			instanceGroup.NamespacedName())

	} else {
		arn = spec.GetPodExecutionRoleArn()
	}

	err := ctx.AwsWorker.CreateFargateProfile(arn)
	if err != nil {

		if becauseErrorContains(err, eks.ErrCodeResourceInUseException) {
			ctx.Log.Info("creation of the fargate profile delayed.",
				"instancegroup",
				instanceGroup.NamespacedName(),
				"cluster",
				spec.GetClusterName(),
				"profile",
				ctx.generateUniqueName(),
				"error", err)
			return nil
		}

		return errors.Wrapf(err, "creation of the fargate profile %v failed", ctx.generateUniqueName())
	}

	ctx.Log.Info("Fargate profile creation started.",
		"instancegroup",
		instanceGroup.NamespacedName(),
		"cluster",
		spec.GetClusterName(),
		"profile",
		ctx.generateUniqueName())

	instanceGroup.SetState(v1alpha1.ReconcileModifying)

	return nil
}
func (ctx *FargateInstanceGroupContext) CloudDiscovery() error {
	ctx.processParameters()

	profile, err := ctx.AwsWorker.DescribeFargateProfile()
	if err != nil {
		profile = &eks.FargateProfile{
			Status: nil,
		}
	}

	if profile.Status == nil {
		ctx.DiscoveredState.ProfileStatus = aws.StringValue(nil)
	} else {
		ctx.DiscoveredState.ProfileStatus = *profile.Status
	}
	return nil
}
func (ctx *FargateInstanceGroupContext) Delete() error {
	instanceGroup := ctx.GetInstanceGroup()
	spec := instanceGroup.GetEKSFargateSpec()

	worker := ctx.AwsWorker
	if spec.GetPodExecutionRoleArn() == "" {
		err := worker.DetachDefaultPolicyFromDefaultRole()
		// Policy was detached
		if err == nil {
			// Role was detached, return and get requeued.
			ctx.Log.Info("Detached default policy.",
				"instancegroup",
				instanceGroup.NamespacedName())
			return nil
		}
		if !becauseErrorContains(err, iam.ErrCodeNoSuchEntityException) {
			ctx.Log.Error(err,
				"Detaching the default policy failed.",
				"instancegroup",
				instanceGroup.NamespacedName())
			return err
		}

		err = ctx.AwsWorker.DeleteDefaultFargateRole()
		if err == nil {
			ctx.Log.Info("Deleted the default role.",
				"instancegroup",
				instanceGroup.NamespacedName())
			return nil
		}
		if !becauseErrorContains(err, iam.ErrCodeNoSuchEntityException) {
			ctx.Log.Error(err,
				"Deleting the default role failed.",
				"instancegroup",
				instanceGroup.NamespacedName())
			return err
		}
	}

	err := worker.DeleteFargateProfile()
	if err != nil {

		if becauseErrorContains(err, eks.ErrCodeResourceInUseException) {
			ctx.Log.Info("Deletion of the fargate profile delayed",
				"instancegroup",
				instanceGroup.NamespacedName(),
				"cluster",
				spec.GetClusterName(),
				"profile",
				ctx.generateUniqueName(),
				"error", err)
			return nil
		}

		ctx.Log.Error(err, "Deletion of the fargate profile failed.",
			"instancegroup",
			instanceGroup.NamespacedName(),
			"cluster",
			spec.GetClusterName(),
			"profile",
			ctx.generateUniqueName())
		return err
	}

	ctx.Log.Info("Deletion of the fargate profile started",
		"instancegroup",
		instanceGroup.NamespacedName(),
		"cluster",
		spec.GetClusterName(),
		"profile",
		ctx.generateUniqueName())

	instanceGroup.SetState(v1alpha1.ReconcileDeleting)

	return nil
}

func (ctx *FargateInstanceGroupContext) Update() error {
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
func (ctx *FargateInstanceGroupContext) UpgradeNodes() error {
	return nil
}
func (ctx *FargateInstanceGroupContext) BootstrapNodes() error {
	return nil
}
func (ctx *FargateInstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileModified || instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
		return true
	}
	return false
}
func (ctx *FargateInstanceGroupContext) StateDiscovery() {
	instanceGroup := ctx.GetInstanceGroup()
	if instanceGroup.GetState() == v1alpha1.ReconcileInit {

		if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
			if ctx.GetDiscoveredState().IsProvisioned() {
				// Role exists and the Profile exists in some form (creating)
				if awsprovider.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), OngoingStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileModifying)
					// Role exists and the Profile exists (active)
				} else if awsprovider.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), FiniteStateString) {
					instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
				} else if awsprovider.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), UpdateRecoverableErrorString) {
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
				if awsprovider.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), OngoingStateString) {
					// deleting stack is in an ongoing state
					instanceGroup.SetState(v1alpha1.ReconcileDeleting)
				} else if awsprovider.IsProfileInConditionState(ctx.GetDiscoveredState().GetProfileStatus(), FiniteStateString) {
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

func (ctx *FargateInstanceGroupContext) SetState(state v1alpha1.ReconcileState) {
	ctx.GetInstanceGroup().SetState(state)
}
func (ctx *FargateInstanceGroupContext) GetState() v1alpha1.ReconcileState {
	return ctx.GetInstanceGroup().GetState()
}
func (ctx *FargateInstanceGroupContext) Locked() bool {
	return false
}
