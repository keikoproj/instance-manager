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

package controllers

import (
	"sync"
	"testing"

	v1alpha1 "github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func init() {
	// Setup logging for tests
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
}

// Helper function to create a properly initialized reconciler for testing
func createTestReconciler(objs ...runtime.Object) *InstanceGroupReconciler {
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

	reconciler := &InstanceGroupReconciler{
		Client:          fakeClient,
		Log:             ctrl.Log.WithName("controllers").WithName("InstanceGroup"),
		MaxParallel:     10,
		NodeRelabel:     true,
		Namespaces:      make(map[string]corev1.Namespace),
		NamespacesLock:  &sync.RWMutex{},
		ConfigRetention: 100,
		Metrics:         common.NewMetricsCollector(),
	}
	return reconciler
}

func TestSpotEventReconciler(t *testing.T) {
	// Create a test instancegroup
	instanceGroup := &v1alpha1.InstanceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ig",
			Namespace: "default",
		},
		Spec: v1alpha1.InstanceGroupSpec{
			// Create a basic spec without invalid fields
		},
	}

	reconciler := createTestReconciler(instanceGroup)

	// Create a spot termination event
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spot-termination",
			Namespace: "default",
		},
		Reason: "SpotInterruption",
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Node",
			Name:      "test-node",
			Namespace: "default",
		},
	}

	// Test the reconciler
	requests := reconciler.spotEventReconciler(event)

	// No requests expected in this test case since we don't have spot instances set up properly
	if len(requests) != 0 {
		t.Errorf("Expected 0 requests, got %d", len(requests))
	}
}

func TestNodeReconciler(t *testing.T) {
	// Create a test node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				"instancegroups.keikoproj.io/instance-group-name": "test-ig",
			},
		},
	}

	reconciler := createTestReconciler(node)

	// Test the reconciler
	requests := reconciler.nodeReconciler(node)

	// Expecting no requests since we don't have actual instancegroups in the test client
	if len(requests) != 0 {
		t.Errorf("Expected 0 requests, got %d", len(requests))
	}
}

func TestConfigMapReconciler(t *testing.T) {
	// Create a test configmap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scaling-configuration",
			Namespace: "default",
		},
		Data: map[string]string{
			"test-instancegroup": "some-data",
		},
	}

	reconciler := createTestReconciler(cm)

	// Test the reconciler
	requests := reconciler.configMapReconciler(cm)

	// Expecting no requests since we don't have actual instancegroups in the test client
	if len(requests) != 0 {
		t.Errorf("Expected 0 requests, got %d", len(requests))
	}
}

func TestNamespaceReconciler(t *testing.T) {
	// Create a test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}

	reconciler := createTestReconciler(ns)

	// Test the reconciler
	requests := reconciler.namespaceReconciler(ns)

	// Expecting no requests since we don't have actual instancegroups in the test client
	if len(requests) != 0 {
		t.Errorf("Expected 0 requests, got %d", len(requests))
	}
}
