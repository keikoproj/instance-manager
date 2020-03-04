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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/cucumber/godog/gherkin"
	"github.com/keikoproj/instance-manager/test-bdd/testutil"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type FunctionalTest struct {
	KubeClient        kubernetes.Interface
	DynamicClient     dynamic.Interface
	ResourceName      string
	ResourceNamespace string
}

const (
	OperationCreate = "create"
	OperationUpdate = "update"
	OperationDelete = "delete"

	ResourceStateCreated = "created"
	ResourceStateDeleted = "deleted"

	NodeStateReady = "ready"
	NodeStateFound = "found"

	DefaultWaiterInterval = time.Second * 30
	DefaultWaiterRetries  = 24
)

var InstanceGroupSchema = schema.GroupVersionResource{
	Group:    "instancemgr.keikoproj.io",
	Version:  "v1alpha1",
	Resource: "instancegroups",
}

var opt = godog.Options{
	Output: colors.Colored(os.Stdout),
	Format: "progress",
}

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &opt)
}

func TestMain(m *testing.M) {
	flag.Parse()
	opt.Paths = flag.Args()

	status := godog.RunWithOptions("godogs", func(s *godog.Suite) {
		FeatureContext(s)
	}, opt)

	if st := m.Run(); st > status {
		status = st
	}
	os.Exit(status)
}

func FeatureContext(s *godog.Suite) {
	t := FunctionalTest{}

	s.BeforeFeature(func(f *gherkin.Feature) {
		log.Info("BDD >> trying to delete any existing test instance-groups")
		// TODO: Delete all IGs
	})

	s.AfterStep(func(f *gherkin.Step, err error) {
		time.Sleep(time.Second * 5)
	})

	s.Step(`^an EKS cluster`, t.anEKSCluster)
	s.Step(`^(\d+) nodes should be (found|ready)`, t.nodesShouldBe)
	s.Step(`^(\d+) nodes should be (found|ready) with label ([^"]*) set to ([^"]*)$`, t.nodesShouldBeWithLabel)
	s.Step(`^the resource should be (created|deleted)$`, t.theResourceShouldBe)
	s.Step(`^the resource should converge to selector ([^"]*)$`, t.theResourceShouldConvergeToSelector)
	s.Step(`^I (create|delete) a resource ([^"]*)$`, t.iOperateOnResource)
	s.Step(`^I update a resource with ([^"]*) set to ([^"]*)$`, t.iUpdateResourceWithField)

}

func (t *FunctionalTest) anEKSCluster() error {
	var (
		home, _        = os.UserHomeDir()
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	)

	if exported := os.Getenv("KUBECONFIG"); exported != "" {
		kubeconfigPath = exported
	}

	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return errors.Errorf("BDD >> expected kubeconfig to exist for create operation, '%v'", kubeconfigPath)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatal("Unable to construct dynamic client", err)
	}

	_, err = client.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	t.KubeClient = client
	t.DynamicClient = dynClient

	return nil
}

func (t *FunctionalTest) iOperateOnResource(operation, resource string) error {
	resourcePath := filepath.Join("templates", resource)
	args := testutil.NewTemplateArguments()

	instanceGroup, err := testutil.ParseInstanceGroupYaml(resourcePath, args)
	if err != nil {
		return err
	}

	t.ResourceName = instanceGroup.GetName()
	t.ResourceNamespace = instanceGroup.GetNamespace()

	switch operation {
	case OperationCreate:
		_, err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Create(instanceGroup, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	case OperationDelete:
		err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Delete(t.ResourceName, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *FunctionalTest) iUpdateResourceWithField(resource, key, value string) error {
	resourcePath := filepath.Join("templates", resource)
	args := testutil.NewTemplateArguments()

	instanceGroup, err := testutil.ParseInstanceGroupYaml(resourcePath, args)
	if err != nil {
		return err
	}

	t.ResourceName = instanceGroup.GetName()
	t.ResourceNamespace = instanceGroup.GetNamespace()

	updateTarget, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(t.ResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	unstructured.SetNestedField(updateTarget.Object, value, strings.Split(key, ".")...)

	_, err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Update(updateTarget, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (t *FunctionalTest) theResourceShouldBe(state string) error {
	var (
		exists bool
	)
	log.Infof("BDD >> checking if resource %v is %v", t.ResourceName, state)
	_, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(t.ResourceName, metav1.GetOptions{})
	if err != nil {
		log.Infof("BDD >> %v is not found: %v", t.ResourceName, err)
		exists = false
	}

	switch state {
	case ResourceStateDeleted:
		if exists {
			return errors.Errorf("expected resource '%v' to be %v", t.ResourceName, ResourceStateDeleted)
		}
	case ResourceStateCreated:
		if !exists {
			return errors.Errorf("expected resource '%v' to be %v", t.ResourceName, ResourceStateCreated)
		}
	}

	return nil
}

func (t *FunctionalTest) theResourceShouldConvergeToSelector(key, value string) error {
	var (
		counter int
	)

	for {
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for resource")
		}

		log.Infof("BDD >> waiting for resource %v to converge to %v=%v", t.ResourceName, key, value)
		resource, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(t.ResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if val, ok, err := unstructured.NestedString(resource.Object, strings.Split(key, ".")...); ok {
			if err != nil {
				return err
			}
			if val == value {
				break
			}
		}
		counter++
		time.Sleep(DefaultWaiterInterval)
	}

	return nil
}

func (t *FunctionalTest) nodesShouldBe(count int, state string) error {
	var (
		counter       int
		found         bool
		labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=", t.ResourceName)
	)
	for {
		var conditionNodes int
		var opts = metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for nodes")
		}
		log.Infof("BDD >> waiting for %v nodes to be %v", count, state)
		nodes, err := t.KubeClient.CoreV1().Nodes().List(opts)
		if err != nil {
			return err
		}

		switch state {
		case NodeStateFound:
			if len(nodes.Items) == count {
				log.Infof("BDD >> found %v nodes", count)
				found = true
			}
		case NodeStateReady:
			for _, node := range nodes.Items {
				if testutil.IsNodeReady(node) {
					conditionNodes++
				}
			}
			if conditionNodes == count {
				log.Infof("BDD >> found %v ready nodes", count)
				found = true
			}
		}

		if found {
			break
		}

		counter++
		time.Sleep(DefaultWaiterInterval)
	}
	return nil
}

// TODO: Merge functions
func (t *FunctionalTest) nodesShouldBeWithLabel(count int, state, key, value string) error {
	var (
		counter       int
		found         bool
		labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=,%v=%v", t.ResourceName, key, value)
	)
	for {
		var conditionNodes int
		var opts = metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for nodes")
		}
		log.Infof("BDD >> waiting for %v nodes with labels %v to be %v", count, labelSelector, state)
		nodes, err := t.KubeClient.CoreV1().Nodes().List(opts)
		if err != nil {
			return err
		}

		switch state {
		case NodeStateFound:
			if len(nodes.Items) == count {
				log.Infof("BDD >> found %v nodes", count)
				found = true
			}
		case NodeStateReady:
			for _, node := range nodes.Items {
				if testutil.IsNodeReady(node) {
					conditionNodes++
				}
			}
			if conditionNodes == count {
				log.Infof("BDD >> found %v ready nodes", count)
				found = true
			}
		}

		if found {
			break
		}

		counter++
		time.Sleep(DefaultWaiterInterval)
	}
	return nil
}
