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

package eksmanaged

import (
	"context"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type EksManagedUnitTest struct {
	Description   string
	Provisioner   *EksManagedInstanceGroupContext
	InstanceGroup *v1alpha1.InstanceGroup
	GroupExist    bool
	NodeGroup     *eks.Nodegroup
	UpdateNeeded  bool
	VpcID         string
	ExpectedState v1alpha1.ReconcileState
}

type FakeIG struct {
	Name         string
	Namespace    string
	ClusterName  string
	CurrentState string
	IsDeleting   bool
}

type stubEKS struct {
	eksiface.EKSAPI
	NodeGroup       *eks.Nodegroup
	NodeGroupExists bool
}

func (s *stubEKS) DescribeNodegroup(input *eks.DescribeNodegroupInput) (*eks.DescribeNodegroupOutput, error) {
	output := &eks.DescribeNodegroupOutput{
		Nodegroup: s.NodeGroup,
	}
	if !s.NodeGroupExists {
		return output, awserr.New(eks.ErrCodeResourceNotFoundException, "not found", errors.New("notFound"))
	}
	return output, nil
}

func (s *stubEKS) CreateNodegroup(input *eks.CreateNodegroupInput) (*eks.CreateNodegroupOutput, error) {
	output := &eks.CreateNodegroupOutput{}
	return output, nil
}

func (s *stubEKS) UpdateNodegroupConfig(input *eks.UpdateNodegroupConfigInput) (*eks.UpdateNodegroupConfigOutput, error) {
	output := &eks.UpdateNodegroupConfigOutput{}
	return output, nil
}

func (s *stubEKS) DeleteNodegroup(input *eks.DeleteNodegroupInput) (*eks.DeleteNodegroupOutput, error) {
	output := &eks.DeleteNodegroupOutput{}
	return output, nil
}

func getNodeGroup(state string) *eks.Nodegroup {
	return &eks.Nodegroup{
		Status: aws.String(state),
		Resources: &eks.NodegroupResources{
			AutoScalingGroups: []*eks.AutoScalingGroup{
				&eks.AutoScalingGroup{
					Name: aws.String("some-asg"),
				},
			},
		},
		ScalingConfig: &eks.NodegroupScalingConfig{
			MinSize: aws.Int64(3),
			MaxSize: aws.Int64(6),
		},
	}
}

func (f *FakeIG) getInstanceGroup() *v1alpha1.InstanceGroup {
	var deletionTimestamp metav1.Time

	if f.Name == "" {
		f.Name = "instancegroup-1"
	}
	if f.Namespace == "" {
		f.Namespace = "namespace-1"
	}
	if f.ClusterName == "" {
		f.ClusterName = "EKS-Test"
	}
	if f.CurrentState == "" {
		f.CurrentState = "Null"
	}
	if f.IsDeleting == true {
		deletionTimestamp = metav1.Time{Time: time.Now()}
	} else {
		nilTime := time.Time{}
		deletionTimestamp = metav1.Time{Time: nilTime}
	}

	instanceGroup := &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              f.Name,
			Namespace:         f.Namespace,
			DeletionTimestamp: &deletionTimestamp,
		},
		Spec: v1alpha1.InstanceGroupSpec{
			Provisioner: "eks-managed",
			EKSManagedSpec: &v1alpha1.EKSManagedSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSManagedConfiguration: &v1alpha1.EKSManagedConfiguration{
					EksClusterName:     f.ClusterName,
					VolSize:            20,
					InstanceType:       "m3.medium",
					NodeRole:           "some-iam-role",
					KeyPairName:        "my-keypair",
					NodeSecurityGroups: []string{"sg-122222", "sg-3333333"},
					NodeLabels:         map[string]string{"foo": "bar"},
					AmiType:            "AL2_x86_64",
					Subnets:            []string{"subnet-122222", "subnet-3333333"},
					Tags: []map[string]string{
						{
							"key":   "a-key",
							"value": "a-value",
						},
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				Type: "managed",
			},
		},
		Status: v1alpha1.InstanceGroupStatus{
			CurrentState: f.CurrentState,
		},
	}

	return instanceGroup
}

func (u *EksManagedUnitTest) Run(t *testing.T) {

	var (
		client    = fake.NewSimpleClientset()
		dynScheme = runtime.NewScheme()
		dynClient = fakedynamic.NewSimpleDynamicClient(dynScheme)
	)

	kube := kubeprovider.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	u.VpcID = "vpc-123345"

	aws := awsprovider.AwsWorker{
		EksClient: &stubEKS{
			NodeGroupExists: u.GroupExist,
			NodeGroup:       u.NodeGroup,
		},
	}

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(u.InstanceGroup)
	unstructuredInstanceGroup := &unstructured.Unstructured{
		Object: obj,
	}
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	kube.KubeDynamic.Resource(v1alpha1.GroupVersionResource).Namespace(u.InstanceGroup.GetNamespace()).Create(context.Background(), unstructuredInstanceGroup, metav1.CreateOptions{})
	input := provisioners.ProvisionerInput{
		AwsWorker:     aws,
		Kubernetes:    kube,
		InstanceGroup: u.InstanceGroup,
		Log:           ctrl.Log.WithName("unit-test").WithName("InstanceGroup"),
	}

	u.Provisioner = New(input)

	if err := u.Provisioner.CloudDiscovery(); err != nil {
		t.Fatal(err)
	}

	u.Provisioner.StateDiscovery()
	if u.ExpectedState != u.InstanceGroup.GetState() {
		t.Fatalf("DiscoveredState, expected:\n %#v, \ngot:\n %#v", u.ExpectedState, u.InstanceGroup.GetState())
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitDelete {
		if err := u.Provisioner.Delete(); err != nil {
			t.Fatal(err)
		}
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitCreate {
		if err := u.Provisioner.Create(); err != nil {
			t.Fatal(err)
		}
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitUpdate {
		if err := u.Provisioner.Update(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStateDiscoveryInitUpdate(t *testing.T) {
	ig := FakeIG{}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exist (active), state should be InitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("ACTIVE"),
		UpdateNeeded:  true,
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryModifying(t *testing.T) {
	ig := FakeIG{}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exist (updating), state should be ReconcileModifying",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("UPDATING"),
		UpdateNeeded:  true,
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileModifying,
	}
	testCase.Run(t)
}

func TestStateDiscoveryFailed(t *testing.T) {
	ig := FakeIG{}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exist (failed), state should be ReconcileErr",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("CREATE_FAILED"),
		UpdateNeeded:  true,
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestStateDiscoveryCreate(t *testing.T) {
	ig := FakeIG{}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup does not exist, state should be InitCreate",
		InstanceGroup: ig.getInstanceGroup(),
		GroupExist:    false,
		ExpectedState: v1alpha1.ReconcileInitCreate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleting(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exists (DELETING), state should be Deleting",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("DELETING"),
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileDeleting,
	}
	testCase.Run(t)
}

func TestStateDiscoveryInitDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exists (ACTIVE), state should be InitDelete",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("ACTIVE"),
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleteFail(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup already exists (DELETE_FAILED), state should be ReconcileErr",
		InstanceGroup: ig.getInstanceGroup(),
		NodeGroup:     getNodeGroup("DELETE_FAILED"),
		GroupExist:    true,
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleted(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksManagedUnitTest{
		Description:   "StateDiscovery - when a nodegroup does not exist, state should be Deleted",
		InstanceGroup: ig.getInstanceGroup(),
		GroupExist:    false,
		ExpectedState: v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}
