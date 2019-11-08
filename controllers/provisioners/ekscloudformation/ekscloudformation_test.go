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
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
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
  name: rollingupgrade.upgrademgr.keikoproj.io
spec:
  group: upgrademgr.keikoproj.io
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
	var rollupSpec = fmt.Sprintf(`apiVersion: upgrademgr.keikoproj.io/v1alpha1
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
	StackUpdateNeeded     bool
	AuthConfigMapExist    bool
	LoadCRD               string
	AuthConfigMapData     string
	StackState            string
	StackARN              string
	ExistingARNs          []string
	ExistingUnmanagedARNs []string
	ExistingEvents        []*corev1.Event
	VpcID                 string
	ExpectedState         v1alpha1.ReconcileState
	ExpectedCR            int
	ExpectedSpotPrice     string
	ExpectedAuthConfigMap *corev1.ConfigMap
}

type stubCF struct {
	cloudformationiface.CloudFormationAPI
	StackExist        bool
	StackUpdateNeeded bool
	StackState        string
	StackARN          string
	ExistingARNs      []string
	InstanceGroup     *v1alpha1.InstanceGroup
	EksClusterName    string
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
	AsgName       string
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
				StackName:     "stack-name",
				AsgName:       "my-asg",
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
			StackName:     "stack-name",
			AsgName:       "my-asg",
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
	if !s.StackUpdateNeeded {
		var err error
		awsErr := awserr.New("ValidationError", "No updates are to be performed.", err)
		return &cloudformation.UpdateStackOutput{StackId: aws.String("")}, awsErr
	}
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

func getFakeAuthConfigMap(f FakeAuthConfigMap, k kubernetes.Interface, exists bool) *corev1.ConfigMap {
	var data = make(map[string]string, len(f.ARNList))
	data["mapUsers"] = "[]\n"

	m := make(map[string]bool)
	for _, item := range f.ARNList {
		if _, ok := m[item]; ok {
			continue
		} else {
			m[item] = true
		}
	}

	var result []string
	for item, _ := range m {
		result = append(result, item)
	}
	f.ARNList = result
	sort.Strings(f.ARNList)

	if len(f.ARNList) != 0 {
		for _, arn := range f.ARNList {
			mapRole := fmt.Sprintf("- rolearn: %v\n  username: system:node:{{EC2PrivateDNSName}}\n  groups:\n  - system:bootstrappers\n  - system:nodes\n", arn)
			data["mapRoles"] += mapRole
		}
	} else {
		data["mapRoles"] = "[]\n"
	}

	if !exists {
		data = nil
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "aws-auth",
		},
		Data: data,
	}
	return cm
}

func createFakeStack(f FakeStack) *cloudformation.Stack {
	var tags []*cloudformation.Tag
	var outputs []*cloudformation.Output

	f.StackTags = []map[string]string{
		{
			"instancegroups.keikoproj.io/ClusterName": f.ClusterName,
		},
		{
			"instancegroups.keikoproj.io/InstanceGroup": f.ResourceName,
		},
		{
			"instancegroups.keikoproj.io/Namespace": f.NamespaceName,
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
			"AsgName": f.AsgName,
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

func getSpotSuggestionEvent(id, scalingGroup, price string, useSpot bool, ts time.Time) *corev1.Event {
	message := fmt.Sprintf(`{"apiVersion":"v1alpha1","spotPrice":"%v", "useSpot": %t}`, price, useSpot)
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("spot-manager-%v.000000000000000", id),
		},
		LastTimestamp: metav1.Time{Time: ts},
		Reason:        "SpotRecommendationGiven",
		Message:       message,
		Type:          "Normal",
		InvolvedObject: corev1.ObjectReference{
			Namespace: "kube-system",
			Name:      scalingGroup,
		},
	}
	return event
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

	clusterName := "EKS-Test"
	stackName := fmt.Sprintf("%v-%v-%v", clusterName, u.InstanceGroup.ObjectMeta.GetNamespace(), u.InstanceGroup.ObjectMeta.GetName())
	aws := awsprovider.AwsWorker{
		StackName: stackName,
		CfClient: &stubCF{
			EksClusterName:    clusterName,
			ExistingARNs:      u.ExistingARNs,
			StackExist:        u.StackExist,
			StackState:        u.StackState,
			StackARN:          u.StackARN,
			InstanceGroup:     u.InstanceGroup,
			StackUpdateNeeded: u.StackUpdateNeeded,
		},
		AsgClient: &stubASG{},
		EksClient: &stubEKS{
			VpcID: u.VpcID,
		},
	}
	bootstrap(client)

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(u.InstanceGroup)
	unstructuredInstanceGroup := &unstructured.Unstructured{
		Object: obj,
	}
	kube.KubeDynamic.Resource(groupVersionResource).Namespace(u.InstanceGroup.GetNamespace()).Create(unstructuredInstanceGroup, metav1.CreateOptions{})

	provisioner, err := New(u.InstanceGroup, kube, aws)
	if err != nil {
		t.Fail()
	}
	u.Provisioner = &provisioner

	if len(u.ExistingEvents) != 0 {
		for _, event := range u.ExistingEvents {
			kube.Kubernetes.CoreV1().Events("kube-system").Create(event)
		}
	}

	u.Provisioner.CloudDiscovery()
	u.Provisioner.StateDiscovery()

	if u.ExpectedAuthConfigMap == nil {
		deletionTs := u.InstanceGroup.GetDeletionTimestamp()
		if u.StackExist && deletionTs.IsZero() {
			fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, u.StackARN)
		}
		if len(u.ExistingUnmanagedARNs) != 0 {
			for _, arn := range u.ExistingUnmanagedARNs {
				fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, arn)
			}
		}
		if len(u.ExistingARNs) != 0 {
			instanceGroups := u.Provisioner.DiscoveredState.GetInstanceGroups()
			for i, arn := range u.ExistingARNs {
				fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, arn)
				g := DiscoveredInstanceGroup{
					Name:             fmt.Sprintf("instance-group-%v", i),
					Namespace:        fmt.Sprintf("namespace-%v", i),
					ClusterName:      "eks-cluster",
					StackName:        fmt.Sprintf("stack-%v", i),
					ARN:              arn,
					LaunchConfigName: fmt.Sprintf("launchconfig-%v", i),
					ScalingGroupName: fmt.Sprintf("scalinggroup-%v", i),
					IsClusterMember:  true,
				}
				instanceGroups.AddGroup(g)
			}
		}
		sort.Strings(fakeAuthMap.ARNList)
		u.ExpectedAuthConfigMap = getFakeAuthConfigMap(fakeAuthMap, client, u.AuthConfigMapExist)
	}

	if u.AuthConfigMapExist {
		for _, arn := range u.ExistingUnmanagedARNs {
			fakeAuthMap.ARNList = append(fakeAuthMap.ARNList, arn)
		}
		cm := getFakeAuthConfigMap(fakeAuthMap, client, true)
		createConfigMap(client, cm)
	} else {
		cm := getFakeAuthConfigMap(fakeAuthMap, client, false)
		createConfigMap(client, cm)
	}

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

	spotPrice := u.Provisioner.InstanceGroup.Spec.EKSCFSpec.EKSCFConfiguration.SpotPrice
	if spotPrice != u.ExpectedSpotPrice {
		t.Fatalf("SpotPrice, expected:\n %#v, \ngot:\n %#v", u.ExpectedSpotPrice, spotPrice)
	}
}

func TestStateDiscoveryInitUpdate(t *testing.T) {
	ig := FakeIG{
		BootstrapArguments: "--node-labels kubernetes.io/role=node",
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when a stack already exist (idle), state should be InitUpdate",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "CREATE_COMPLETE",
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileModifying(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when a stack already exist (busy), state should be ReconcileModifying",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "UPDATE_IN_PROGRESS",
		ExpectedState:      v1alpha1.ReconcileModifying,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileInitCreate(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when a stack does not exist, state should be ReconcileInitCreate",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         false,
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitCreate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryInitDeleting(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when a stack exist (idle), and resource is deleting state should be InitDelete",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "CREATE_COMPLETE",
		ExpectedState:      v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryReconcileDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when a stack exist (busy), and resource is deleting state should be Deleting",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "DELETE_IN_PROGRESS",
		ExpectedState:      v1alpha1.ReconcileDeleting,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeleted(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when both stack does not exist, and resource is deleting state should be Deleted",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         false,
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}

func TestStateDiscoveryDeletedExist(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when stack-state is finite deleted state should Deleted",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "DELETE_COMPLETE",
		ExpectedState:      v1alpha1.ReconcileDeleted,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUpdateRecoverableError(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when stack-state is update recoverable state should InitUpdate",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "UPDATE_ROLLBACK_COMPLETE",
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUnrecoverableError(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when stack-state is unrecoverable state should Error",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "UPDATE_ROLLBACK_FAILED",
		ExpectedState:      v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUnrecoverableErrorDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:       "StateDiscovery - when stack in err state, allow deletes",
		InstanceGroup:     ig.getInstanceGroup(),
		StackExist:        true,
		StackUpdateNeeded: true,
		StackState:        "ROLLBACK_COMPLETE",
		ExpectedState:     v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryRecoverableErrorDelete(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when stack in err state, allow deletes",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "UPDATE_ROLLBACK_COMPLETE",
		ExpectedState:      v1alpha1.ReconcileInitDelete,
	}
	testCase.Run(t)
}

func TestStateDiscoveryUnrecoverableErrorDeleteFailure(t *testing.T) {
	ig := FakeIG{
		IsDeleting: true,
	}
	testCase := EksCfUnitTest{
		Description:        "StateDiscovery - when stack delete fails state should be Error",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		StackState:         "DELETE_FAILED",
		ExpectedState:      v1alpha1.ReconcileErr,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingCreateConfigMap(t *testing.T) {
	ig := FakeIG{}
	selfARN := "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname"
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when the auth configmap does not exist, it will be created",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: false,
		ExpectedAuthConfigMap: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "aws-auth",
				Namespace: "kube-system",
			},
			Data: map[string]string{
				"mapRoles": fmt.Sprintf("- rolearn: %v\n  username: system:node:{{EC2PrivateDNSName}}\n  groups:\n  - system:bootstrappers\n  - system:nodes\n", selfARN),
				"mapUsers": "[]\n",
			},
		},
		ExpectedState: v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingUpdateConfigMap(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:        "BootstrapNodes - when the auth configmap exist, ARN will be appended to it",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         true,
		StackUpdateNeeded:  true,
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
		StackUpdateNeeded:  true,
		StackARN:           "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname3",
		ExistingARNs:       []string{"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname1", "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname2"},
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
	}
	testCase.Run(t)
}

func TestNodeBootstrappingUpdateConfigMapWithExistingUnmanagedMembers(t *testing.T) {
	ig := FakeIG{}
	testCase := EksCfUnitTest{
		Description:           "BootstrapNodes - when the auth configmap exist, ARN will be appended to it",
		InstanceGroup:         ig.getInstanceGroup(),
		StackExist:            true,
		StackUpdateNeeded:     true,
		StackARN:              "arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname3",
		ExistingARNs:          []string{"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname2"},
		ExistingUnmanagedARNs: []string{"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/groupfriendlyname1"},
		AuthConfigMapExist:    true,
		ExpectedState:         v1alpha1.ReconcileInitUpdate,
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
		Description:        "CRDStrategy - rollup strategy can be submitted successfully",
		LoadCRD:            "rollingupgrade",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         false,
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitCreate,
		ExpectedCR:         1,
	}
	testCase.Run(t)
}

func TestSpotInstancesRecommendationEnable(t *testing.T) {
	ig := FakeIG{
		UpgradeStrategyType:             "crd",
		UpgradeStrategyCRD:              "rollingupgrade",
		UpgradeStrategyCRDSpec:          getRollupSpec("rollup"),
		UpgradeStrategyCRDStatusPath:    "status.currentStatus",
		UpgradeStrategyCRDStatusSuccess: "success",
	}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Status.ActiveScalingGroupName = "my-asg"
	events := []*corev1.Event{
		getSpotSuggestionEvent("1", "my-asg", "0.005", true, time.Now()),
		getSpotSuggestionEvent("2", "my-asg", "0.007", true, time.Now().Add(time.Minute*time.Duration(3))),
		getSpotSuggestionEvent("3", "my-asg", "0.006", true, time.Now().Add(time.Minute*time.Duration(5))),
	}
	testCase := EksCfUnitTest{
		Description:        "Spot Instances - should take latest event's recommendation (enable)",
		LoadCRD:            "rollingupgrade",
		InstanceGroup:      instanceGroup,
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		ExistingEvents:     events,
		ExpectedSpotPrice:  "0.006",
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
		ExpectedCR:         1,
	}
	testCase.Run(t)
}

func TestSpotInstancesRecommendationDisable(t *testing.T) {
	ig := FakeIG{
		UpgradeStrategyType:             "crd",
		UpgradeStrategyCRD:              "rollingupgrade",
		UpgradeStrategyCRDSpec:          getRollupSpec("rollup"),
		UpgradeStrategyCRDStatusPath:    "status.currentStatus",
		UpgradeStrategyCRDStatusSuccess: "success",
	}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Status.ActiveScalingGroupName = "my-asg"
	events := []*corev1.Event{
		getSpotSuggestionEvent("1", "my-asg", "0.005", true, time.Now()),
		getSpotSuggestionEvent("3", "my-asg", "0.006", false, time.Now().Add(time.Minute*time.Duration(6))),
		getSpotSuggestionEvent("2", "my-asg", "0.007", true, time.Now().Add(time.Minute*time.Duration(5))),
	}
	testCase := EksCfUnitTest{
		Description:        "Spot Instances - should take latest event's recommendation (disable)",
		LoadCRD:            "rollingupgrade",
		InstanceGroup:      instanceGroup,
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		ExistingEvents:     events,
		ExpectedSpotPrice:  "",
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
		ExpectedCR:         1,
	}
	testCase.Run(t)
}

func TestSpotInstancesManual(t *testing.T) {
	ig := FakeIG{
		UpgradeStrategyType:             "crd",
		UpgradeStrategyCRD:              "rollingupgrade",
		UpgradeStrategyCRDSpec:          getRollupSpec("rollup"),
		UpgradeStrategyCRDStatusPath:    "status.currentStatus",
		UpgradeStrategyCRDStatusSuccess: "success",
	}
	instanceGroup := ig.getInstanceGroup()
	instanceGroup.Status.ActiveScalingGroupName = "my-asg"
	instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration.SpotPrice = "0.005"
	testCase := EksCfUnitTest{
		Description:        "Spot Instances - should take user input from custom resource",
		LoadCRD:            "rollingupgrade",
		InstanceGroup:      instanceGroup,
		StackExist:         true,
		StackUpdateNeeded:  true,
		AuthConfigMapExist: true,
		ExpectedSpotPrice:  "0.005",
		ExpectedState:      v1alpha1.ReconcileInitUpdate,
		ExpectedCR:         1,
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
		Description:        "CRDStrategy - rollup strategy can be submitted successfully",
		LoadCRD:            "rollingupgrade",
		InstanceGroup:      ig.getInstanceGroup(),
		StackExist:         false,
		AuthConfigMapExist: true,
		ExpectedState:      v1alpha1.ReconcileInitCreate,
		ExpectedCR:         1,
	}
	testCase.Run(t)
}

func TestUpdateAuthConfigMap(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	ctx.fakeBootstrapState()
	ctx.updateAuthConfigMap()
	expectedActiveARNs := []string{
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/discoveredARN",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-1",
		"arn:aws:autoscaling:region:account-id:autoScalingGroup:groupid:autoScalingGroupName/arn-2",
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

func TestStateSetter(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	ctx.SetState(v1alpha1.ReconcileReady)
	if ctx.InstanceGroup.Status.CurrentState != string(v1alpha1.ReconcileReady) {
		t.Fatalf("TestStateSetter: got %v, expected: %v", ctx.InstanceGroup.Status.CurrentState, string(v1alpha1.ReconcileReady))
	}
}

func TestStateGetter(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	ctx.InstanceGroup.Status.CurrentState = string(v1alpha1.ReconcileReady)
	if ctx.GetState() != v1alpha1.ReconcileReady {
		t.Fatalf("TestStateGetter: got %v, expected: %v", string(ctx.GetState()), string(v1alpha1.ReconcileReady))
	}
}

func Test_ControllerConfigLoader(t *testing.T) {
	ctx := getBasicContext(t, blankMocker)
	payload := []byte(`
stackNamePrefix: myOrg
defaultSubnets:
- subnet-12345678
- subnet-23344567
defaultClusterName: myEksCluster
defaultArns:
- MyARN-1
- MyARN-2`)
	ig := ctx.GetInstanceGroup()
	specConfig := &ig.Spec.EKSCFSpec.EKSCFConfiguration
	config, err := LoadControllerConfiguration(ig, payload)
	if err != nil {
		t.Fatal("Test_ControllerConfigLoader expected error not to have occured")
	}

	expectedConfig := EksCfDefaultConfiguration{
		StackNamePrefix: "myOrg",
		DefaultSubnets:  []string{"subnet-12345678", "subnet-23344567"},
		EksClusterName:  "myEksCluster",
		DefaultARNs:     []string{"MyARN-1", "MyARN-2"},
	}

	if !reflect.DeepEqual(config, expectedConfig) {
		t.Fatalf("Test_ControllerConfigLoader: got %+v, expected: %+v", config, expectedConfig)
	}

	if specConfig.GetClusterName() != expectedConfig.EksClusterName {
		t.Fatalf("Test_ControllerConfigLoader: got %v, expected: %v", specConfig.GetClusterName(), expectedConfig.EksClusterName)
	}

	if !reflect.DeepEqual(specConfig.GetSubnets(), expectedConfig.DefaultSubnets) {
		t.Fatalf("Test_ControllerConfigLoader: got %v, expected: %v", specConfig.GetSubnets(), expectedConfig.DefaultSubnets)
	}
}

func TestGetExistingRoleName(t *testing.T) {
	tt := []struct {
		testCase string
		input    string
		expected string
	}{
		{
			testCase: "custom resource creation with no/empty role",
			input:    "",
			expected: "",
		},
		{
			testCase: "custom resource creation with an existing role with prefix",
			input:    "arn:aws:iam::0123456789123:role/some-role",
			expected: "some-role",
		},
		{
			testCase: "custom resource creation with an existing role without prefix",
			input:    "some-role",
			expected: "some-role",
		},
	}

	for _, tc := range tt {
		resp := getExistingRoleName(tc.input)
		if !reflect.DeepEqual(resp, tc.expected) {
			t.Fatalf("Test Case [%s] Failed Expected [%s] Got [%s]\n", tc.testCase, tc.expected, resp)
		}
	}
}
func TestGetManagedPolicyARNs(t *testing.T) {
	tt := []struct {
		testCase string
		input    []string
		expected string
	}{
		{
			testCase: "custom resource creation with no managed policies",
			input:    []string{},
			expected: "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy,arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy,arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
		},
		{
			testCase: "custom resource creation with 1 managed policies",
			input:    []string{"AWSDynamoDBFullAccess"},
			expected: "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy,arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy,arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly,arn:aws:iam::aws:policy/AWSDynamoDBFullAccess",
		},
		{
			testCase: "custom resource creation with multiple managed policies",
			input:    []string{"My-Managed-Policy-1", "My-Managed-Policy-2", "My-Managed-Policy-3"},
			expected: "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy,arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy,arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly,arn:aws:iam::aws:policy/My-Managed-Policy-1,arn:aws:iam::aws:policy/My-Managed-Policy-2,arn:aws:iam::aws:policy/My-Managed-Policy-3",
		},
		{
			testCase: "custom resource creation with managed policy arn instead of name",
			input:    []string{"arn:aws:iam::aws:policy/My-Managed-Policy-1"},
			expected: "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy,arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy,arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly,arn:aws:iam::aws:policy/My-Managed-Policy-1",
		},
		{
			testCase: "custom resource creation with managed policy arn with account number",
			input:    []string{"arn:aws:iam::123456789012:policy/My-Managed-Policy-3"},
			expected: "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy,arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy,arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly,arn:aws:iam::123456789012:policy/My-Managed-Policy-3",
		},
	}

	for _, tc := range tt {
		resp := getManagedPolicyARNs(tc.input)
		if !reflect.DeepEqual(resp, tc.expected) {
			t.Fatalf("Test Case [%s] Failed Expected [%s] Got [%s]\n", tc.testCase, tc.expected, resp)
		}
	}
}

func TestGetNodeAutoScalingGroupMetrics(t *testing.T) {
	tt := []struct {
		testCase string
		input    []string
		expected string
	}{
		{
			testCase: "NodeGroup with no metrics collection",
			input:    []string{},
			expected: "",
		},
		{
			testCase: "NodeGroup metricsCollection with few metrics",
			input:    []string{"groupMinSize", "groupMaxSize"},
			expected: "GroupMinSize,GroupMaxSize",
		},
		{
			testCase: "NodeGroup metricsCollection with all",
			input:    []string{"all"},
			expected: "",
		},
	}

	for _, tc := range tt {
		resp := getNodeAutoScalingGroupMetrics(tc.input)
		if !reflect.DeepEqual(resp, tc.expected) {
			t.Fatalf("Test Case [%s] Failed Expected [%s] Got [%s]\n", tc.testCase, tc.expected, resp)
		}
	}
}

func TestParseCustomResourceYamlEmptyString(t *testing.T) {
	empty, err := common.ParseCustomResourceYaml("")
	if len(empty.Object) > 0 {
		t.Fatalf("Expected ParseCustomResourceYaml to return Unstructured whose Unstructured.object is of length: 0 but"+
			" instead object is of length %d", len(empty.Object))
	}
	if err != nil {
		t.Fatal("Empty YAML string produces error")
	}
}
