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

package ekscloudformation

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/orkaproj/instance-manager/api/v1alpha1"
	"github.com/orkaproj/instance-manager/controllers/common"
	awsprovider "github.com/orkaproj/instance-manager/controllers/providers/aws"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var loggingEnabled bool

var rollupCrd = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: rollingupgrade.upgrademgr.orkaproj.io
spec:
  group: upgrademgr.orkaproj.io
  names:
    kind: RollingUpgrade
    plural: rollups
  scope: Namespaced
  version: v1alpha1
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []`

var blankMocker = awsprovider.AwsWorker{
	StackName: "test",
	CfClient:  &stubCF{},
	AsgClient: &stubASG{},
	EksClient: &stubEKS{},
}

func init() {
	flag.BoolVar(&loggingEnabled, "logging-enabled", false, "Enable Logging")
}

func getRollupSpec(name string) string {
	var rollupSpec = fmt.Sprintf(`apiVersion: upgrademgr.orkaproj.io/v1alpha1
kind: RollingUpgrade
metadata:
  name: %v
spec:
  postDrainDelaySeconds: 90
  nodeIntervalSeconds: 300
  asgName: nodes.mycluster.k8s.local
  postDrainScript: |
    set -ex
    echo "Hello, postDrain!"
  postWaitScript: |
    echo "Hello, postWait"
  postTerminateScript: |
    echo "Hello, postTerminate!"
status:
  currentStatus: running`, name)
	return rollupSpec
}

type EksCfUnitTest struct {
	Description           string
	Provisioner           *EksCfInstanceGroupContext
	InstanceGroup         *v1alpha1.InstanceGroup
	StackExist            bool
	AuthConfigMapExist    bool
	LoadCRD               string
	AuthConfigMapData     string
	StackState            string
	StackARN              string
	ExistingARNs          []string
	VpcID                 string
	ExpectedState         v1alpha1.ReconcileState
	ExpectedCR            int
	ExpectedAuthConfigMap *corev1.ConfigMap
}

type stubCF struct {
	cloudformationiface.CloudFormationAPI
	StackExist     bool
	StackState     string
	StackARN       string
	ExistingARNs   []string
	InstanceGroup  *v1alpha1.InstanceGroup
	EksClusterName string
}

type stubASG struct {
	autoscalingiface.AutoScalingAPI
	ExistingGroups []*autoscaling.Group
}

type stubEKS struct {
	eksiface.EKSAPI
	VpcID string
}

type FakeIG struct {
	Name                            string
	Namespace                       string
	ClusterName                     string
	CurrentState                    string
	UpgradeStrategyType             string
	UpgradeStrategyCRD              string
	UpgradeStrategyCRDSpec          string
	UpgradeStrategyCRDStatusPath    string
	UpgradeStrategyCRDStatusSuccess string
	UpgradeStrategyCRDStatusFail    string
	ConcurrencyPolicy               string
	BootstrapArguments              string
	IsDeleting                      bool
}

type FakeStack struct {
	StackName     string
	ClusterName   string
	ResourceName  string
	NamespaceName string
	StackState    string
	StackARN      string
	StackTags     []map[string]string
	StackOutputs  []map[string]string
}

type FakeAuthConfigMap struct {
	ARNList []string
}

func (s *stubEKS) DescribeCluster(input *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
	output := &eks.DescribeClusterOutput{
		Cluster: &eks.Cluster{
			ResourcesVpcConfig: &eks.VpcConfigResponse{
				VpcId: aws.String(s.VpcID),
			},
		},
	}
	return output, nil
}

func (s *stubCF) DescribeStacks(input *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
	var fakeStack *cloudformation.Stack
	var existingStacks []*cloudformation.Stack

	output := &cloudformation.DescribeStacksOutput{Stacks: []*cloudformation.Stack{}}
	if s.StackState == "" {
		s.StackState = "CREATE_COMPLETE"
	}
	if len(s.ExistingARNs) != 0 {
		for i, arn := range s.ExistingARNs {
			namespaceName := fmt.Sprintf("namespace-%v", strconv.Itoa(i+2))
			instanceGroupName := fmt.Sprintf("instancegroup-%v", strconv.Itoa(i+2))
			fakeStackInput := FakeStack{
				ClusterName:   s.EksClusterName,
				NamespaceName: namespaceName,
				ResourceName:  instanceGroupName,
				StackState:    s.StackState,
				StackARN:      arn,
			}
			existingStack := createFakeStack(fakeStackInput)
			existingStacks = append(existingStacks, existingStack)
		}
		output.Stacks = existingStacks
	}

	if s.StackExist {
		fakeStackInput := FakeStack{
			ClusterName:   s.EksClusterName,
			NamespaceName: s.InstanceGroup.ObjectMeta.Namespace,
			ResourceName:  s.InstanceGroup.ObjectMeta.Name,
			StackState:    s.StackState,
			StackARN:      s.StackARN,
		}
		fakeStack = createFakeStack(fakeStackInput)
		output.Stacks = append(output.Stacks, fakeStack)
	}

	inputStackName := aws.StringValue(input.StackName)
	if inputStackName != "" {
		if s.StackExist {
			currentStackName := aws.StringValue(fakeStack.StackName)
			if inputStackName == currentStackName {
				return &cloudformation.DescribeStacksOutput{Stacks: []*cloudformation.Stack{fakeStack}}, nil
			}
		}
		return &cloudformation.DescribeStacksOutput{Stacks: []*cloudformation.Stack{}}, nil
	}
	return output, nil
}

func (s *stubCF) CreateStack(*cloudformation.CreateStackInput) (*cloudformation.CreateStackOutput, error) {
	return &cloudformation.CreateStackOutput{StackId: aws.String("")}, nil
}

func (s *stubCF) UpdateStack(*cloudformation.UpdateStackInput) (*cloudformation.UpdateStackOutput, error) {
	return &cloudformation.UpdateStackOutput{StackId: aws.String("")}, nil
}

func (s *stubCF) DeleteStack(*cloudformation.DeleteStackInput) (*cloudformation.DeleteStackOutput, error) {
	return &cloudformation.DeleteStackOutput{}, nil
}

func (s *stubASG) DescribeAutoScalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	var groups []*autoscaling.Group
	for _, group := range s.ExistingGroups {
		groups = append(groups, group)
	}
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: groups,
	}
	return output, nil
}

func (s *stubASG) DescribeLaunchConfigurations(*autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	return &autoscaling.DescribeLaunchConfigurationsOutput{}, nil
}

func createInitConfigMap(k kubernetes.Interface) {
	var cm corev1.ConfigMap
	cmFilePath, _ := filepath.Abs("../../../config/crds/eks_cf_cm.yaml")
	cmData, _ := ioutil.ReadFile(cmFilePath)
	yaml.Unmarshal(cmData, &cm.Data)
	cm.Name = "eks-cf-cm"
	cm.Namespace = "kube-system"
	createConfigMap(k, &cm)
}

func createAuthConfigMap(k kubernetes.Interface) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "aws-auth",
		},
		Data: map[string]string{"mapRoles": "[]\n"},
	}
	createConfigMap(k, cm)
}

func createFakeStack(f FakeStack) *cloudformation.Stack {
	var tags []*cloudformation.Tag
	var outputs []*cloudformation.Output

	f.StackTags = []map[string]string{
		{
			"instancegroups.orkaproj.io/ClusterName": f.ClusterName,
		},
		{
			"instancegroups.orkaproj.io/InstanceGroup": f.ResourceName,
		},
		{
			"instancegroups.orkaproj.io/Namespace": f.NamespaceName,
		},
		{
			"EKS_GROUP_ARN": f.StackARN,
		},
	}

	for _, rawTag := range f.StackTags {
		for key, value := range rawTag {
			tag := &cloudformation.Tag{Key: aws.String(key), Value: aws.String(value)}
			tags = append(tags, tag)
		}
	}

	f.StackOutputs = []map[string]string{
		{
			"NodeInstanceRole": f.StackARN,
		},
		{
			"LaunchConfigName": f.StackARN,
		},
		{
			"AsgName": f.StackName,
		},
	}

	for _, rawOutput := range f.StackOutputs {
		for key, value := range rawOutput {
			output := &cloudformation.Output{OutputKey: aws.String(key), OutputValue: aws.String(value)}
			outputs = append(outputs, output)
		}
	}
	stackName := fmt.Sprintf("%v-%v-%v", f.ClusterName, f.NamespaceName, f.ResourceName)
	output := &cloudformation.Stack{
		StackName:   aws.String(stackName),
		StackStatus: aws.String(f.StackState),
		Tags:        tags,
		Outputs:     outputs,
	}
	return output
}

func createFakeAuthConfigMap(f FakeAuthConfigMap, k kubernetes.Interface) *corev1.ConfigMap {
	var mapRoles string
	if len(f.ARNList) == 0 {
		mapRoles = "[]\n"
	}
	for _, arn := range f.ARNList {
		mapRole := fmt.Sprintf("- rolearn: %v\n  username: system:node:{{EC2PrivateDNSName}}\n  groups:\n  - system:bootstrappers\n  - system:nodes\n", arn)
		mapRoles += mapRole
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "aws-auth",
		},
		Data: map[string]string{"mapRoles": mapRoles},
	}
	return cm
}

func bootstrap(k kubernetes.Interface) {
	if !loggingEnabled {
		log.Out = ioutil.Discard
	}
	createInitConfigMap(k)
}

func (ctx *EksCfInstanceGroupContext) fakeBootstrapState() {
	discoveredState := &DiscoveredState{
		InstanceGroups: DiscoveredInstanceGroups{
			Items: []DiscoveredInstanceGroup{
				{
					ARN: "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/discoveredARN",
				},
			},
		},
	}
	ctx.SetDiscoveredState(discoveredState)
	ctx.DefaultARNList = []string{
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-1",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-2",
	}

}

func (f *FakeIG) getInstanceGroup() *v1alpha1.InstanceGroup {
	var deletionTimestamp metav1.Time

	if f.UpgradeStrategyType == "" {
		f.UpgradeStrategyType = "rollingUpdate"
	}
	if f.Name == "" {
		f.Name = "instancegroup-1"
	}
	if f.ConcurrencyPolicy == "" {
		f.ConcurrencyPolicy = "forbid"
	}
	if f.Namespace == "" {
		f.Namespace = "namespace-1"
	}
	if f.ClusterName == "" {
		f.ClusterName = "test-cluster"
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
			Provisioner: "eks-cf",
			EKSCFSpec: v1alpha1.EKSCFSpec{
				MaxSize: 3,
				MinSize: 1,
				EKSCFConfiguration: v1alpha1.EKSCFConfiguration{
					BootstrapArguments: f.BootstrapArguments,
					Tags: []map[string]string{
						{
							"key":   "a-key",
							"value": "a-value",
						},
					},
				},
			},
			AwsUpgradeStrategy: v1alpha1.AwsUpgradeStrategy{
				Type: f.UpgradeStrategyType,
				CRDType: v1alpha1.CRDUpgradeStrategy{
					Spec:                f.UpgradeStrategyCRDSpec,
					ConcurrencyPolicy:   f.ConcurrencyPolicy,
					CRDName:             f.UpgradeStrategyCRD,
					StatusJSONPath:      f.UpgradeStrategyCRDStatusPath,
					StatusSuccessString: f.UpgradeStrategyCRDStatusSuccess,
					StatusFailureString: f.UpgradeStrategyCRDStatusFail,
				},
			},
		},
		Status: v1alpha1.InstanceGroupStatus{
			CurrentState: f.CurrentState,
		},
	}

	return instanceGroup
}

func getBasicContext(t *testing.T, awsMocker awsprovider.AwsWorker) EksCfInstanceGroupContext {
	client := fake.NewSimpleClientset()
	dynScheme := runtime.NewScheme()
	dynClient := fakedynamic.NewSimpleDynamicClient(dynScheme)
	ig := FakeIG{}
	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	ctx, err := New(ig.getInstanceGroup(), kube, awsMocker)
	if err != nil {
		t.Fail()
	}
	return ctx
}

func (u *EksCfUnitTest) Run(t *testing.T) {
	//var mapRoles string
	var fakeAuthMap FakeAuthConfigMap
	var rollupResult *unstructured.UnstructuredList
	client := fake.NewSimpleClientset()
	dynScheme := runtime.NewScheme()
	dynClient := fakedynamic.NewSimpleDynamicClient(dynScheme)

	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	u.VpcID = "vpc-123345"
	u.InstanceGroup.Spec.EKSCFSpec.EKSCFConfiguration.SetSecurityGroups([]string{"sg-122222", "sg-3333333"})
	u.InstanceGroup.Spec.EKSCFSpec.EKSCFConfiguration.SetClusterName("EKS-Test")
	if u.LoadCRD == "rollingupgrade" {
		CRDSchema := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1beta1", Resource: "customresourcedefinitions"}
		CRDSpec, _ := common.ParseCustomResourceYaml(rollupCrd)

		_, err := kube.KubeDynamic.Resource(CRDSchema).Create(CRDSpec, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("error: %v", err)
		}
	}

	if u.StackARN == "" {
		u.StackARN = "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname"
	}

	if u.AuthConfigMapExist {
		createAuthConfigMap(client)
	}

	if u.ExpectedAuthConfigMap == nil {
		if u.StackExist {
			fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, u.StackARN)
		}
		if len(u.ExistingARNs) != 0 {
			for _, arn := range u.ExistingARNs {
				fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, arn)
			}
		}
		sort.Strings(fakeAuthMap.ARNList)
		u.ExpectedAuthConfigMap = createFakeAuthConfigMap(fakeAuthMap, client)
	}
	clusterName := "EKS-Test"
	stackName := fmt.Sprintf("%v-%v-%v", clusterName, u.InstanceGroup.ObjectMeta.GetNamespace(), u.InstanceGroup.ObjectMeta.GetName())
	aws := awsprovider.AwsWorker{
		StackName: stackName,
		CfClient: &stubCF{
			EksClusterName: clusterName,
			ExistingARNs:   u.ExistingARNs,
			StackExist:     u.StackExist,
			StackState:     u.StackState,
			StackARN:       u.StackARN,
			InstanceGroup:  u.InstanceGroup,
		},
		AsgClient: &stubASG{},
		EksClient: &stubEKS{
			VpcID: u.VpcID,
		},
	}
	bootstrap(client)

	provisioner, err := New(u.InstanceGroup, kube, aws)
	if err != nil {
		t.Fail()
	}
	u.Provisioner = &provisioner
	u.Provisioner.CloudDiscovery()
	u.Provisioner.StateDiscovery()

	if u.ExpectedState != u.InstanceGroup.GetState() {
		t.Fatalf("DiscoveredState, expected:\n %#v, \ngot:\n %#v", u.ExpectedState, u.InstanceGroup.GetState())
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitDelete {
		u.Provisioner.Delete()
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitCreate {
		u.Provisioner.Create()
	}

	if u.InstanceGroup.GetState() == v1alpha1.ReconcileInitUpdate {
		u.Provisioner.Update()
	}

	u.Provisioner.UpgradeNodes()
	u.Provisioner.BootstrapNodes()

	cm, err := client.CoreV1().ConfigMaps("kube-system").Get("aws-auth", metav1.GetOptions{})
	if err != nil {
		t.Fail()
	}

	if u.LoadCRD == "rollingupgrade" {
		CRDSpec, _ := common.ParseCustomResourceYaml(getRollupSpec("rollup"))
		apiVersionSplit := strings.Split(CRDSpec.GetAPIVersion(), "/")
		CRDSchema := schema.GroupVersionResource{Group: apiVersionSplit[0], Version: apiVersionSplit[1], Resource: "rollingupgrade"}
		rollupResult, _ = kube.KubeDynamic.Resource(CRDSchema).List(metav1.ListOptions{})
		if len(rollupResult.Items) != u.ExpectedCR {
			t.Fatalf("CR Objects, expected:\n %#v, \ngot:\n %#v", u.ExpectedCR, len(rollupResult.Items))
		}
	}

	if !reflect.DeepEqual(cm, u.ExpectedAuthConfigMap) {
		t.Fatalf("BootstrapNodes, expected:\n %#v, \ngot:\n %#v", u.ExpectedAuthConfigMap.Data, cm.Data)
	}
}

func TestStateDiscoveryInitUpdate(t *testing.T) {
	ig := FakeIG{
		BootstrapArguments: "--node-labels kubernetes.io/role=node",
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when a stack already exist (idle), state should be InitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "CREATE_COMPLETE",
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileModifying(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when a stack already exist (busy), state should be ReconcileModifying",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "UPDATE_IN_PROGRESS",
		ExpectedState: v1alpha1.ReconcileModifying,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileInitCreate(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when a stack does not exist, state should be ReconcileInitCreate",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    false,
		ExpectedState: v1alpha1.ReconcileInitCreate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryInitDeleting(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when a stack exist (idle), and resource is deleting state should be InitDelete",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "CREATE_COMPLETE",
		ExpectedState: v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when a stack exist (busy), and resource is deleting state should be Deleting",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "DELETE_IN_PROGRESS",
		ExpectedState: v1alpha1.ReconcileDeleting,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleted(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when both stack does not exist, and resource is deleting state should be Deleted",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    false,
		ExpectedState: v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeletedExist(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when stack-state is finite deleted state should Deleted",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "DELETE_COMPLETE",
		ExpectedState: v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUpdateRecoverableError(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when stack-state is update recoverable state should InitUpdate",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "UPDATE_ROLLBACK_COMPLETE",
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUnrecoverableError(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when stack-state is unrecoverable state should Error",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "UPDATE_ROLLBACK_FAILED",
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUnrecoverableErrorDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:   "StateDiscovery - when stack delete fails state should be Error",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    true,
		StackState:    "DELETE_FAILED",
		ExpectedState: v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingCreateConfigMap(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when the auth configmap does not exist, it will be created",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		AuthConfigMapExist: false,
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingUpdateConfigMap(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when the auth configmap exist, ARN will be appended to it",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackARN:           "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname",
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingUpdateConfigMapWithExistingMembers(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when the auth configmap exist, ARN will be appended to it",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackARN:           "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname3",
		ExistingARNs:       []string{"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname1", "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname2"},
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingRemoveMembers(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when instance group is deleted, other members are retained",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         false,
		StackARN:           "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname3",
		ExistingARNs:       []string{"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname1", "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname2"},
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}

func TestCrdStrategyCRExist(t *testing.T) {
	ig := FakeIG{
		UpgradeStrategyType:             "crd",
		UpgradeStrategyCRD:              "rollingupgrade",
		UpgradeStrategyCRDSpec:          getRollupSpec("rollup"),
		UpgradeStrategyCRDStatusPath:    "status.currentStatus",
		UpgradeStrategyCRDStatusSuccess: "success",
	}
	testCase := EksCfUnitTest{
		Description:   "CRDStrategy - rollup strategy can be submitted successfully",
		LoadCRD:       "rollingupgrade",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    false,
		ExpectedState: v1alpha1.ReconcileInitCreate,
		ExpectedCR:    1,
	}
	testCase.Run(t)
}

func TestCrdStrategyCRLongName(t *testing.T) {
	ig := FakeIG{
		UpgradeStrategyType:             "crd",
		UpgradeStrategyCRD:              "rollingupgrade",
		UpgradeStrategyCRDSpec:          getRollupSpec(strings.Repeat("a", 65)),
		UpgradeStrategyCRDStatusPath:    "status.currentStatus",
		UpgradeStrategyCRDStatusSuccess: "success",
	}
	testCase := EksCfUnitTest{
		Description:   "CRDStrategy - rollup strategy can be submitted successfully",
		LoadCRD:       "rollingupgrade",
		InstanceGroup: ig.getInstanceGroup(),
		StackExist:    false,
		ExpectedState: v1alpha1.ReconcileInitCreate,
		ExpectedCR:    1,
	}
	testCase.Run(t)
}

func TestGetActiveNodesARN(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	ctx.fakeBootstrapState()

	expectedActiveARNs := []string{
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-1",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-2",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/discoveredARN",
	}
	nodesArn := ctx.getActiveNodeArns()
	if !reflect.DeepEqual(nodesArn, expectedActiveARNs) {
		t.Fatalf("getActiveNodeArns returned: %v, expected: %v", nodesArn, expectedActiveARNs)
	}
}

func TestUpdateAuthConfigMap(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	ctx.fakeBootstrapState()
	ctx.createEmptyNodesAuthConfigMap()
	ctx.updateAuthConfigMap()
	expectedActiveARNs := []string{
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-1",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-2",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/discoveredARN",
	}
	var expectedAuthMap string
	for _, arn := range expectedActiveARNs {
		expectedAuthMap += fmt.Sprintf("- rolearn: %v\n  username: system:node:{{EC2PrivateDNSName}}\n  groups:\n  - system:bootstrappers\n  - system:nodes\n", arn)
	}
	cm, err := ctx.KubernetesClient.Kubernetes.CoreV1().ConfigMaps("kube-system").Get("aws-auth", metav1.GetOptions{})
	if err != nil {
		t.Fatal("createEmptyNodesAuthConfigMap: config map not found")
	}
	if cm.Data["mapRoles"] != expectedAuthMap {
		t.Fatalf("updateAuthConfigMap returned: %v, expected: %v", cm.Data["mapRoles"], expectedAuthMap)
	}
}

func TestIsReadyPositive(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
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
	ctx := getBasicContext(t, blankMocker)
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

func TestIsUpgradeNeededPositive(t *testing.T) {
	mocker := blankMocker
	existingASGs := []*autoscaling.Group{
		{
			AutoScalingGroupName: aws.String("my-scaling-group"),
			Instances: []*autoscaling.Instance{
				{
					LaunchConfigurationName: nil,
					InstanceId:              aws.String("my-instance"),
				},
			},
		},
	}
	mocker.AsgClient = &stubASG{
		ExistingGroups: existingASGs,
	}
	ctx := getBasicContext(t, mocker)
	discoveredState := &DiscoveredState{
		SelfGroup: &DiscoveredInstanceGroup{ScalingGroupName: "my-scaling-group"},
	}
	ctx.SetDiscoveredState(discoveredState)
	if !ctx.IsUpgradeNeeded() {
		t.Fatal("TestIsUpgradeNeededPositive: got false, expected: true")
	}
}

func TestIsUpgradeNeededNegative(t *testing.T) {
	mocker := blankMocker
	existingASGs := []*autoscaling.Group{
		{
			AutoScalingGroupName: aws.String("my-scaling-group"),
			Instances: []*autoscaling.Instance{
				{
					LaunchConfigurationName: aws.String("correct-launch-configuration"),
					InstanceId:              aws.String("my-instance"),
				},
			},
		},
	}
	mocker.AsgClient = &stubASG{
		ExistingGroups: existingASGs,
	}
	ctx := getBasicContext(t, mocker)
	discoveredState := &DiscoveredState{
		SelfGroup: &DiscoveredInstanceGroup{ScalingGroupName: "my-scaling-group"},
	}
	ctx.SetDiscoveredState(discoveredState)
	if ctx.IsUpgradeNeeded() {
		t.Fatal("TestIsUpgradeNeededPositive: got true, expected: false")
	}
}
