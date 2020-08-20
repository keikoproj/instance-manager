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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/iam"
	awsauth "github.com/keikoproj/aws-auth/pkg/mapper"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeletePositive(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
		IAMRole: &iam.Role{},
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
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		IAMRole: &iam.Role{},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
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
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
			ResourceList: []*autoscaling.LaunchConfiguration{
				{
					LaunchConfigurationName: aws.String("my-cluster-instance-manager-instance-group-1-1234566"),
				},
			},
		},
	})

	asgMock.DeleteLaunchConfigurationErr = errors.New("some-error")
	err := ctx.Delete()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
}

func TestDeleteAutoScalingGroupNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
	})

	asgMock.DeleteAutoScalingGroupErr = errors.New("some-error")
	err := ctx.Delete()
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))
	asgMock.DeleteAutoScalingGroupErr = nil
}

func TestRemoveAuthRoleNegative(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		ig2     = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	// two instancegroups with same role arn
	ig.Status.NodesArn = "same-role"
	ig2.Name = "instance-group-2"
	ig2.Namespace = "different-namespace"
	ig2.Status.NodesArn = "same-role"

	igObj, err := kubeprovider.GetUnstructuredInstanceGroup(ig)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	ig2Obj, err := kubeprovider.GetUnstructuredInstanceGroup(ig2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = ctx.KubernetesClient.KubeDynamic.Resource(v1alpha1.GroupVersionResource).Namespace("instance-manager").Create(igObj, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = ctx.KubernetesClient.KubeDynamic.Resource(v1alpha1.GroupVersionResource).Namespace(ig2.Namespace).Create(ig2Obj, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		IAMRole: &iam.Role{
			Arn: aws.String("same-role"),
		},
		ScalingGroup: &autoscaling.Group{},
		ScalingConfiguration: &scaling.LaunchConfiguration{
			AwsWorker: w,
		},
	})

	ctx.BootstrapNodes()

	// Only one role is added to aws-auth
	auth, _, err := awsauth.ReadAuthMap(k.Kubernetes)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(auth.MapRoles)).To(gomega.Equal(1))

	// after delete, the role should not be deleted
	err = ctx.Delete()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))

	auth, _, err = awsauth.ReadAuthMap(k.Kubernetes)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(auth.MapRoles)).To(gomega.Equal(1))

	// this time the role should be successfully removed
	err = ctx.KubernetesClient.KubeDynamic.Resource(v1alpha1.GroupVersionResource).Namespace(ig2.Namespace).Delete(ig2.Name, &metav1.DeleteOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = ctx.Delete()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ctx.GetState()).To(gomega.Equal(v1alpha1.ReconcileDeleting))

	auth, _, err = awsauth.ReadAuthMap(k.Kubernetes)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(auth.MapRoles)).To(gomega.Equal(0))
}
