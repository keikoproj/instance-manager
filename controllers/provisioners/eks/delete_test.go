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

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
)

func TestDeletePositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := New(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		ScalingGroup:        &autoscaling.Group{},
		LaunchConfiguration: &autoscaling.LaunchConfiguration{},
		IAMRole:             &iam.Role{},
	})

	err := ctx.Delete()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
}

func TestDeleteManagedRoleNegative(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := New(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		IAMRole: &iam.Role{},
	})

	iamMock.DeleteRoleErr = awserr.New(iam.ErrCodeUnmodifiableEntityException, "", errors.New("some-error"))
	err := ctx.Delete()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))

	// should not delete unmanaged roles
	configuration.ExistingRoleName = "existing-role"
	err = ctx.Delete()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
}

func TestDeleteLaunchConfigurationNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := New(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		LaunchConfiguration: &autoscaling.LaunchConfiguration{},
	})

	asgMock.DeleteLaunchConfigurationErr = errors.New("some-error")
	err := ctx.Delete()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
	asgMock.DeleteLaunchConfigurationErr = nil
}

func TestDeleteAutoScalingGroupNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
	)

	w := MockAwsWorker(asgMock, iamMock)
	ctx := New(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		ScalingGroup: &autoscaling.Group{},
	})

	asgMock.DeleteAutoScalingGroupErr = errors.New("some-error")
	err := ctx.Delete()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
	asgMock.DeleteAutoScalingGroupErr = nil

}
