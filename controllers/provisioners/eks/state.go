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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
)

const (
	ScalingGroupDeletionStatus = "Delete in progress"
)

var (
	NonRetryableStates = []v1alpha1.ReconcileState{v1alpha1.ReconcileErr, v1alpha1.ReconcileReady, v1alpha1.ReconcileDeleted}
)

func (ctx *EksInstanceGroupContext) StateDiscovery() {
	var (
		instanceGroup = ctx.GetInstanceGroup()
		state         = ctx.GetDiscoveredState()
		provisioned   = state.IsProvisioned()
		group         = state.GetScalingGroup()
	)
	// only discover state if it's a new reconcile
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		return
	}

	var deleted bool
	if !ctx.InstanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		deleted = true
	}

	if deleted {
		// resource is being deleted
		if provisioned {
			// scaling group still provisioned
			if aws.StringValue(group.Status) == ScalingGroupDeletionStatus {
				// scaling group is being deleted
				instanceGroup.SetState(v1alpha1.ReconcileDeleting)
			} else {
				// scaling group still exists
				instanceGroup.SetState(v1alpha1.ReconcileInitDelete)
			}
		} else {
			// scaling group does not exist
			instanceGroup.SetState(v1alpha1.ReconcileDeleted)
		}
	} else {
		// resource is not being deleted
		if provisioned {
			// scaling group exists
			instanceGroup.SetState(v1alpha1.ReconcileInitUpdate)
		} else {
			// scaling group does not exist
			instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
		}
	}

}

func (ctx *EksInstanceGroupContext) IsReady() bool {
	instanceGroup := ctx.GetInstanceGroup()
	return instanceGroup.GetState() == v1alpha1.ReconcileModified
}
