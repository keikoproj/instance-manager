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
	"flag"
	"io/ioutil"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

type EksManagedUnitTest struct {
	Description   string
	Provisioner   *EksManagedInstanceGroupContext
	InstanceGroup *v1alpha1.InstanceGroup
	GroupExist    bool
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
	VpcID string
}

func (s *stubEKS) DescribeNodegroup(input *eks.DescribeNodegroupInput) (*eks.DescribeNodegroupOutput, error) {
	output := &eks.DescribeNodegroupOutput{
		Cluster: &eks.Cluster{
			ResourcesVpcConfig: &eks.VpcConfigResponse{
				VpcId: aws.String(s.VpcID),
			},
		},
	}
	return output, nil
}

var loggingEnabled bool

func init() {
	flag.BoolVar(&loggingEnabled, "logging-enabled", false, "Enable Logging")
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

	if !loggingEnabled {
		log.Out = ioutil.Discard
	}

	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	u.VpcID = "vpc-123345"

	aws := awsprovider.AwsWorker{
		EksClient: &stubEKS{
			VpcID: u.VpcID,
		},
	}

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(u.InstanceGroup)
	unstructuredInstanceGroup := &unstructured.Unstructured{
		Object: obj,
	}
	kube.KubeDynamic.Resource(v1alpha1.GroupVersionResource).Namespace(u.InstanceGroup.GetNamespace()).Create(unstructuredInstanceGroup, metav1.CreateOptions{})

	provisioner, err := New(u.InstanceGroup, kube, aws)
	if err != nil {
		t.Fatal(err)
	}
	u.Provisioner = provisioner

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
		Description:   "StateDiscovery - when a stack already exist (idle), state should be InitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		GroupExist:    true,
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}
