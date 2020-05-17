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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eksmanaged"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// InstanceGroupReconciler reconciles an InstanceGroup object
type InstanceGroupReconciler struct {
	client.Client
	SpotRecommendationTime float64
	Log                    logr.Logger
	ControllerConfPath     string
	MaxParallel            int
	Auth                   *InstanceGroupAuthenticator
}

type InstanceGroupAuthenticator struct {
	Aws        awsprovider.AwsWorker
	Kubernetes kubeprovider.KubernetesClientSet
}

func (r *InstanceGroupReconciler) Finalize(instanceGroup *v1alpha1.InstanceGroup, finalizerName string) {
	// Resource is being deleted
	meta := &instanceGroup.ObjectMeta
	deletionTimestamp := meta.GetDeletionTimestamp()
	if !deletionTimestamp.IsZero() {
		// And state is "Deleted"
		if instanceGroup.GetState() == v1alpha1.ReconcileDeleted {
			// Unset Finalizer if present
			if common.ContainsString(meta.GetFinalizers(), finalizerName) {
				meta.SetFinalizers(common.RemoveString(instanceGroup.ObjectMeta.Finalizers, finalizerName))
			}
		}
	}
}

func (r *InstanceGroupReconciler) SetFinalizer(instanceGroup *v1alpha1.InstanceGroup, finalizerName string) {
	// Resource is not being deleted
	if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		// And does not contain finalizer
		if !common.ContainsString(instanceGroup.ObjectMeta.Finalizers, finalizerName) {
			// Set Finalizer
			instanceGroup.ObjectMeta.Finalizers = append(instanceGroup.ObjectMeta.Finalizers, finalizerName)
		}
	}
}

func (r *InstanceGroupReconciler) NewProvisionerInput(instanceGroup *v1alpha1.InstanceGroup) (provisioners.ProvisionerInput, error) {
	var input provisioners.ProvisionerInput
	config := provisioners.ProvisionerConfiguration{}
	if _, err := os.Stat(r.ControllerConfPath); os.IsExist(err) {
		ctrlConfig, err := common.ReadFile(r.ControllerConfPath)
		if err != nil {
			return input, err
		}

		err = yaml.Unmarshal(ctrlConfig, &config)
		if err != nil {
			return input, err
		}
	}

	input = provisioners.ProvisionerInput{
		AwsWorker:     r.Auth.Aws,
		Kubernetes:    r.Auth.Kubernetes,
		InstanceGroup: instanceGroup,
		Configuration: config,
		Log:           r.Log,
	}
	return input, nil
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=list
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;create;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups/status,verbs=get;update;patch
func (r *InstanceGroupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("instancegroup", req.NamespacedName)

	instanceGroup := &v1alpha1.InstanceGroup{}
	err := r.Get(context.Background(), req.NamespacedName, instanceGroup)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.Log.Info("instancegroup not found", "instancegroup", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "reconcile failed")
		return ctrl.Result{}, err
	}

	if err = instanceGroup.Spec.Validate(); err != nil {
		r.Log.Error(err, "reconcile failed")
		instanceGroup.SetState(v1alpha1.ReconcileErr)
		r.Update(context.Background(), instanceGroup)
		return ctrl.Result{}, err
	}

	// Add Finalizer if not present, and set the initial state
	finalizerName := fmt.Sprintf("finalizers.%v.instancegroups.keikoproj.io", instanceGroup.Spec.Provisioner)
	r.SetFinalizer(instanceGroup, finalizerName)

	input, err := r.NewProvisionerInput(instanceGroup)
	if err != nil {
		r.Log.Error(err, "failed to initialize provisioner", instanceGroup.GetName())
		return ctrl.Result{}, nil
	}
	provisionerKind := strings.ToLower(instanceGroup.Spec.Provisioner)
	r.Log.Info(
		"reconcile event started",
		"instancegroup", req.Name,
		"namespace", req.Namespace,
		"provisioner", provisionerKind,
		"resourceVersion", instanceGroup.GetResourceVersion(),
	)

	switch provisionerKind {
	case eks.ProvisionerName:
		ctx := eks.New(input)
		defer r.Update(context.Background(), ctx.GetInstanceGroup())
		err = HandleReconcileRequest(ctx)
		if err != nil {
			r.Log.Error(err,
				"reconcile failed",
				"instancegroup", instanceGroup.GetName(),
				"provisioner", provisionerKind,
			)
			ctx.SetState(v1alpha1.ReconcileErr)
		}
		if eks.IsRetryable(instanceGroup) {
			r.Log.Info(
				"reconcile event ended with requeue",
				"instancegroup", req.Name,
				"namespace", req.Namespace,
				"provisioner", provisionerKind,
				"resourceVersion", instanceGroup.GetResourceVersion(),
			)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case eksmanaged.ProvisionerName:
		ctx := eksmanaged.New(input)
		defer r.Update(context.Background(), ctx.GetInstanceGroup())
		err = HandleReconcileRequest(ctx)
		if err != nil {
			r.Log.Error(err,
				"reconcile failed",
				"instancegroup", instanceGroup.GetName(),
				"provisioner", provisionerKind,
			)
			ctx.SetState(v1alpha1.ReconcileErr)
		}
		if eksmanaged.IsRetryable(instanceGroup) {
			r.Log.Info(
				"reconcile event ended with requeue",
				"instancegroup", req.Name,
				"namespace", req.Namespace,
				"provisioner", provisionerKind,
				"resourceVersion", instanceGroup.GetResourceVersion(),
			)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	default:
		return ctrl.Result{}, errors.Errorf("provisioner '%v' does not exist", provisionerKind)
	}

	r.Finalize(instanceGroup, finalizerName)
	r.Log.Info(
		"reconcile event ended",
		"instancegroup", req.Name,
		"namespace", req.Namespace,
		"provisioner", provisionerKind,
		"resourceVersion", instanceGroup.GetResourceVersion(),
	)
	return ctrl.Result{}, nil
}

func (r *InstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.InstanceGroup{}).
		Watches(&source.Kind{Type: &corev1.Event{}}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.spotEventReconciler),
		}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxParallel}).
		Complete(r)
}

func (r *InstanceGroupReconciler) spotEventReconciler(obj handler.MapObject) []ctrl.Request {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.Object)
	if err != nil {
		return nil
	}

	if reason, ok, _ := unstructured.NestedString(unstructuredObj, "reason"); ok {
		if reason != kubeprovider.SpotRecommendationReason {
			return nil
		}
	} else {
		return nil
	}

	creationTime := obj.Meta.GetCreationTimestamp()
	minutesSince := time.Since(creationTime.Time).Minutes()
	if minutesSince > r.SpotRecommendationTime {
		return nil
	}

	ctrl.Log.Info(fmt.Sprintf("spot recommendation %v/%v", obj.Meta.GetNamespace(), obj.Meta.GetName()))

	involvedObjectName, exists, err := unstructured.NestedString(unstructuredObj, "involvedObject", "name")
	if err != nil || !exists {
		r.Log.Error(err,
			"failed to process v1.event",
			"event", obj.Meta.GetName(),
		)
		return nil
	}

	tags, err := awsprovider.GetScalingGroupTagsByName(involvedObjectName, r.Auth.Aws.AsgClient)
	if err != nil {
		return nil
	}

	instanceGroup := types.NamespacedName{}
	instanceGroup.Name = awsprovider.GetTagValueByKey(tags, provisioners.TagInstanceGroupName)
	instanceGroup.Namespace = awsprovider.GetTagValueByKey(tags, provisioners.TagInstanceGroupNamespace)
	if instanceGroup.Name == "" || instanceGroup.Namespace == "" {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: instanceGroup,
		},
	}
}
