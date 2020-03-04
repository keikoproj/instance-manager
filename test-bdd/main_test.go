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
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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

	s.BeforeSuite(func() {
		log.Info("BDD >> trying to delete any existing test instance-groups")
		t.anEKSCluster()
		t.deleteAll()
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
	s.Step(`^I update a resource ([^"]*) with ([^"]*) set to ([^"]*)$`, t.iUpdateResourceWithField)

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
	var (
		keySlice = testutil.DeleteEmpty(strings.Split(key, "."))
	)
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

	unstructured.SetNestedField(updateTarget.UnstructuredContent(), value, keySlice...)

	_, err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Update(updateTarget, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (t *FunctionalTest) theResourceShouldBe(state string) error {
	var (
		exists  bool
		counter int
	)

	exists = true
	for {
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for resource state")
		}
		log.Infof("BDD >> waiting for resource %v/%v to become %v", t.ResourceNamespace, t.ResourceName, state)
		_, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(t.ResourceName, metav1.GetOptions{})
		if err != nil {
			if !kerrors.IsNotFound(err) {
				return err
			}
			log.Infof("BDD >> %v/%v is not found: %v", t.ResourceNamespace, t.ResourceName, err)
			exists = false
		}

		switch state {
		case ResourceStateDeleted:
			if !exists {
				break
			}
		case ResourceStateCreated:
			if exists {
				break
			}
		}
		counter++
		time.Sleep(DefaultWaiterInterval)
	}

	return nil
}

func (t *FunctionalTest) theResourceShouldConvergeToSelector(selector string) error {
	var (
		counter  int
		split    = testutil.DeleteEmpty(strings.Split(selector, "="))
		key      = split[0]
		keySlice = testutil.DeleteEmpty(strings.Split(key, "."))
		value    = split[1]
	)

	for {
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for resource")
		}

		log.Infof("BDD >> waiting for resource %v/%v to converge to %v=%v", t.ResourceNamespace, t.ResourceName, key, value)
		resource, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(t.ResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if val, ok, err := unstructured.NestedString(resource.UnstructuredContent(), keySlice...); ok {
			if err != nil {
				return err
			}
			if strings.ToLower(val) == strings.ToLower(value) {
				break
			}
		}
		counter++
		time.Sleep(DefaultWaiterInterval)
	}

	return nil
}

func (t *FunctionalTest) nodesShouldBe(count int, state string) error {
	return t.waitForNodeCountState(count, state, "", "")
}

func (t *FunctionalTest) nodesShouldBeWithLabel(count int, state, key, value string) error {
	return t.waitForNodeCountState(count, state, key, value)
}

func (t *FunctionalTest) waitForNodeCountState(count int, state, key, value string) error {
	var (
		counter       int
		found         bool
		labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=", t.ResourceName)
	)

	if key != "" {
		labelSelector += fmt.Sprintf(",%v=%v", key, value)
	}

	for {
		var conditionNodes int
		var opts = metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for nodes")
		}
		log.Infof("BDD >> %v/%v waiting for %v nodes to be %v", t.ResourceNamespace, t.ResourceName, count, state)
		nodes, err := t.KubeClient.CoreV1().Nodes().List(opts)
		if err != nil {
			return err
		}

		switch state {
		case NodeStateFound:
			if len(nodes.Items) == count {
				log.Infof("BDD >> %v/%v found %v nodes", t.ResourceNamespace, t.ResourceName, count)
				found = true
			}
		case NodeStateReady:
			for _, node := range nodes.Items {
				if testutil.IsNodeReady(node) {
					conditionNodes++
				}
			}
			if conditionNodes == count {
				log.Infof("BDD >> %v/%v found %v ready nodes", t.ResourceNamespace, t.ResourceName, count)
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

func (t *FunctionalTest) deleteAll() error {
	var deleteFn = func(path string, info os.FileInfo, err error) error {

		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}

		resource, err := testutil.ParseInstanceGroupYaml(path, testutil.NewTemplateArguments())
		if err != nil {
			return err
		}

		t.DynamicClient.Resource(InstanceGroupSchema).Namespace(resource.GetNamespace()).Delete(resource.GetName(), &metav1.DeleteOptions{})
		log.Infof("BDD >> submitted deletion for %v/%v", resource.GetNamespace(), resource.GetName())
		return nil
	}

	var waitFn = func(path string, info os.FileInfo, err error) error {
		var (
			counter int
		)

		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}

		resource, err := testutil.ParseInstanceGroupYaml(path, testutil.NewTemplateArguments())
		if err != nil {
			return err
		}

		for {
			if counter >= DefaultWaiterRetries {
				return errors.New("waiter timed out waiting for deletion")
			}
			log.Infof("BDD >> waiting for resource deletion of %v/%v", resource.GetNamespace(), resource.GetName())
			_, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(resource.GetNamespace()).Get(resource.GetName(), metav1.GetOptions{})
			if err != nil {
				if kerrors.IsNotFound(err) {
					log.Infof("BDD >> resource %v/%v is deleted", resource.GetNamespace(), resource.GetName())
					break
				}
			}
			counter++
			time.Sleep(DefaultWaiterInterval)
		}
		return nil
	}

	if err := filepath.Walk("templates", deleteFn); err != nil {
		return err
	}

	if err := filepath.Walk("templates", waitFn); err != nil {
		return err
	}

	return nil
}
