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
	//	"flag"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	//"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/pkg/errors"
	//	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	//	"k8s.io/apimachinery/pkg/runtime"
	//	fakedynamic "k8s.io/client-go/dynamic/fake"
	//	"k8s.io/client-go/kubernetes/fake"
	"testing"
	"time"
)

type EksFargateUnitTest struct {
	Description         string
	Provisioner         *InstanceGroupContext
	InstanceGroup       *v1alpha1.InstanceGroup
	ProfileBasic        *eks.FargateProfile
	ProfileFromDescribe *eks.FargateProfile
	ProfileFromCreate   *eks.FargateProfile
	UpdateNeeded        bool
	ListOfProfiles      []*string
	ProfileExists       bool
	MakeCreateFail      bool
	MakeDescribeFail    bool
	ExpectedState       v1alpha1.ReconcileState
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
	ProfileFromDescribe *eks.FargateProfile
	ProfileFromCreate   *eks.FargateProfile
	ProfileBasic        *eks.FargateProfile
	ProfileExists       bool
	ListOfProfiles      []*string
	MakeCreateFail      bool
	MakeDescribeFail    bool
}
type stubIAM struct {
	iamiface.IAMAPI
	Profile *eks.FargateProfile
}

func (s *stubIAM) CreateRole(input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	output := &iam.CreateRoleOutput{
		Role: &iam.Role{
			Arn: aws.String("the profile execution arn"),
		},
	}
	return output, nil
}
func (s *stubIAM) GetRole(input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	output := &iam.GetRoleOutput{}
	return output, nil
}
func (s *stubIAM) AttachRolePolicy(input *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error) {
	return &iam.AttachRolePolicyOutput{}, nil
}

func (s *stubEKS) DescribeFargateProfile(input *eks.DescribeFargateProfileInput) (*eks.DescribeFargateProfileOutput, error) {
	if s.MakeDescribeFail {
		return nil, awserr.New(eks.ErrCodeResourceNotFoundException, "not found", errors.New("notFound"))
	}

	output := &eks.DescribeFargateProfileOutput{
		FargateProfile: s.ProfileFromDescribe,
	}
	return output, nil
}

func (s *stubEKS) CreateFargateProfile(input *eks.CreateFargateProfileInput) (*eks.CreateFargateProfileOutput, error) {
	output := &eks.CreateFargateProfileOutput{
		FargateProfile: s.ProfileFromCreate,
	}
	if s.MakeCreateFail {
		return nil, errors.New("CreateFargateProfile failed")
	}

	return output, nil
}

func (s *stubEKS) DeleteFargateProfile(input *eks.DeleteFargateProfileInput) (*eks.DeleteFargateProfileOutput, error) {
	output := &eks.DeleteFargateProfileOutput{}
	return output, nil
}

func (s *stubEKS) ListFargateProfilesPages(input *eks.ListFargateProfilesInput, fn func(page *eks.ListFargateProfilesOutput, lastPage bool) bool) error {
	fn(&eks.ListFargateProfilesOutput{
		FargateProfileNames: s.ListOfProfiles,
	}, true)
	return nil
}

func init() {
	//	flag.BoolVar(&loggingEnabled, "logging-enabled", false, "Enable Logging")
	//log.Out = ioutil.Discard
}

func getProfile(state string) *eks.FargateProfile {
	return &eks.FargateProfile{
		Status: aws.String(state)}
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
			Provisioner: "eks-fargate",
			EKSFargateSpec: &v1alpha1.EKSFargateSpec{
				ClusterName:         aws.String(""),
				ProfileName:         aws.String(""),
				PodExecutionRoleArn: aws.String(""),
				Subnets:             []*string{aws.String("subnet-1111111"), aws.String("subnet-222222")},
				Tags: []map[string]string{
					{
						"key":   "a-key",
						"value": "a-value",
					},
				},
			},
		},
		Status: v1alpha1.InstanceGroupStatus{
			CurrentState: f.CurrentState,
		},
	}

	return instanceGroup
}
func (u *EksFargateUnitTest) BuildProvisioner(t *testing.T) *InstanceGroupContext {
	aws := &awsprovider.AwsFargateWorker{
		EksClient: &stubEKS{
			ProfileBasic:        u.ProfileBasic,
			ProfileFromDescribe: u.ProfileFromDescribe,
			ProfileFromCreate:   u.ProfileFromCreate,
			ProfileExists:       u.ProfileExists,
			MakeCreateFail:      u.MakeCreateFail,
			MakeDescribeFail:    u.MakeDescribeFail,
			ListOfProfiles:      u.ListOfProfiles,
		},
		IamClient:   &stubIAM{},
		ProfileName: u.InstanceGroup.Spec.EKSFargateSpec.GetProfileName(),
		ClusterName: u.InstanceGroup.Spec.EKSFargateSpec.GetClusterName(),
	}
	provisioner, err := New(u.InstanceGroup, aws)
	aws.RetryLimit = 1

	if err != nil {
		t.Fatal(err)
	}
	u.Provisioner = provisioner
	return provisioner
}

func (u *EksFargateUnitTest) Run(t *testing.T) {

	provisioner := u.BuildProvisioner(t)

	if err := provisioner.CloudDiscovery(); err != nil {
		t.Fatal(err)
	}

	provisioner.StateDiscovery()
	if u.ExpectedState != u.InstanceGroup.GetState() {
		t.Fatalf("DiscoveredState, expected:\n %#v, \ngot:\n %#v", u.ExpectedState, u.InstanceGroup.GetState())
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitDelete {
		if err := provisioner.Delete(); err != nil {
			t.Fatal(err)
		}
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitCreate {
		if err := provisioner.Create(); err != nil {
			t.Fatal(err)
		}
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitUpdate {
		if err := provisioner.Update(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStateDiscoveryInitUpdate(t *testing.T) {
	ig := FakeIG{CurrentState: string(v1alpha1.ReconcileInit)}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - when a profile already exist (active), state should be InitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("ACTIVE"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryInitCreate(t *testing.T) {
	ig := FakeIG{CurrentState: string(v1alpha1.ReconcileInit)}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - when a profile NOT exist , state should be ReconcileInitCreate",
		InstanceGroup: ig.getInstanceGroup(),
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitCreate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileModifying1(t *testing.T) {
	ig := FakeIG{CurrentState: string(v1alpha1.ReconcileInit)}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - when a profile is CREATING, state should be ReconcileModifying",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("CREATING"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileModifying,
	}
	testCase.Run(t)
}
func TestStateDiscoveryReconcileModifying2(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - when a profile is DELETING, state should be ReconcileModifying",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("DELETING"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileModifying,
	}
	testCase.Run(t)
}
func TestStateDiscoveryErr1(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource creation state and the profile is DELETE_FAILED, state should be ReconcileErr",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("DELETE_FAILED"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}
func TestStateDiscoveryReconcileRecoverableUpdate(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource creation state and the profile is CREATE_FAILED, state should be ReconcileInitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("CREATE_FAILED"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}
func TestStateDiscoveryReconcileDeleted(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource is deleting state and no profile, state should be ReconcileDeleted",
		InstanceGroup: ig.getInstanceGroup(),
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}
func TestStateDiscoveryReconcileInitDelete(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource is deleting state and the profile is ACTIVE, state should be ReconcileInitDelete",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("ACTIVE"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileDeleting1(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource is deleting state and the profile is CREATING, state should be ReconcileDeleting",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("CREATING"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileDeleting,
	}
	testCase.Run(t)
}
func TestStateDiscoveryReconcileDeleting2(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource is deleting state and the profile is DELETING, state should be ReconcileDeleting",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("DELETING"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileDeleting,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleteWithRecoverableUpate(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource delete state and the profile is CREATE_FAILED, state should be ReconcileInitDelete",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("CREATE_FAILED"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}
func TestStateDiscoveryErr2(t *testing.T) {
	ig := FakeIG{IsDeleting: true}
	testCase := EksFargateUnitTest{
		Description:   "StateDiscovery - resource delete state and the profile is DELETE_FAILED, state should be ReconcileErr",
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("DELETE_FAILED"),
		},
		UpdateNeeded:  true,
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}
func TestIsReadyPass(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("ACTIVE"),
		},
	}
	ctx := testCase.BuildProvisioner(t)
	if !ctx.IsReady() {
		t.Fatal("TestIsReadyPass: got false, expected: true")
	}
}
func TestIsReadyFail(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		InstanceGroup: ig.getInstanceGroup(),
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("CREATING"),
		},
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.IsReady() {
		t.Fatal("TestIsReadyFail: got positive, expected: false")
	}
}
func TestCanCreateAndDelete(t *testing.T) {
	ig := FakeIG{CurrentState: string(v1alpha1.ReconcileInit)}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		//ProfileFromDescribe: &eks.FargateProfile{
		//	Status: aws.String("ACTIVE"),
		//		},
	}
	ctx := testCase.BuildProvisioner(t)
	canCreate, err := ctx.CanCreateAndDelete()
	if err != nil {
		t.Fatalf("TestCanCreateAndDelete: got unexpected exception: %v", err)
	}
	if !canCreate {
		t.Fatal("TestCanCreateAndDelete: got false, expected: true")
	}
}

func TestCanCreateAndDelete1(t *testing.T) {
	ig := FakeIG{CurrentState: string(v1alpha1.ReconcileInit)}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("DELETING"),
		},
		ListOfProfiles: []*string{aws.String("profile1")},
	}
	ctx := testCase.BuildProvisioner(t)
	canCreate, err := ctx.CanCreateAndDelete()
	if err != nil {
		t.Fatalf("TestCanCreateAndDelete: got unexpected exception: %v", err)
	}
	if canCreate {
		t.Fatal("TestCanCreateAndDelete: got true, expected: false")
	}
}
func TestCreateWithSuppliedExecutionArn(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f")) // no arn
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil, // no existing profile
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Create() != nil {
		t.Fatal("TestCreateWithSuppliedExecutionArn: got positive, expected: false")
	}
	if ctx.GetInstanceGroup().Status.GetFargateRoleName() != "" {
		t.Fatal("TestCreateWithSuppliedExecutionArn: expect empty rolename")
	}
}
func TestCreateWithOutSuppliedExecutionArn(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String(""))
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil, // no existing profile
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Create() != nil {
		t.Fatal("TestCreateWithSuppliedExecutionArn: got positive, expected: false")
	}
	if ctx.GetInstanceGroup().Status.GetFargateRoleName() != *ctx.AwsFargateWorker.CreateDefaultRoleName() {
		t.Fatal("TestCreateWithSuppliedExecutionArn: expect empty rolename")
	}
}
func TestCreateWithFailure(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	testCase := EksFargateUnitTest{
		InstanceGroup:    instanceGroup,
		MakeDescribeFail: true,
		MakeCreateFail:   true,
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Create() == nil {
		t.Fatal("TestCreateWithFailure: expected an exception and didn't get one")
	}
}
func TestCreateWithSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	testCase := EksFargateUnitTest{
		InstanceGroup:  instanceGroup,
		MakeCreateFail: false,
		ProfileBasic:   nil, // no existing profile
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Create() != nil {
		t.Fatal("TestCreateWithSuccess: got an exception when not expected")
	}
	if ctx.GetState() != v1alpha1.ReconcileModifying {
		t.Fatalf("TestCreateWithSuccess: Expecting end state to be ReconcileModifying.  It was %s instead", ctx.GetState())
	}

}
func TestDeleteWithSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	testCase := EksFargateUnitTest{
		InstanceGroup:  instanceGroup,
		MakeCreateFail: false,
		ProfileBasic:   getProfile("ACTIVE"),
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Delete() != nil {
		t.Fatal("TestDeleteWithSuccess: got an exception when not expected")
	}
	if ctx.GetState() != v1alpha1.ReconcileDeleting {
		t.Fatalf("TestDeleteWithSuccess: Expecting end state to be ReconcileDeleting.  It was %s instead", ctx.GetState())
	}
}
func TestUpdateWithChangesSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("DinahCluster"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("DinahProfile"))
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	testCase := EksFargateUnitTest{
		InstanceGroup:  instanceGroup,
		MakeCreateFail: false,
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String("ACTIVE"),
		},
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Update() != nil {
		t.Fatal("TestUpdateWithSuccess: got an exception when not expected")
	}
	if ctx.GetState() != v1alpha1.ReconcileDeleting {
		t.Fatalf("TestDeleteWithSuccess: Expecting end state to be ReconcileDeleting.  It was %s instead", ctx.GetState())
	}
}
func TestUpgradeNodes(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil,
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.UpgradeNodes() != nil {
		t.Fatal("TestUpgradeNodes: got an exception when not expected")
	}
}
func TestBootstrapNodes(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil,
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.BootstrapNodes() != nil {
		t.Fatal("TestBootstrapNodes: got an exception when not expected")
	}
}
func TestIsUpgradeNeeded(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil,
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.IsUpgradeNeeded() != false {
		t.Fatal("TestIsUpgradeNeeded: got an true when false expected")
	}
}
func TestCloudDiscovery(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileBasic:  nil,
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.CloudDiscovery() != nil {
		t.Fatal("TestCloudDiscovery: got an exception when none expected")
	}
}
func TestCreateFargateTags(t *testing.T) {
	input := []map[string]string{{"key1": "value1"}}
	output := CreateFargateTags(input)
	if len(output) != 1 || *output["key1"] != "value1" {
		t.Fatalf("TestCreateFargateTags: output is %v", output)
	}
}
func TestEqualTagsSuccess(t *testing.T) {
	tag1 := map[string]*string{"key2": aws.String("value2"), "key1": aws.String("value1")}
	tag2 := map[string]*string{"key1": aws.String("value1"), "key2": aws.String("value2")}
	b := equalTags(tag1, tag2)
	if b != true {
		t.Fatalf("TestEqualTagsSuccess1: expected true and got a false")
	}
	b = equalTags(tag2, tag1)
	if b != true {
		t.Fatalf("TestEqualTagsSuccess2: expected true and got a false")
	}
}
func TestEqualTagsFalse1(t *testing.T) {
	tag1 := map[string]*string{"key2": aws.String("value2"), "key1": aws.String("value1"), "key3": aws.String("blsh")}
	tag2 := map[string]*string{"key1": aws.String("value1"), "key2": aws.String("value2")}
	b := equalTags(tag1, tag2)
	if b != false {
		t.Fatalf("TestEqualTagsSuccess1: expected false and got a true")
	}
}
func TestEqualTagsFalse2(t *testing.T) {
	tag1 := map[string]*string{"key2": aws.String("value2"), "key1": aws.String("value1"), "key3": aws.String("blsh")}
	tag2 := map[string]*string{"key3": aws.String("somethingelse"), "key1": aws.String("value1"), "key2": aws.String("value2")}
	b := equalTags(tag1, tag2)
	if b != false {
		t.Fatalf("TestEqualTagsSuccess1: expected false and got a true")
	}
}
func TestEqualSubnetSlicesSuccess(t *testing.T) {
	oldSubnets := []*string{aws.String("subnet-12331"), aws.String("subnet-xl3823"), aws.String("subnet-vlieo12")}
	newSubnets := []*string{aws.String("subnet-12331"), aws.String("subnet-xl3823"), aws.String("subnet-vlieo12")}
	b := equalSubnetSlices(oldSubnets, newSubnets)
	if !b {
		t.Fatal("TestEqualSubnetSlicesSuccess: get false, expected true")
	}
}
func TestEqualSubnetSlicesFailure1(t *testing.T) {
	oldSubnets := []*string{aws.String("subnet-xl3823"), aws.String("subnet-vlieo12")}
	newSubnets := []*string{aws.String("subnet-12331"), aws.String("subnet-xl3823"), aws.String("subnet-vlieo12")}
	b := equalSubnetSlices(oldSubnets, newSubnets)
	if b {
		t.Fatal("TestEqualSubnetSlicesFailure1: got true, expected false")
	}
}
func TestEqualSubnetSlicesFailure2(t *testing.T) {
	oldSubnets := []*string{aws.String("subnet-xl3823"), aws.String("subnet-vlieo12")}
	newSubnets := []*string{aws.String("subnet-xl3823"), aws.String("subnet-vxxxo12")}
	b := equalSubnetSlices(oldSubnets, newSubnets)
	if b {
		t.Fatal("TestEqualSubnetSlicesFailure1: got true, expected false")
	}
}
func TestEqualSelectorsSuccess1(t *testing.T) {
	selectors1 := []*eks.FargateProfileSelector{
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace1"),
			Labels:    map[string]*string{"l11": aws.String("v11"), "l12": aws.String("v12")},
		},
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace2"),
			Labels:    map[string]*string{"l21": aws.String("v21"), "l22": aws.String("v22")},
		},
	}
	selectors2 := []*eks.FargateProfileSelector{
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace1"),
			Labels:    map[string]*string{"l11": aws.String("v11"), "l12": aws.String("v12")},
		},
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace2"),
			Labels:    map[string]*string{"l21": aws.String("v21"), "l22": aws.String("v22")},
		},
	}
	b := equalSelectors(selectors1, selectors2)
	if !b {
		t.Fatal("TestEqualSelectorsSuccess: got false, expected true")
	}
}
func TestEqualSelectorsSuccess2(t *testing.T) {
	selectors1 := []*eks.FargateProfileSelector{
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace1"),
			Labels:    map[string]*string{"l11": aws.String("v11"), "l12": aws.String("v12")},
		},
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace2"),
			Labels:    map[string]*string{"l21": aws.String("v21"), "l22": aws.String("v22")},
		},
	}
	selectors2 := []*eks.FargateProfileSelector{
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace2"),
			Labels:    map[string]*string{"l21": aws.String("v21"), "l22": aws.String("v22")},
		},
		&eks.FargateProfileSelector{
			Namespace: aws.String("namespace1"),
			Labels:    map[string]*string{"l12": aws.String("v12"), "l11": aws.String("v11")},
		},
	}
	b := equalSelectors(selectors1, selectors2)
	if !b {
		t.Fatal("TestEqualSelectorsSuccess: got false, expected true")
	}
}
func TestHasChangedSimpleTagSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{{"key2": "value2"}, {"key1": "value1"}})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{"key1": aws.String("value1"), "key2": aws.String("value2")},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != false {
		t.Fatal("TestHasChangedSimpleTagSuccess: get a true but expected a false")
	}
}
func TestHasChangedSimpleTagFailureOnValue(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{{"key2": "value2"}, {"key1": "value2"}})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{"key1": aws.String("value1"), "key2": aws.String("value2")},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedSimpleTagFailureOnValue: get a false but expected a true")
	}
}
func TestHasChangedSimpleTagFailureOnKey(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{{"key2": "value2"}, {"key3": "value1"}})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{"key1": aws.String("value1"), "key2": aws.String("value2")},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedSimpleTagFailureOnKey: get a false but expected a true")
	}
}
func TestHasChangedSimpleTagFailureOnLength(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{{"key2": "value2"}, {"key3": "value1"}})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{"key3": aws.String("value1"), "key1": aws.String("value1"), "key2": aws.String("value2")},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedSimpleTagFailureOnLength: get a false but expected a true")
	}
}
func TestHasChangedArnFail(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:g"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedArnFail: get a false but expected a true")
	}
}
func TestHasChangedArnFailOnDefaultArn(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName(aws.String("ClusterXYZ"))
	instanceGroup.Spec.EKSFargateSpec.SetProfileName(aws.String("ProfileABC"))
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String(""))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("ClusterXYZ_ProfileABC_Role"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != false {
		t.Fatal("TestHasChangedArnFailOnDefaultArn: get a true but expected a false")
	}
}
func TestHasChangedArnSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != false {
		t.Fatal("TestHasChangedSuccess: get a false but expected a true")
	}
}
func TestHasChangedSubnetsFailureByValue(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{aws.String("subnet-123"), aws.String("subnet-abcea")})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{aws.String("subnet-123"), aws.String("subnet-abc")},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedSubnetsFailureByValue: get a false but expected a true")
	}
}
func TestHasChangedSubnetsFailureByLength(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{aws.String("subnet-123"), aws.String("subnet-abc"), aws.String("subnet-000")})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{aws.String("subnet-123"), aws.String("subnet-abc")},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b != true {
		t.Fatal("TestHasChangedSubnetsFailureByLength")
	}
}
func TestHasChangedSubnetsSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetTags([]map[string]string{})
	instanceGroup.Spec.EKSFargateSpec.SetSelectors([]*v1alpha1.EKSFargateSelectors{})
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn(aws.String("a:b:c:d:e:f"))
	instanceGroup.Spec.EKSFargateSpec.SetSubnets([]*string{})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Tags:                map[string]*string{},
			Selectors:           []*eks.FargateProfileSelector{},
			PodExecutionRoleArn: aws.String("a:b:c:d:e:f"),
			Subnets:             []*string{},
		},
	}
	ctx := testCase.BuildProvisioner(t)
	b := ctx.HasChanged()
	if b == true {
		t.Fatal("TestHasChangedSubnetsSuccess: get a true but expected a false")
	}
}
