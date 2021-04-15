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
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eksfargate"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eksmanaged"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InstanceGroupReconciler reconciles an InstanceGroup object
type InstanceGroupReconciler struct {
	client.Client
	SpotRecommendationTime float64
	ConfigNamespace        string
	NodeRelabel            bool
	Log                    logr.Logger
	MaxParallel            int
	Auth                   *InstanceGroupAuthenticator
	ConfigMap              *corev1.ConfigMap
	Namespaces             map[string]corev1.Namespace
	NamespacesLock         *sync.Mutex
	ConfigRetention        int
	Metrics                *common.MetricsCollector
}

type InstanceGroupAuthenticator struct {
	Aws        awsprovider.AwsWorker
	Kubernetes kubeprovider.KubernetesClientSet
}

const (
	FinalizerStr = "finalizer.instancegroups.keikoproj.io"

	ErrorReasonGetFailed               = "GetRequest"
	ErrorReasonDefaultsUnmarshalFailed = "UnmarshalDefaults"
	ErrorReasonDefaultsApplyFailed     = "ApplyDefaults"
	ErrorReasonValidationFailed        = "ResourceValidation"
	ErrorReasonReconcileFailed         = "HandleReconcile"
)

func (r *InstanceGroupReconciler) Finalize(instanceGroup *v1alpha1.InstanceGroup) {
	// Resource is being deleted
	meta := &instanceGroup.ObjectMeta
	deletionTimestamp := meta.GetDeletionTimestamp()
	if !deletionTimestamp.IsZero() {
		// And state is "Deleted"
		if instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
			// remove all finalizers
			meta.SetFinalizers([]string{})
			if err := r.Update(context.Background(), instanceGroup); err != nil {
				// avoid update errors for already deleted resources
				if kubeprovider.IsStorageError(err) {
					return
				}
				r.Log.Error(err, "failed to update custom resource")
			}
		}
	}
}

func (r *InstanceGroupReconciler) SetFinalizer(instanceGroup *v1alpha1.InstanceGroup) {
	// Resource is not being deleted
	if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		// And does not contain finalizer
		if !common.ContainsString(instanceGroup.ObjectMeta.Finalizers, FinalizerStr) {
			// Set Finalizer
			instanceGroup.ObjectMeta.Finalizers = append(instanceGroup.ObjectMeta.Finalizers, FinalizerStr)
			if err := r.Update(context.Background(), instanceGroup); err != nil {
				r.Log.Error(err, "failed to update custom resource")
			}
		}
	}
}

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=list;get;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=list;patch;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups/status,verbs=get;update;patch

func (r *InstanceGroupReconciler) Reconcile(ctxt context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("instancegroup", req.NamespacedName)

	instanceGroup := &v1alpha1.InstanceGroup{}
	err := r.Get(ctxt, req.NamespacedName, instanceGroup)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.Metrics.UnsetInstanceGroup(instanceGroup.NamespacedName(), "")
			r.Log.Info("instancegroup not found", "instancegroup", req.NamespacedName)
			r.Metrics.IncSuccess(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "reconcile failed", "instancegroup", req.NamespacedName)
		r.Metrics.IncFail(req.NamespacedName.String(), ErrorReasonGetFailed)
		return ctrl.Result{}, err
	}
	statusPatch := kubeprovider.MergePatch(*instanceGroup)

	// set/unset finalizer
	r.SetFinalizer(instanceGroup)
	startState := string(instanceGroup.GetState())
	defer r.Metrics.SetInstanceGroup(instanceGroup.NamespacedName(), startState, string(instanceGroup.GetState()))

	input := provisioners.ProvisionerInput{
		AwsWorker:       r.Auth.Aws,
		Kubernetes:      r.Auth.Kubernetes,
		Configuration:   r.ConfigMap,
		InstanceGroup:   instanceGroup,
		Log:             r.Log,
		ConfigRetention: r.ConfigRetention,
		Metrics:         r.Metrics,
	}

	var (
		status     = instanceGroup.GetStatus()
		configHash = kubeprovider.ConfigmapHash(r.ConfigMap)
	)
	status.SetConfigHash(configHash)

	if !reflect.DeepEqual(*r.ConfigMap, corev1.ConfigMap{}) {
		// Configmap exist - apply defaults/boundaries if namespace is not excluded
		namespace := instanceGroup.GetNamespace()
		if !r.IsNamespaceAnnotated(namespace, provisioners.ConfigurationExclusionAnnotationKey, "true") {
			// namespace is not excluded - proceed with applying defaults/boundaries
			var defaultConfig *provisioners.ProvisionerConfiguration
			if defaultConfig, err = provisioners.NewProvisionerConfiguration(r.ConfigMap, instanceGroup); err != nil {
				r.Metrics.IncFail(instanceGroup.NamespacedName(), ErrorReasonDefaultsUnmarshalFailed)
				return ctrl.Result{}, err
			}

			if err = defaultConfig.SetDefaults(); err != nil {
				r.Log.Error(err, "failed to set configuration defaults", "instancegroup", instanceGroup.NamespacedName())
				r.Metrics.IncFail(instanceGroup.NamespacedName(), ErrorReasonDefaultsApplyFailed)
				return ctrl.Result{}, err
			}

			input.InstanceGroup = defaultConfig.InstanceGroup
		} else {
			// unset config hash if namespace is excluded
			r.Log.Info("namespace excluded from managed configuration", "namespace", namespace)
			status.SetConfigHash("")
		}
	}

	provisionerKind := strings.ToLower(input.InstanceGroup.Spec.Provisioner)

	if !common.ContainsEqualFold(v1alpha1.Provisioners, provisionerKind) {
		r.Metrics.IncFail(instanceGroup.NamespacedName(), ErrorReasonValidationFailed)
		return ctrl.Result{}, errors.Errorf("provisioner '%v' does not exist", provisionerKind)
	}

	r.Log.Info("reconcile event started", "instancegroup", req.NamespacedName, "provisioner", provisionerKind)
	var ctx CloudDeployer
	switch {
	case strings.EqualFold(provisionerKind, eks.ProvisionerName):
		ctx = eks.New(input)
	case strings.EqualFold(provisionerKind, eksmanaged.ProvisionerName):
		ctx = eksmanaged.New(input)
	case strings.EqualFold(provisionerKind, eksfargate.ProvisionerName):
		ctx = eksfargate.New(input)
	}

	if err = input.InstanceGroup.Validate(); err != nil {
		ctx.SetState(v1alpha1.ReconcileErr)
		r.PatchStatus(input.InstanceGroup, statusPatch)
		r.Metrics.IncFail(instanceGroup.NamespacedName(), ErrorReasonValidationFailed)
		return ctrl.Result{}, errors.Wrapf(err, "provisioner %v reconcile failed", provisionerKind)
	}

	if err = HandleReconcileRequest(ctx); err != nil {
		ctx.SetState(v1alpha1.ReconcileErr)
		r.PatchStatus(input.InstanceGroup, statusPatch)
		r.Metrics.IncFail(instanceGroup.NamespacedName(), ErrorReasonReconcileFailed)
		return ctrl.Result{}, errors.Wrapf(err, "provisioner %v reconcile failed", provisionerKind)
	}

	if provisioners.IsRetryable(input.InstanceGroup) {
		r.Log.Info("reconcile event ended with requeue", "instancegroup", req.NamespacedName, "provisioner", provisionerKind)
		r.PatchStatus(input.InstanceGroup, statusPatch)
		r.Metrics.IncSuccess(instanceGroup.NamespacedName())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	r.Log.Info("reconcile event ended", "instancegroup", req.NamespacedName, "provisioner", provisionerKind)
	r.PatchStatus(input.InstanceGroup, statusPatch)
	r.Finalize(instanceGroup)
	r.Metrics.IncSuccess(instanceGroup.NamespacedName())
	return ctrl.Result{}, nil
}

func (r *InstanceGroupReconciler) PatchStatus(instanceGroup *v1alpha1.InstanceGroup, patch client.Patch) {
	patchData, _ := patch.Data(instanceGroup)
	r.Log.Info("patching resource status", "instancegroup", instanceGroup.NamespacedName(), "patch", string(patchData), "resourceVersion", instanceGroup.GetResourceVersion())
	if err := r.Status().Patch(context.Background(), instanceGroup, patch); err != nil {
		// avoid error if object already deleted
		if kubeprovider.IsStorageError(err) {
			return
		}
		r.Log.Info("failed to patch status", "error", err, "instancegroup", instanceGroup.NamespacedName())
	}
}

func (r *InstanceGroupReconciler) IsNamespaceAnnotated(namespace, key, value string) bool {
	if ns, ok := r.Namespaces[namespace]; ok {
		nsObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ns)
		if err != nil {
			r.Log.Error(err, "failed to convert namespace to unstructured", "namespace", namespace)
			return false
		}
		unstructuredNamespace := &unstructured.Unstructured{
			Object: nsObject,
		}

		annotations := unstructuredNamespace.GetAnnotations()
		if kubeprovider.HasAnnotation(annotations, key, value) {
			return true
		}
	}
	return false
}
