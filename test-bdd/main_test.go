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

package main

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	awsprovider "github.com/orkaproj/instance-manager/controllers/providers/aws"
	"github.com/orkaproj/instance-manager/test-bdd/testutil"
	"github.com/sirupsen/logrus"
	apiextcs "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type ClientSet struct {
	cloudformationClient cloudformationiface.CloudFormationAPI
	kube                 kubernetes.Interface
	kubeDynamic          dynamic.Interface
	kubeApiextcs         apiextcs.Interface
}

const (
	TemplateRolling     = "./templates/instance-group.yaml"
	TemplateCRDStrategy = "./templates/instance-group-crd.yaml"
	CRDManifest         = "../config/crd/bases/instancemgr.orkaproj.io_instancegroups.yaml"
)

var (
	log                  = logrus.New()
	EKSClusterName       = flag.String("eks-cluster", "my-eks-cluster", "The name of the EKS Cluster")
	EKSClusterRegionName = flag.String("aws-region", "us-west-2", "The region of the EKS Cluster")
	KubeconfigPath       = flag.String("kubeconfig", "~/.kube/config", "Path to kubeconfig file")
	KeyPairName          = flag.String("keypair-name", "MyKeyPair", "Name of ec2 keypair")
	VPCID                = flag.String("vpc-id", "", "VPC ID to use")
	AMIIDStable          = flag.String("ami-id-stable", "", "Previous version AMI")
	AMIIDLatest          = flag.String("ami-id-latest", "", "Current version AMI")
	Subnets              = flag.String("subnets", "", "List of comma separated subnets")
	SecurityGroups       = flag.String("security-groups", "", "List of comma separated security groups")
	Args                 testutil.TemplateArguments
	clientSet            = newClientSet()
)

func TestE2e(t *testing.T) {
	Args.ClusterName = EKSClusterName
	Args.KeyPairName = KeyPairName
	Args.VpcID = VPCID
	Args.NodeSecurityGroups = strings.Split(*SecurityGroups, ",")
	Args.Subnets = strings.Split(*Subnets, ",")
	Args.AmiID = AMIIDStable
	RegisterFailHandler(Fail)
	junitReporter := reporters.NewJUnitReporter("junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t, "InstanceGroup Type Suite", []Reporter{junitReporter})
}

func newClientSet() ClientSet {
	var clientSet ClientSet
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *KubeconfigPath)
	if err != nil {
		log.Fatal("Unable to get client configuration: ", err)
	}

	extClient, err := apiextcs.NewForConfig(config)
	if err != nil {
		log.Fatal("Unable to construct extensions client", err)
	}
	clientSet.kubeApiextcs = extClient

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatal("Unable to construct dynamic client", err)
	}
	clientSet.kubeDynamic = dynClient

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal("Unable to construct kubernetes client", err)
	}

	clientSet.kube = kubeClient
	clientSet.cloudformationClient = awsprovider.GetAwsCloudformationClient(*EKSClusterRegionName)
	return clientSet
}

func getStackName(o *unstructured.Unstructured) string {
	return fmt.Sprintf("%v-%v-%v", *EKSClusterName, o.GetNamespace(), o.GetName())
}

var _ = Describe("instance-manager is installed", func() {
	// Pre functional test
	It("should create CRD", func() {
		err := testutil.CreateCRD(clientSet.kubeApiextcs, CRDManifest)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("EKSCF InstanceGroups CRUD operations are succesfull", func() {

	// CRUD Create
	It("should create instance groups", func() {
		var crdExpectedLabel = "bdd-test-crd"
		var rollingExpectedLabel = "bdd-test-rolling"
		var expectedReadyCount = 3

		// Create instance-groups of crd / rollingUpdate strategy type
		crdInstanceGroup, err := testutil.CreateUpdateInstanceGroup(clientSet.kubeDynamic, TemplateCRDStrategy, Args)
		Expect(err).NotTo(HaveOccurred())
		crdStackName := getStackName(crdInstanceGroup)

		rollingInstanceGroup, err := testutil.CreateUpdateInstanceGroup(clientSet.kubeDynamic, TemplateRolling, Args)
		Expect(err).NotTo(HaveOccurred())
		rollingStackName := getStackName(rollingInstanceGroup)

		// Nodes should join the cluster within reasonable time
		crdResult := testutil.WaitForNodesCreate(clientSet.kube, crdExpectedLabel, expectedReadyCount)
		Expect(crdResult).Should(BeTrue())

		rollingResult := testutil.WaitForNodesCreate(clientSet.kube, rollingExpectedLabel, expectedReadyCount)
		Expect(rollingResult).Should(BeTrue())

		// InstanceGroup status should be "Ready"
		crdReadiness := testutil.WaitForInstanceGroupReadiness(clientSet.kubeDynamic, crdInstanceGroup.GetNamespace(), crdInstanceGroup.GetName())
		Expect(crdReadiness).Should(BeTrue())

		rollingReadiness := testutil.WaitForInstanceGroupReadiness(clientSet.kubeDynamic, rollingInstanceGroup.GetNamespace(), rollingInstanceGroup.GetName())
		Expect(rollingReadiness).Should(BeTrue())

		// Stacks should be CREATE_COMPLETE
		crdStackStatus := testutil.GetStackState(clientSet.cloudformationClient, crdStackName)
		Expect(crdStackStatus).Should(Equal("CREATE_COMPLETE"))

		rollingStackStatus := testutil.GetStackState(clientSet.cloudformationClient, rollingStackName)
		Expect(rollingStackStatus).Should(Equal("CREATE_COMPLETE"))
	})

	// CRUD Update
	It("should update instance groups", func() {
		var workflowName string
		var workflowNamespace = "instance-manager"
		var workflowPath = []string{"status", "strategyResourceName"}
		var rollingExpectedLabel = "bdd-test-rolling"
		var crdExpectedLabel = "bdd-test-crd"

		Args.AmiID = AMIIDLatest

		// Update InstanceGroups to new AMI
		crdInstanceGroup, err := testutil.CreateUpdateInstanceGroup(clientSet.kubeDynamic, TemplateCRDStrategy, Args)
		Expect(err).NotTo(HaveOccurred())

		rollingInstanceGroup, err := testutil.CreateUpdateInstanceGroup(clientSet.kubeDynamic, TemplateRolling, Args)
		Expect(err).NotTo(HaveOccurred())

		crdStackName := getStackName(crdInstanceGroup)
		rollingStackName := getStackName(rollingInstanceGroup)

		// crd strategy should create workflow and expose it's name in status
		workflowName, err = testutil.WaitForInstanceGroupString(clientSet.kubeDynamic, crdInstanceGroup.GetNamespace(), crdInstanceGroup.GetName(), workflowPath...)
		Expect(err).NotTo(HaveOccurred())

		// Workflow is created
		wfCreation := testutil.WaitForWorkflowCreation(clientSet.kubeDynamic, workflowNamespace, workflowName)
		Expect(wfCreation).Should(BeTrue())

		// Nodes should be replaced within reasonable time
		rollingUpgrade := testutil.WaitForNodesRotate(clientSet.kube, rollingExpectedLabel)
		Expect(rollingUpgrade).Should(BeTrue())

		workflowUpgrade := testutil.WaitForNodesRotate(clientSet.kube, crdExpectedLabel)
		Expect(workflowUpgrade).Should(BeTrue())

		// Wait for workflow success
		wfStatus := testutil.WaitForWorkflowSuccess(clientSet.kubeDynamic, workflowNamespace, workflowName)
		Expect(wfStatus).Should(BeTrue())

		// InstanceGroup CR Status should be Ready
		rollingReadiness := testutil.WaitForInstanceGroupReadiness(clientSet.kubeDynamic, rollingInstanceGroup.GetNamespace(), rollingInstanceGroup.GetName())
		Expect(rollingReadiness).Should(BeTrue())

		crdReadiness := testutil.WaitForInstanceGroupReadiness(clientSet.kubeDynamic, crdInstanceGroup.GetNamespace(), crdInstanceGroup.GetName())
		Expect(crdReadiness).Should(BeTrue())

		// Stacks should be UPDATE_COMPLETE
		crdStackStatus := testutil.GetStackState(clientSet.cloudformationClient, crdStackName)
		Expect(crdStackStatus).Should(Equal("UPDATE_COMPLETE"))

		rollingStackStatus := testutil.GetStackState(clientSet.cloudformationClient, rollingStackName)
		Expect(rollingStackStatus).Should(Equal("UPDATE_COMPLETE"))

	})

	// CRUD Delete
	It("should delete an InstanceGroup with crd strategy", func() {
		var crdExpectedLabel = "bdd-test-crd"
		var rollingExpectedLabel = "bdd-test-rolling"

		// Delete instance groups
		crdInstanceGroup, err := testutil.DeleteInstanceGroup(clientSet.kubeDynamic, TemplateCRDStrategy, Args)
		Expect(err).NotTo(HaveOccurred())

		rollingInstanceGroup, err := testutil.DeleteInstanceGroup(clientSet.kubeDynamic, TemplateRolling, Args)
		Expect(err).NotTo(HaveOccurred())

		crdStackName := getStackName(crdInstanceGroup)
		rollingStackName := getStackName(rollingInstanceGroup)

		// Nodes should be removed from the cluster within reasonable time
		crdDelete := testutil.WaitForNodesDelete(clientSet.kube, crdExpectedLabel)
		Expect(crdDelete).Should(BeTrue())

		rollingDelete := testutil.WaitForNodesDelete(clientSet.kube, rollingExpectedLabel)
		Expect(rollingDelete).Should(BeTrue())

		// InstanceGroup CR should be deleted
		crdDeleted := testutil.WaitForInstanceGroupDeletion(clientSet.kubeDynamic, crdInstanceGroup.GetNamespace(), crdInstanceGroup.GetName())
		Expect(crdDeleted).Should(BeTrue())

		rollingDeleted := testutil.WaitForInstanceGroupDeletion(clientSet.kubeDynamic, rollingInstanceGroup.GetNamespace(), rollingInstanceGroup.GetName())
		Expect(rollingDeleted).Should(BeTrue())

		// Cloudformation Stack should not exist
		crdStackExist := testutil.IsStackExist(clientSet.cloudformationClient, crdStackName)
		Expect(crdStackExist).Should(BeFalse())

		rollingStackExist := testutil.IsStackExist(clientSet.cloudformationClient, rollingStackName)
		Expect(rollingStackExist).Should(BeFalse())
	})
})
