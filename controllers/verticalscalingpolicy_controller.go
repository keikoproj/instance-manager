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
	"github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	//"k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// VerticalScalingPolicyReconciler reconciles a VerticalScalingPolicy object
type VerticalScalingPolicyReconciler struct {
	client.Client
	Log            logr.Logger
	Auth           *InstanceGroupAuthenticator
	ManagerContext *SharedContext
	Resync         chan event.GenericEvent
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
	r.
	// update the policies map
	r.ManagerContext.UpsertPolicy(vsp)

	// Which type ranges should we use?

	// Get instance types info
	// Call AWS (via caching) to get instance types info
	types, err := r.Auth.Aws.DescribeInstanceTypes()
	if err != nil {
		return ctrl.Result{}, err
	}

	instanceTypeRange, err := r.calculateInstanceTypeRange(vsp, types)
	if err != nil {
		return ctrl.Result{}, err
	}

	// decide on computed type

	// come up with matching instance type according to resources/requests/limits and instance family

	// Should we scale?
	for _, ig := range vsp.Spec.Targets {
		igObj := r.ManagerContext.InstanceGroups[ig.Name]
		var nodesOfIG []corev1.Node
		for _, node := range r.ManagerContext.Nodes {
			if kubernetes.HasAnnotationWithValue(node.GetLabels(), v1alpha1.NodeIGAnnotationKey, ig.Namespace + "-" + ig.Name) {
				nodesOfIG = append(nodesOfIG, node)
			}
		}
		currInstanceTypeIndex := common.GetStringIndexInSlice(instanceTypeRange.InstanceTypes, igObj.Status.CurrentInstanceType)
		if currInstanceTypeIndex == -1 {
			r.Log.Error(err, "reconcile failed, current instance type not found in computed types", "verticalscalingpolicy", req.NamespacedName)
			return ctrl.Result{}, err
		}

		// TODO: For period seconds we need to store node stats with timestamp or read and understand events
		hasLargerInstanceType := len(instanceTypeRange.InstanceTypes) > currInstanceTypeIndex + 2
		hasSmallerInstanceType := currInstanceTypeIndex - 1 >= 0
		// If there is a larger instance type available, check if we want to vertically scale up the IG
		if hasLargerInstanceType {
			// Check if policy exists to scale up on nodes count close to max
			scaleUpOnNodesCountPolicy := getBehaviorPolicy(vsp.Spec.Behavior.ScaleUp.Policies, v1alpha1.VSPolicyTypeNodesCountUtilizationPercent)
			if scaleUpOnNodesCountPolicy != nil {
				// Scale up if current nodes count is greater than specifiedPercentage*maxSize of IG
				if len(nodesOfIG) > int(igObj.Spec.EKSSpec.MaxSize)*scaleUpOnNodesCountPolicy.Value/100 {
					r.ManagerContext.ComputedTypes[igObj.Namespace + "/" + igObj.Name] = instanceTypeRange.InstanceTypes[currInstanceTypeIndex + 1]
				}

			}
		}
		// If there is a smaller instance type available, check if we want to vertically scale down the IG
		if hasSmallerInstanceType {

		}
	}
	// check if behavior conditions are met + validations

	// Update computed type on shared data structure

	// reconcile instance-group if there is drift
	r.NotifyTargets(vsp)
	return ctrl.Result{}, nil
}

type InstanceTypeRange struct {
	MaxType       string   // m5.8xlarge
	MinType       string   // m5.xlarge
	InstanceTypes []string // [m5.xlarge, m5.2xlarge, m5.4xlarge, m5.8xlarge]
	DesiredType   string
}

// Decides MaxType, MinType, and updates InstanceTypes
func (r *VerticalScalingPolicyReconciler) calculateInstanceTypeRange(v *v1alpha1.VerticalScalingPolicy, instanceTypesInfo []*ec2.InstanceTypeInfo) (*InstanceTypeRange, error) {
	var (
		typeRange         = &InstanceTypeRange{}
		hasInstanceFamily bool
		resources         = v.Spec.Resources
	)

	// validate provided instance family
	instanceFamily, ok := v.InstanceFamily()
	if ok {
		if instanceFamilyExists(instanceFamily, instanceTypesInfo) {
			hasInstanceFamily = true
		} else {
			r.Log.Info("provided instance family does not exist", "instanceFamily", instanceFamily)
		}
	}

	// if instance family is invalid or not provided, we need to detect it
	if !hasInstanceFamily {
		var err error
		instanceFamily, err = r.deriveInstanceFamily(resources, instanceTypesInfo)
		if err != nil {
			return typeRange, errors.Wrap(err, "failed to derive instance family")
		}
	}

	// get min/max type in a family according to requests/limits
	typeRange.MinType = r.minInstanceType(resources, instanceTypesInfo, instanceFamily)
	typeRange.MaxType = r.maxInstanceType(resources, instanceTypesInfo, instanceFamily)
	typeRange.InstanceTypes = r.rangeInstanceTypes(instanceTypesInfo, typeRange.MinType, typeRange.MaxType)

	return typeRange, nil
}

// TODO: Alfredo

// Decide which instance family to use
func (r *VerticalScalingPolicyReconciler) deriveInstanceFamily(resources *corev1.ResourceRequirements, instanceTypesInfo []*ec2.InstanceTypeInfo) (string, error) {
	return "", nil
}

// Decide which instance family to use
func (r *VerticalScalingPolicyReconciler) minInstanceType(resources *corev1.ResourceRequirements, instanceTypesInfo []*ec2.InstanceTypeInfo, family string) string {
	return ""
}

// Decide which instance family to use
func (r *VerticalScalingPolicyReconciler) maxInstanceType(resources *corev1.ResourceRequirements, instanceTypesInfo []*ec2.InstanceTypeInfo, family string) string {
	return ""
}

// Decide which instance family to use
func (r *VerticalScalingPolicyReconciler) rangeInstanceTypes(instanceTypesInfo []*ec2.InstanceTypeInfo, min, max string) []string {
	return []string{}
}

func instanceFamilyExists(family string, instanceTypesInfo []*ec2.InstanceTypeInfo) bool {
	families := make([]string, 0)
	for _, t := range instanceTypesInfo {
		instanceType := aws.StringValue(t.InstanceType)
		instance := strings.Split(instanceType, ".")
		families = append(families, instance[0])
	}

	if !common.ContainsString(families, family) {
		return false
	}
	return true
}

func (r *VerticalScalingPolicyReconciler) NotifyTargets(vsp *v1alpha1.VerticalScalingPolicy) {
	vspTarget := vsp.Spec.Target
	notification := event.GenericEvent{
		Object: &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vspTarget.Name,
				Namespace: vspTarget.Namespace,
			},
		},
	}
	r.ManagerContext.InstanceGroupEvents <- notification
}

func getBehaviorPolicy(policies []*v1alpha1.PolicySpec, name string) *v1alpha1.PolicySpec {
	for _, policy := range policies {
		if policy.Type == name {
			return policy
		}
	}
	return nil
}