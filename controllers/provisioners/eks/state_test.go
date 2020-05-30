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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
)

func TestStateDiscovery(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		crDeleted            bool
		scalingGroupExist    bool
		scalingGroupDeleting bool
		expectedState        v1alpha1.ReconcileState
	}{
		{crDeleted: false, scalingGroupExist: false, expectedState: v1alpha1.ReconcileInitCreate},
		{crDeleted: false, scalingGroupExist: true, expectedState: v1alpha1.ReconcileInitUpdate},
		{crDeleted: true, scalingGroupExist: true, expectedState: v1alpha1.ReconcileInitDelete},
		{crDeleted: true, scalingGroupExist: true, scalingGroupDeleting: true, expectedState: v1alpha1.ReconcileDeleting},
		{crDeleted: true, scalingGroupExist: false, expectedState: v1alpha1.ReconcileDeleted},
	}

	for i, tc := range tests {
		t.Logf("#%v -> %v", i, tc.expectedState)

		// assume initial state of init
		ig.SetState(v1alpha1.ReconcileInit)
		var deleteStatus string

		if tc.scalingGroupDeleting {
			deleteStatus = ScalingGroupDeletionStatus
		}
		if tc.crDeleted {
			ig.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
		}
		ctx.SetDiscoveredState(&DiscoveredState{
			Provisioned: tc.scalingGroupExist,
			ScalingGroup: &autoscaling.Group{
				Status: aws.String(deleteStatus),
			},
		})
		ctx.StateDiscovery()
		g.Expect(ctx.GetState()).To(gomega.Equal(tc.expectedState))
	}
}

func TestIsReady(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		initialState  v1alpha1.ReconcileState
		expectedReady bool
	}{
		{initialState: v1alpha1.ReconcileInit, expectedReady: false},
		{initialState: v1alpha1.ReconcileModified, expectedReady: true},
		{initialState: v1alpha1.ReconcileErr, expectedReady: false},
		{initialState: v1alpha1.ReconcileModifying, expectedReady: false},
		{initialState: v1alpha1.ReconcileDeleting, expectedReady: false},
		{initialState: v1alpha1.ReconcileInitCreate, expectedReady: false},
		{initialState: v1alpha1.ReconcileInitUpdate, expectedReady: false},
		{initialState: v1alpha1.ReconcileInitUpgrade, expectedReady: false},
	}

	for i, tc := range tests {
		t.Logf("#%v -> %v", i, tc.initialState)
		ig.SetState(tc.initialState)
		ready := ctx.IsReady()
		g.Expect(ready).To(gomega.Equal(tc.expectedReady))
	}

}
