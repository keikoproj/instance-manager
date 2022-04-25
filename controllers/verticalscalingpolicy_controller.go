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

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// VerticalScalingPolicyReconciler reconciles a VerticalScalingPolicy object
type VerticalScalingPolicyReconciler struct {
	client.Client
	Log            logr.Logger
	Auth           *InstanceGroupAuthenticator
	ManagerContext *SharedContext
}

//+kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=verticalscalingpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=verticalscalingpolicies/status,verbs=get;update;patch

func (r *VerticalScalingPolicyReconciler) Reconcile(ctxt context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("verticalscalingpolicy", req.NamespacedName)

	vsp := &v1alpha1.VerticalScalingPolicy{}
	err := r.Get(ctxt, req.NamespacedName, vsp)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.Log.Info("verticalscalingpolicy not found", "verticalscalingpolicy", req.NamespacedName)
			r.ManagerContext.RemovePolicy(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "reconcile failed", "verticalscalingpolicy", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// update the policies map
	r.ManagerContext.UpsertPolicy(vsp)

	// Which type ranges should we use?

	// Get instance types info
	// Call AWS (via caching) to get instance types info
	types, err := r.Auth.Aws.DescribeInstanceTypes()
	if err != nil {
		return ctrl.Result{}, err
	}

	calculateInstanceTypeRange(vsp, types)

	// decide on computed type

	// come up with matching instance type accroding to resouces/requests/limits and instance family

	// Should we scale?

	// check if behavior conditions are met + validations

	// Update computed type on shared data structure

	// reconcile instance-group if there is drift
	r.NotifyUpdates(vsp)
	return ctrl.Result{}, nil
}

type InstanceTypeRange struct {
	MaxType       string   // m5.8xlarge
	MinType       string   // m5.xlarge
	InstanceTypes []string // [m5.xlarge, m5.2xlarge, m5.4xlarge, m5.8xlarge]
	DesiredType   string
}

func calculateInstanceTypeRange(vsp *v1alpha1.VerticalScalingPolicy, instanceTypesInfo []*ec2.InstanceTypeInfo) *InstanceTypeRange {
	return &InstanceTypeRange{}
}

func (r *VerticalScalingPolicyReconciler) NotifyUpdates(vsp *v1alpha1.VerticalScalingPolicy) {
	vspTarget := vsp.Spec.Target
	notification := event.GenericEvent{
		Object: &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vspTarget.Name,
				Namespace: vspTarget.Namespace,
			},
		},
	}
	fmt.Println(notification)
	r.ManagerContext.InstanceGroupEvents <- notification
}
