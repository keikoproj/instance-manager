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
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/keikoproj/instance-manager/test-bdd/testutil"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type FunctionalTest struct {
	KubeClient        kubernetes.Interface
	DynamicClient     dynamic.Interface
	RESTConfig        *rest.Config
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

	DefaultWaiterInterval = time.Second * 15
	DefaultWaiterRetries  = 80
)

var InstanceGroupSchema = schema.GroupVersionResource{
	Group:    "instancemgr.keikoproj.io",
	Version:  "v1alpha1",
	Resource: "instancegroups",
}

var opt = godog.Options{
	Output: colors.Colored(os.Stdout),
	Format: "pretty",
}

func init() {
	godog.BindCommandLineFlags("godog.", &opt)
}

func TestMain(m *testing.M) {
	flag.Parse()
	opt.Paths = flag.Args()

	status := godog.TestSuite{
		Name:                 "godogs",
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &opt,
	}.Run()

	if st := m.Run(); st > status {
		status = st
	}
	os.Exit(status)
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	t := FunctionalTest{}

	ctx.BeforeSuite(func() {
		log.Info("BDD >> trying to delete any existing test instance-groups")
		if err := t.anEKSCluster(); err != nil {
			log.Errorf("BDD >> failed to setup EKS cluster: %v", err)
		}
		if err := t.deleteAll(); err != nil {
			log.Errorf("BDD >> failed to delete resources: %v", err)
		}
	})

	ctx.AfterSuite(func() {
		log.Info("BDD >> trying to delete any existing test instance-groups")
		if err := t.anEKSCluster(); err != nil {
			log.Errorf("BDD >> failed to setup EKS cluster: %v", err)
		}
		if err := t.deleteAll(); err != nil {
			log.Errorf("BDD >> failed to delete resources: %v", err)
		}
	})
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	t := FunctionalTest{}

	ctx.AfterStep(func(step *godog.Step, err error) {
		time.Sleep(time.Second * 5)
	})

	// Order matters
	ctx.Step(`^an EKS cluster`, t.anEKSCluster)
	ctx.Step(`^(\d+) nodes should be (found|ready)`, t.nodesShouldBe)
	ctx.Step(`^(\d+) nodes should be (found|ready) with label ([^"]*) set to ([^"]*)$`, t.nodesShouldBeWithLabel)
	ctx.Step(`^the resource should be (created|deleted)$`, t.theResourceShouldBe)
	ctx.Step(`^the resource should converge to selector ([^"]*)$`, t.theResourceShouldConvergeToSelector)
	ctx.Step(`^the resource condition ([^"]*) should be (true|false)$`, t.theResourceConditionShouldBe)
	ctx.Step(`^I (create|delete) a resource ([^"]*)$`, t.iOperateOnResource)
	ctx.Step(`^I update a resource ([^"]*) with annotation ([^"]*) set to ([^"]*)$`, t.iUpdateResourceWithAnnotation)
	ctx.Step(`^I update a resource ([^"]*) with ([^"]*) set to ([^"]*)$`, t.iUpdateResourceWithField)
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
	t.RESTConfig = config

	if t.KubeClient == nil {
		client, err := kubernetes.NewForConfig(t.RESTConfig)
		if err != nil {
			return err
		}

		t.KubeClient = client
	}

	if t.DynamicClient == nil {
		dynClient, err := dynamic.NewForConfig(t.RESTConfig)
		if err != nil {
			log.Fatal("Unable to construct dynamic client", err)
		}

		t.DynamicClient = dynClient
	}

	_, err = t.KubeClient.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	return nil
}

func (t *FunctionalTest) iOperateOnResource(operation, fileName string) error {
	resourcePath := filepath.Join("templates", fileName)
	args := testutil.NewTemplateArguments()

	gvr, resource, err := testutil.GetResourceFromYaml(resourcePath, t.RESTConfig, args)
	if err != nil {
		return err
	}

	t.ResourceName = resource.GetName()
	t.ResourceNamespace = resource.GetNamespace()

	switch operation {
	case OperationCreate:
		_, err = t.DynamicClient.Resource(gvr.Resource).Namespace(t.ResourceNamespace).Create(context.Background(), resource, metav1.CreateOptions{})
		if err != nil {
			if kerrors.IsAlreadyExists(err) {
				// already created
				break
			}
			return err
		}
	case OperationDelete:
		err = t.DynamicClient.Resource(gvr.Resource).Namespace(t.ResourceNamespace).Delete(context.Background(), t.ResourceName, metav1.DeleteOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// already deleted
				break
			}
			return err
		}
	}
	return nil
}

func (t *FunctionalTest) iUpdateResourceWithAnnotation(fileName, annotation string, value string) error {
	resourcePath := filepath.Join("templates", fileName)
	args := testutil.NewTemplateArguments()

	gvr, resource, err := testutil.GetResourceFromYaml(resourcePath, t.RESTConfig, args)
	if err != nil {
		return err
	}

	t.ResourceName = resource.GetName()
	t.ResourceNamespace = resource.GetNamespace()

	updateTarget, err := t.DynamicClient.Resource(gvr.Resource).Namespace(t.ResourceNamespace).Get(context.Background(), t.ResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if err := unstructured.SetNestedField(updateTarget.UnstructuredContent(), value, []string{"metadata", "annotations", annotation}...); err != nil {
		return err
	}

	_, err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Update(context.Background(), updateTarget, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	return nil

}

func (t *FunctionalTest) iUpdateResourceWithField(fileName, key string, value string) error {
	var (
		keySlice     = testutil.DeleteEmpty(strings.Split(key, "."))
		overrideType bool
		intValue     int64
	)

	resourcePath := filepath.Join("templates", fileName)
	args := testutil.NewTemplateArguments()

	n, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		overrideType = true
		intValue = n
	}

	gvr, resource, err := testutil.GetResourceFromYaml(resourcePath, t.RESTConfig, args)
	if err != nil {
		return err
	}

	t.ResourceName = resource.GetName()
	t.ResourceNamespace = resource.GetNamespace()

	updateTarget, err := t.DynamicClient.Resource(gvr.Resource).Namespace(t.ResourceNamespace).Get(context.Background(), t.ResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if overrideType {
		if err := unstructured.SetNestedField(updateTarget.UnstructuredContent(), intValue, keySlice...); err != nil {
			return err
		}
	} else {
		if err := unstructured.SetNestedField(updateTarget.UnstructuredContent(), value, keySlice...); err != nil {
			return err
		}
	}

	_, err = t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Update(context.Background(), updateTarget, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	return nil
}

func (t *FunctionalTest) theResourceConditionShouldBe(cType string, cond string) error {
	var (
		counter        int
		expectedStatus = cases.Title(language.English).String(cond)
	)

	for {
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for resource state")
		}
		log.Infof("BDD >> waiting for resource %v/%v to meet condition %v=%v", t.ResourceNamespace, t.ResourceName, cType, cond)
		resource, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(context.Background(), t.ResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if conditions, ok, err := unstructured.NestedSlice(resource.UnstructuredContent(), "status", "conditions"); ok {
			if err != nil {
				return err
			}

			for _, c := range conditions {
				condition, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				tp, found := condition["type"]
				if !found {
					continue
				}
				condType, ok := tp.(string)
				if !ok {
					continue
				}
				if condType == cType {
					status := condition["status"].(string)
					if corev1.ConditionStatus(status) == corev1.ConditionStatus(expectedStatus) {
						return nil
					}
				}
			}
		}
		counter++
		time.Sleep(DefaultWaiterInterval)
	}
}

func (t *FunctionalTest) theResourceShouldBe(state string) error {
	var (
		exists  bool
		counter int
	)

	for {
		exists = true
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for resource state")
		}
		log.Infof("BDD >> waiting for resource %v/%v to become %v", t.ResourceNamespace, t.ResourceName, state)
		_, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(context.Background(), t.ResourceName, metav1.GetOptions{})
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
				log.Infof("BDD >> %v/%v is deleted", t.ResourceNamespace, t.ResourceName)
				return nil
			}
		case ResourceStateCreated:
			if exists {
				log.Infof("BDD >> %v/%v is created", t.ResourceNamespace, t.ResourceName)
				return nil
			}
		}

		counter++
		time.Sleep(DefaultWaiterInterval)
	}

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
		resource, err := t.DynamicClient.Resource(InstanceGroupSchema).Namespace(t.ResourceNamespace).Get(context.Background(), t.ResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if val, ok, err := unstructured.NestedString(resource.UnstructuredContent(), keySlice...); ok {
			if err != nil {
				return err
			}
			if strings.EqualFold(val, value) {
				break
			}
		}
		counter++
		time.Sleep(DefaultWaiterInterval)
	}

	return nil
}

func (t *FunctionalTest) nodesShouldBe(count int, state string) error {
	return t.waitForNodeCountState(count, state, fmt.Sprintf("test=%v", t.ResourceName))
}

func (t *FunctionalTest) nodesShouldBeWithLabel(count int, state, key, value string) error {
	selector := fmt.Sprintf("test=%v,%v=%v", t.ResourceName, key, value)
	return t.waitForNodeCountState(count, state, selector)
}

func (t *FunctionalTest) waitForNodeCountState(count int, state, selector string) error {
	var (
		counter int
		found   bool
	)

	for {
		var conditionNodes int
		var opts = metav1.ListOptions{
			LabelSelector: selector,
		}
		if counter >= DefaultWaiterRetries {
			return errors.New("waiter timed out waiting for nodes")
		}
		log.Infof("BDD >> %v/%v waiting for %v nodes to be %v with selector %v", t.ResourceNamespace, t.ResourceName, count, state, selector)
		nodes, err := t.KubeClient.CoreV1().Nodes().List(context.Background(), opts)
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
	var deleteFn = func(path string, info os.FileInfo, walkErr error) error {

		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}

		gvr, resource, err := testutil.GetResourceFromYaml(path, t.RESTConfig, testutil.NewTemplateArguments())
		if err != nil {
			return err
		}

		var (
			namespace = resource.GetNamespace()
			name      = resource.GetName()
			kind      = resource.GetKind()
		)

		if strings.EqualFold(kind, "ConfigMap") {
			return nil
		}

		err = t.DynamicClient.Resource(gvr.Resource).Namespace(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			log.Warnf("BDD >> failed to delete %v %v/%v: %v", kind, namespace, name, err)
		}
		log.Infof("BDD >> submitted deletion for %v %v/%v", kind, namespace, name)
		return nil
	}

	var waitFn = func(path string, info os.FileInfo, walkErr error) error {
		var counter int

		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}

		gvr, resource, err := testutil.GetResourceFromYaml(path, t.RESTConfig, testutil.NewTemplateArguments())
		if err != nil {
			return err
		}

		var (
			namespace = resource.GetNamespace()
			name      = resource.GetName()
			kind      = resource.GetKind()
		)

		if strings.EqualFold(kind, "ConfigMap") {
			return nil
		}

		for {
			if counter >= DefaultWaiterRetries {
				return errors.New("waiter timed out waiting for deletion")
			}

			log.Infof("BDD >> waiting for %v deletion of %v/%v", kind, namespace, name)
			_, err := t.DynamicClient.Resource(gvr.Resource).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				if kerrors.IsNotFound(err) {
					log.Infof("BDD >> %v %v/%v is deleted", kind, namespace, name)
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

	// Delete configmap last
	var (
		cmName      = "instance-manager"
		cmNamespace = "instance-manager"
		cmKind      = "ConfigMap"
	)
	log.Infof("BDD >> submitted deletion for %v %v/%v", cmKind, cmNamespace, cmName)
	err := t.KubeClient.CoreV1().ConfigMaps(cmNamespace).Delete(context.Background(), cmName, metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		log.Warnf("BDD >> failed to delete %v %v/%v: %v", cmKind, cmNamespace, cmName, err)
	}

	return nil
}
