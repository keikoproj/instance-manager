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
	//UpdateNeeded        bool
	ProfileExists            bool
	MakeCreateProfileFail    bool
	MakeCreateProfileRetry   bool
	MakeDeleteProfileFail    bool
	MakeDeleteProfileRetry   bool
	MakeDescribeProfileFail  bool
	CheckArnFor              string
	MakeCreateRoleFail       bool
	MakeGetRoleFail          bool
	CreateRoleDupFound       bool
	DetachRolePolicyFound    bool
	DetachRolePolicyFail     bool
	MakeAttachRolePolicyFail bool
	MakeDeleteRoleFail       bool
	DeleteRoleFound          bool
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
	ProfileFromDescribe     *eks.FargateProfile
	ProfileFromCreate       *eks.FargateProfile
	ProfileBasic            *eks.FargateProfile
	ProfileExists           bool
	MakeCreateProfileFail   bool
	MakeCreateProfileRetry  bool
	MakeDeleteProfileFail   bool
	MakeDeleteProfileRetry  bool
	MakeDescribeProfileFail bool
	CheckArnFor             string
}
type stubIAM struct {
	iamiface.IAMAPI
	Profile                  *eks.FargateProfile
	MakeCreateRoleFail       bool
	MakeGetRoleFail          bool
	CreateRoleDupFound       bool
	MakeAttachRolePolicyFail bool
	DetachRolePolicyFound    bool
	DetachRolePolicyFail     bool
	MakeDeleteRoleFail       bool
	DeleteRoleFound          bool
}

func (s *stubIAM) DetachRolePolicy(input *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error) {
	if s.DetachRolePolicyFail == false {
		if !s.DetachRolePolicyFound {
			return nil, awserr.New(iam.ErrCodeNoSuchEntityException, "not found", errors.New(""))
		}
		output := &iam.DetachRolePolicyOutput{}
		return output, nil
	} else {
		return nil, errors.New("detach role policy failed")
	}
}
func (s *stubIAM) DeleteRole(input *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error) {
	if s.MakeDeleteRoleFail == false {
		if !s.DeleteRoleFound {
			return nil, awserr.New(iam.ErrCodeNoSuchEntityException, "not found", errors.New(""))
		}
		output := &iam.DeleteRoleOutput{}
		return output, nil
	} else {
		return nil, errors.New("delete role failed")
	}
}
func (s *stubIAM) CreateRole(input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	if s.MakeCreateRoleFail == false {
		if s.CreateRoleDupFound {
			return nil, awserr.New(iam.ErrCodeEntityAlreadyExistsException, "duplicate found", errors.New(""))
		}
		output := &iam.CreateRoleOutput{
			Role: &iam.Role{
				Arn: aws.String("the profile execution arn"),
			},
		}
		return output, nil
	} else {
		return nil, errors.New("create role failed")
	}
}
func (s *stubIAM) GetRole(input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	if s.MakeGetRoleFail == false {
		output := &iam.GetRoleOutput{
			Role: &iam.Role{
				Arn: aws.String("eksfargate::dummy_arn"),
			},
		}
		return output, nil
	} else {
		return nil, errors.New("get role failed")
	}
}
func (s *stubIAM) AttachRolePolicy(input *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error) {
	if s.MakeAttachRolePolicyFail == false {
		return &iam.AttachRolePolicyOutput{}, nil
	} else {
		return nil, errors.New("attach role policy failed")
	}
}

func (s *stubEKS) DescribeFargateProfile(input *eks.DescribeFargateProfileInput) (*eks.DescribeFargateProfileOutput, error) {
	if s.MakeDescribeProfileFail {
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
	if s.MakeCreateProfileRetry {
		return nil, awserr.New(eks.ErrCodeResourceInUseException, "resource in use", errors.New("resource in use"))
	}

	if s.MakeCreateProfileFail {
		return nil, errors.New("create profile failed")
	}
	if s.CheckArnFor != "" && s.CheckArnFor != *input.PodExecutionRoleArn {
		return nil, errors.New("bad arn")
	}

	return output, nil
}

func (s *stubEKS) DeleteFargateProfile(input *eks.DeleteFargateProfileInput) (*eks.DeleteFargateProfileOutput, error) {
	if s.MakeDeleteProfileRetry {
		return nil, awserr.New(eks.ErrCodeResourceInUseException, "resource in use", errors.New("resource in use"))
	}
	if s.MakeDeleteProfileFail == false {
		output := &eks.DeleteFargateProfileOutput{}
		return output, nil
	} else {
		return nil, errors.New("delete profile failed")
	}
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
				ClusterName:         "",
				ProfileName:         "",
				PodExecutionRoleArn: "",
				Subnets:             []string{"subnet-1111111", "subnet-222222"},
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
	aws := &awsprovider.AwsWorker{
		EksClient: &stubEKS{
			ProfileBasic:            u.ProfileBasic,
			ProfileFromDescribe:     u.ProfileFromDescribe,
			ProfileFromCreate:       u.ProfileFromCreate,
			ProfileExists:           u.ProfileExists,
			MakeCreateProfileFail:   u.MakeCreateProfileFail,
			MakeCreateProfileRetry:  u.MakeCreateProfileRetry,
			MakeDeleteProfileFail:   u.MakeDeleteProfileFail,
			MakeDeleteProfileRetry:  u.MakeDeleteProfileRetry,
			MakeDescribeProfileFail: u.MakeDescribeProfileFail,
			CheckArnFor:             u.CheckArnFor,
		},
		IamClient: &stubIAM{
			MakeCreateRoleFail:       u.MakeCreateRoleFail,
			MakeGetRoleFail:          u.MakeGetRoleFail,
			CreateRoleDupFound:       u.CreateRoleDupFound,
			MakeAttachRolePolicyFail: u.MakeAttachRolePolicyFail,
			DetachRolePolicyFound:    u.DetachRolePolicyFound,
			DetachRolePolicyFail:     u.DetachRolePolicyFail,
			MakeDeleteRoleFail:       u.MakeDeleteRoleFail,
			DeleteRoleFound:          u.DeleteRoleFound,
		},
	}
	provisioner, err := New(u.InstanceGroup, *aws)

	if err != nil {
		t.Fatal(err)
	}
	u.Provisioner = &provisioner
	return &provisioner
}

func (u *EksFargateUnitTest) Run(t *testing.T) v1alpha1.ReconcileState {

	provisioner := u.BuildProvisioner(t)

	if err := provisioner.CloudDiscovery(); err != nil {
		t.Fatal(err)
	}

	provisioner.StateDiscovery()
	return u.InstanceGroup.GetState()
}
func TestAllStateDiscovery(t *testing.T) {
	type args struct {
		description  string
		profileState *string
		isDeleting   bool
	}
	testFunction := func(t *testing.T, args args) v1alpha1.ReconcileState {
		ig := FakeIG{IsDeleting: args.isDeleting, CurrentState: string(v1alpha1.ReconcileInit)}
		testCase := EksFargateUnitTest{
			Description:   args.description,
			InstanceGroup: ig.getInstanceGroup(),
			ProfileFromDescribe: &eks.FargateProfile{
				Status: args.profileState,
			},
		}
		return testCase.Run(t)
	}
	tests := []struct {
		name string
		args args
		want v1alpha1.ReconcileState
	}{
		{
			name: "TestStateDiscoveryReconcileModified",
			args: args{
				description:  "StateDiscovery - when a profile already exist (active), state should be InitUpdate",
				profileState: aws.String(eks.FargateProfileStatusActive),
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileInitUpdate,
		},
		{
			name: "TestStateDiscoveryInitCreate",
			args: args{
				description:  "StateDiscovery - when a profile NOT exist , state should be ReconcileInitCreate",
				profileState: nil,
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileInitCreate,
		},
		{
			name: "TestStateDiscoveryReconcileModifying1",
			args: args{
				description:  "StateDiscovery - when a profile is CREATING, state should be ReconcileModifying",
				profileState: aws.String(eks.FargateProfileStatusCreating),
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileModifying,
		},
		{
			name: "TestStateDiscoveryReconcileModifying2",
			args: args{
				description:  "StateDiscovery - when a profile is DELETING, state should be ReconcileModifying",
				profileState: aws.String(eks.FargateProfileStatusDeleting),
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileModifying,
		},
		{
			name: "TestStateDiscoveryErr1",
			args: args{
				description:  "StateDiscovery - resource creation state and the profile is DELETE_FAILED, state should be ReconcileErr",
				profileState: aws.String(eks.FargateProfileStatusDeleteFailed),
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileErr,
		},
		{
			name: "TestStateDiscoveryReconcileRecoverableUpdate",
			args: args{
				description:  "StateDiscovery - resource creation state and the profile is CREATE_FAILED, state should be ReconcileErr",
				profileState: aws.String(eks.FargateProfileStatusCreateFailed),
				isDeleting:   false,
			},
			want: v1alpha1.ReconcileInitDelete,
		},
		{
			name: "TestStateDiscoveryReconcileDeleted",
			args: args{
				description:  "StateDiscovery - resource is deleting state and no profile, state should be ReconcileDeleted",
				profileState: nil,
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileDeleted,
		},
		{
			name: "TestStateDiscoveryReconcileInitDelete",
			args: args{
				description:  "StateDiscovery - resource is deleting state and the profile is ACTIVE, state should be ReconcileInitDelete",
				profileState: aws.String(eks.FargateProfileStatusActive),
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileInitDelete,
		},
		{
			name: "TestStateDiscoveryReconcileDeleting1",
			args: args{
				description:  "StateDiscovery - resource is deleting state and the profile is CREATING, state should be ReconcileDeleting",
				profileState: aws.String(eks.FargateProfileStatusCreating),
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileDeleting,
		},
		{
			name: "TestStateDiscoveryReconcileDeleting2",
			args: args{
				description:  "StateDiscovery - resource is deleting state and the profile is DELETING, state should be ReconcileDeleting",
				profileState: aws.String(eks.FargateProfileStatusDeleting),
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileDeleting,
		},
		{
			name: "TestStateDiscoveryDeleteWithRecoverableUpate",
			args: args{
				description:  "StateDiscovery - resource delete state and the profile is CREATE_FAILED, state should be ReconcileErr",
				profileState: aws.String(eks.FargateProfileStatusCreateFailed),
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileErr,
		},
		{
			name: "TestStateDiscoveryErr2",
			args: args{
				description:  "StateDiscovery - resource delete state and the profile is DELETE_FAILED, state should be ReconcileErr",
				profileState: aws.String(eks.FargateProfileStatusDeleteFailed),
				isDeleting:   true,
			},
			want: v1alpha1.ReconcileErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testFunction(t, tt.args)
			if got != tt.want {
				t.Errorf("%v: got %v, want %v", tt.name, got, tt.want)
			}
		})

	}
}
func TestIsReadyPositive(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		InstanceGroup: ig.getInstanceGroup(),
	}
	ctx := testCase.BuildProvisioner(t)
	instanceGroup := ctx.GetInstanceGroup()
	instanceGroup.SetState(v1alpha1.ReconcileModified)
	if !ctx.IsReady() {
		t.Fatal("TestIsReadyPositive: got false, expected: true")
	}
	instanceGroup.SetState(v1alpha1.ReconcileDeleted)
	if !ctx.IsReady() {
		t.Fatal("TestIsReadyPositive: got false, expected: true")
	}
}

func TestIsReadyNegative(t *testing.T) {
	ig := FakeIG{}
	testCase := EksFargateUnitTest{
		InstanceGroup: ig.getInstanceGroup(),
	}
	ctx := testCase.BuildProvisioner(t)
	instanceGroup := ctx.GetInstanceGroup()
	instanceGroup.SetState(v1alpha1.ReconcileInit)
	if ctx.IsReady() {
		t.Fatal("TestIsReadyNegative: got true, expected: false")
	}
	instanceGroup.SetState(v1alpha1.ReconcileInitCreate)
	if ctx.IsReady() {
		t.Fatal("TestIsReadyNegative: got true, expected: false")
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
	if ctx.UpgradeNodes() == nil {
		t.Fatal("TestUpgradeNodes: expected an exception but did not get one.")
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
func TestCreateFargateTags(t *testing.T) {
	input := []map[string]string{{"key1": "value1"}}
	output := CreateFargateTags(input)
	if len(output) != 1 || *output["key1"] != "value1" {
		t.Fatalf("TestCreateFargateTags: output is %v", output)
	}
}
func TestCreateFargateSubnets(t *testing.T) {
	subnets := []string{"subnet1", "subnet2", "subnet3"}
	output := CreateFargateSubnets(subnets)
	if len(output) != 3 {
		t.Fatalf("TestCreateFargateTags: output length is %v", len(output))
	}
	for i, _ := range output {
		if *output[i] != subnets[i] {
			t.Fatalf("TestCreateFargateTags: bad subnet. Got %v", *output[i])
		}
	}
}
func TestCloudDiscoveryFailureGettingProfile(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{InstanceGroup: instanceGroup,
		MakeDescribeProfileFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	ctx.CloudDiscovery()
	if ctx.GetDiscoveredState().GetProfileStatus() != awsprovider.FargateProfileStatusMissing {
		t.Fatalf("TestGetStateFailureGettingProfile: expected nil but got a status")
	}
}
func TestCloudDiscoverySuccessGettingProfile(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{InstanceGroup: instanceGroup,
		ProfileFromDescribe: &eks.FargateProfile{
			Status: aws.String(eks.FargateProfileStatusActive),
		},
	}
	ctx := testCase.BuildProvisioner(t)
	ctx.CloudDiscovery()

	if ctx.GetDiscoveredState().GetProfileStatus() == awsprovider.FargateProfileStatusMissing {
		t.Fatalf("TestGetStateSuccessGettingProfile: expected profile but got a nil")
	}
}
func TestCreateWithSuppliedArnSuccessProfileCreation(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn("a:b:c:d:e:f") // no arn
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
		CheckArnFor:   "a:b:c:d:e:f",
	}
	ctx := testCase.BuildProvisioner(t)
	if ctx.Create() != nil {
		t.Fatal("TestCreateWithSuppliedArnSuccessProfileCreation: got error, expected: nil")
	}
}
func TestCreateWithoutArnCreateRoleFail(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:      instanceGroup,
		MakeCreateRoleFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err == nil {
		t.Fatal("TestCreateWithoutArnProfileCreation: expected error got nil")
	}
	if err.Error() != "create role failed" {
		t.Fatalf("TestCreateWithoutArnProfileCreation: Unexpected error. Got %v", err)
	}
}
func TestCreateWithoutArnCreateRoleFindsDup(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:      instanceGroup,
		CreateRoleDupFound: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err != nil {
		t.Fatal("TestCreateWithoutArnCreateRoleFindsDup: expected nil")
	}
}
func TestCreateWithoutArnAttachRolePolicyFails(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:            instanceGroup,
		CreateRoleDupFound:       true,
		MakeAttachRolePolicyFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err == nil {
		t.Fatal("TestCreateWithoutArnAttachRolePolicyFails: expected error")
	}
	if err.Error() != "attach role policy failed" {
		t.Fatalf("TestCreateWithoutArnAttachRolePolicyFails: bad error message.  Got %v", err.Error())
	}
}
func TestCreateWithoutArnGetRoleFailed(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:      instanceGroup,
		CreateRoleDupFound: true,
		MakeGetRoleFail:    true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err == nil {
		t.Fatal("TestCreateWithoutArnGetRoleFailed: expected error got nil")
	}
	if err.Error() != "get role failed" {
		t.Fatalf("TestCreateWithoutArnGetRoleFailed: bad error message.  Got %v", err.Error())
	}
}
func TestCreateWithoutArnCreateProfileSucceeds(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:      instanceGroup,
		CreateRoleDupFound: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err != nil {
		t.Fatal("TestCreateWithoutArnCreateProfileSucceeds: expected nil")
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileModifying {
		t.Fatalf("TestCreateWithoutArnCreateProfileSucceeds: expected ReconcileModifying state.  Got %v", instanceGroup.GetState())
	}
}
func TestCreateWithoutArnCreateProfileFails(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		CreateRoleDupFound:    true,
		MakeCreateProfileFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err == nil {
		t.Fatal("TestCreateWithoutArnCreateProfileFails: expected error")
	}
	if err.Error() != "create profile failed" {
		t.Fatalf("TestCreateWithoutArnCreateProfileFails: Bad error message.  Got %v", err.Error())
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestCreateWithoutArnCreateProfileFails: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestCreateProfileWithRetry(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:          instanceGroup,
		CreateRoleDupFound:     true,
		MakeCreateProfileRetry: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Create()
	if err != nil {
		t.Fatalf("TestCreateProfileWithRetry: expected nil.  Got %v", err)
	}
}
func TestUpdate(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Update()
	if err != nil {
		t.Fatalf("TestUpdate: expected nil but got error: %v", err)
	}
}
func TestUpdate1(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.SetAnnotations(map[string]string{LastAppliedConfigurationKey: "blah"})
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Update()
	if err == nil {
		t.Fatal("TestUpdate1: error but got nil")
	}
	if err.Error() != "update not supported" {
		t.Fatalf("TestUpdate1: bad error message.  Got %v", err.Error())
	}
}
func TestDeleteWithArnDeleteProfileSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn("a:b:c:d:e:f") // no arn
	testCase := EksFargateUnitTest{
		InstanceGroup: instanceGroup,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err != nil {
		t.Fatalf("TestDeleteWithArnDeleteProfileSuccess: expected nil.  Got %v", err)
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileDeleting {
		t.Fatalf("TestDeleteWithArnDeleteProfileSuccess: expected ReconcileDeleting state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithArnDeleteProfileFail(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	instanceGroup.Spec.EKSFargateSpec.SetPodExecutionRoleArn("a:b:c:d:e:f") // no arn
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		MakeDeleteProfileFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err == nil {
		t.Fatal("TestDeleteWithArnDeleteProfileFail: expected error.  Got nil")
	}
	if err.Error() != "delete profile failed" {
		t.Fatalf("TestDeleteWithArnDeleteProfileFail: bad error message.  Got %v", err.Error())
	}

	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithArnDeleteProfileFail: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDetachPolicyFromRoleCreated(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		DetachRolePolicyFound: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err != nil {
		t.Fatalf("TestDeleteWithoutArnDetachPolicyFromRoleCreated: expected nil.  Got %v", err)
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithoutArnDetachPolicyFromRoleCreated: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDetachPolicyFromRoleFailed(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:        instanceGroup,
		DetachRolePolicyFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err == nil {
		t.Fatal("TestDeleteWithoutArnDetachPolicyFromRoleFailed: expected error.  Got nil")
	}
	if err.Error() != "detach role policy failed" {
		t.Fatalf("TestDeleteWithoutArnDetachPolicyFromRoleFailed: bad error.  Got %v", err.Error())
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithoutArnDetachPolicyFromRoleFailed: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDeleteRoleFailed(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		DetachRolePolicyFound: false,
		MakeDeleteRoleFail:    true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err == nil {
		t.Fatal("TestDeleteWithoutArnDeleteRoleFailed: expected error.  Got nil")
	}
	if err.Error() != "delete role failed" {
		t.Fatalf("TestDeleteWithoutArnDeleteRoleFailed: bad error.  Got %v", err.Error())
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithoutArnDeleteRoleFailed: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDeleteRoleCreated(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		DetachRolePolicyFound: false,
		DeleteRoleFound:       true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err != nil {
		t.Fatalf("TestDeleteWithoutArnDeleteRoleCreated: expected nil.  Got %v", err)
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithoutArnDeleteRoleCreated: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDeleteProfileFailed(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		DetachRolePolicyFound: false,
		DeleteRoleFound:       false,
		MakeDeleteProfileFail: true,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err == nil {
		t.Fatal("TestDeleteWithoutArnDeleteProfileFailed: expected error.  Got nil")
	}
	if err.Error() != "delete profile failed" {
		t.Fatalf("TestDeleteWithoutArnDeleteProfileFailed: bad error message.  Got %v", err.Error())
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileInit {
		t.Fatalf("TestDeleteWithoutArnDeleteProfileFailed: expected ReconcileInit state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithoutArnDeleteProfileSuccess(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:         instanceGroup,
		DetachRolePolicyFound: false,
		DeleteRoleFound:       false,
		MakeDeleteProfileFail: false,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err != nil {
		t.Fatalf("TestDeleteWithoutArnDeleteProfileSuccess: expected nil.  Got %v", err)
	}
	if instanceGroup.GetState() != v1alpha1.ReconcileDeleting {
		t.Fatalf("TestDeleteWithoutArnDeleteProfileSuccess: expected ReconcileDeleting state.  Got %v", instanceGroup.GetState())
	}
}
func TestDeleteWithRetry(t *testing.T) {
	ig := FakeIG{}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Spec.EKSFargateSpec.SetClusterName("DinahCluster")
	instanceGroup.Spec.EKSFargateSpec.SetProfileName("DinahProfile")
	testCase := EksFargateUnitTest{
		InstanceGroup:          instanceGroup,
		MakeDeleteProfileRetry: true,
		DetachRolePolicyFound:  false,
		DeleteRoleFound:        false,
	}
	ctx := testCase.BuildProvisioner(t)
	err := ctx.Delete()
	if err != nil {
		t.Fatalf("TestDeleteWithRetry: expected nil.  Got %v", err)
	}
}
