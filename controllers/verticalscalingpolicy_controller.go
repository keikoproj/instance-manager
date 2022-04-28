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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	resource "k8s.io/apimachinery/pkg/api/resource"
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
		igName := fmt.Sprintf("%v/%v", igObj.GetNamespace(), igObj.GetName())
		var nodesOfIG = make([]*corev1.Node, 0)
		// var scaleUp bool
		// var scaleDown bool
		// var desiredType string

		for _, node := range r.ManagerContext.Nodes {
			if kubernetes.HasAnnotationWithValue(node.GetLabels(), v1alpha1.InstanceGroupNameAnnotationKey, fmt.Sprintf("%v-%v", igObj.GetNamespace(), igObj.GetName())) {
				nodesOfIG = append(nodesOfIG, node)
			}
		}

		currInstanceTypeIndex := common.GetStringIndexInSlice(instanceTypeRange.InstanceTypes, igObj.Status.CurrentInstanceType)
		if currInstanceTypeIndex == -1 {
			r.Log.Error(err, "reconcile failed, current instance type not found in computed types", "verticalscalingpolicy", req.NamespacedName)
			return ctrl.Result{}, err
		}

		// TODO: For period seconds we need to store node stats with timestamp or read and understand events
		hasLargerInstanceType := len(instanceTypeRange.InstanceTypes) > currInstanceTypeIndex+2
		hasSmallerInstanceType := currInstanceTypeIndex-1 >= 0

		totalCPUAllocatable := *resource.NewQuantity(0, resource.BinarySI)
		totalCPUCapacity := *resource.NewQuantity(0, resource.BinarySI)

		totalMemoryAllocatable := *resource.NewQuantity(0, resource.BinarySI)
		totalMemoryCapacity := *resource.NewQuantity(0, resource.BinarySI)

		// Calculate current CPU utilization
		// Calculate current Memory utilization
		for _, node := range nodesOfIG {
			totalCPUAllocatable.Add(*node.Status.Allocatable.Cpu())
			totalMemoryAllocatable.Add(*node.Status.Allocatable.Memory())
			totalCPUCapacity.Add(*node.Status.Capacity.Cpu())
			totalMemoryCapacity.Add(*node.Status.Capacity.Memory())
		}

		totalCPUCapacityFloat := totalCPUCapacity.AsApproximateFloat64()
		totalCPUAllocatableFloat := totalCPUAllocatable.AsApproximateFloat64()
		totalMemoryCapacityFloat := totalMemoryCapacity.AsApproximateFloat64()
		totalMemoryAllocatableFloat := totalMemoryAllocatable.AsApproximateFloat64()

		scaleUpBehaviorPolicies := vsp.Spec.Behavior.ScaleUp.Policies // TODO: Make sure to avoid null pointer exception here by validating vsp before this
		scaleUpOnNodesCountPolicy := getBehaviorPolicy(scaleUpBehaviorPolicies, v1alpha1.NodesCountUtilizationPercent)
		scaleUpOnCpuUtilization := getBehaviorPolicy(scaleUpBehaviorPolicies, v1alpha1.CPUUtilizationPercent)
		scaleUpOnMemoryUtilization := getBehaviorPolicy(scaleUpBehaviorPolicies, v1alpha1.MemoryUtilizationPercent)

		scaleUp_stabilizationWindow := time.Duration(vsp.Spec.Behavior.ScaleUp.StabilizationWindowSeconds) * time.Second
		scaleDown_stabilizationWindow := time.Duration(vsp.Spec.Behavior.ScaleDown.StabilizationWindowSeconds) * time.Second

		// If there is a larger instance type available, check if we want to vertically scale up the IG
		if hasLargerInstanceType && vsp.Status != nil && time.Since(vsp.Status.TargetStatuses[igName].LastTransitionTime.Time) > scaleUp_stabilizationWindow {
			nextBiggerInstance := instanceTypeRange.InstanceTypes[currInstanceTypeIndex+1]

			if scaleUpOnNodesCountPolicy != nil {
				// Scale up if current nodes count crosses requested threshold (close to maxSize of IG)
				if len(nodesOfIG) > int(igObj.Spec.EKSSpec.MaxSize)*scaleUpOnNodesCountPolicy.Value/100 {
					// Add periodSeconds logic here
					r.ManagerContext.ComputedTypes[igName] = nextBiggerInstance
					continue
				}
			}

			if scaleUpOnCpuUtilization != nil {
				// Scale up if total CPU utilization crosses requested threshold (close to node sizing)
				if 100*(totalCPUCapacityFloat-totalCPUAllocatableFloat)/totalCPUCapacityFloat > float64(scaleUpOnCpuUtilization.Value) {
					// Add periodSeconds logic here
					r.ManagerContext.ComputedTypes[igName] = nextBiggerInstance
					continue
				}
			}

			if scaleUpOnMemoryUtilization != nil {
				if 100*(totalMemoryCapacityFloat-totalMemoryAllocatableFloat)/totalMemoryCapacityFloat > float64(scaleUpOnMemoryUtilization.Value) {
					// Add periodSeconds logic here
					r.ManagerContext.ComputedTypes[igName] = instanceTypeRange.InstanceTypes[currInstanceTypeIndex+1]
					continue
				}
			}
		}

		// If there is a smaller instance type available, check if we want to vertically scale down the IG
		if hasSmallerInstanceType && time.Since(vsp.Status.TargetStatuses[igName].LastTransitionTime.Time) > scaleDown_stabilizationWindow {
			scaleDownBehaviorPolicies := vsp.Spec.Behavior.ScaleDown.Policies
			scaleDownOnCpuUtilization := getBehaviorPolicy(scaleDownBehaviorPolicies, v1alpha1.CPUUtilizationPercent)
			scaleDownOnMemoryUtilization := getBehaviorPolicy(scaleDownBehaviorPolicies, v1alpha1.MemoryUtilizationPercent)
			currentCapacityUtilization := 100 * (totalCPUCapacityFloat - totalCPUAllocatableFloat) / totalCPUCapacityFloat
			currentMemoryUtilization := 100 * (totalMemoryCapacityFloat - totalMemoryAllocatableFloat) / totalMemoryCapacityFloat
			// minimumNodesRequired := 0

			/**
			 * When we scale down, utilizations on smaller instance double
			 * If smaller instance utilization > scaleUpOnCpuUtilization || scaleUpOnMemoryUtilization
			 * 		=> scale down is going to trigger a scale up after stabilizationWindowSeconds
			 * 		=> we shouldn't scale down
			 *
			 * TODO:
			 * Number of smaller Instances >= Minimum number of nodes required due to affinity/antiaffinity rules
			 * If number of smaller instances > nodesCountUtilizationThreshold
			 * 		=> scale down is going to trigger a scale up after stabilizationWindowSeconds
			 * 		=> we shouldn't scale down
			 */

			if scaleDownOnCpuUtilization != nil {
				if currentCapacityUtilization < float64(scaleDownOnCpuUtilization.Value) { // Check if scale down is a possibility
					if currentCapacityUtilization/2 < float64(scaleUpOnCpuUtilization.Value) { // Check if eventual scale up is a possibility
						// Add periodSeconds logic here
						r.ManagerContext.ComputedTypes[igName] = instanceTypeRange.InstanceTypes[currInstanceTypeIndex-1]
						continue
					}
				}
			}

			if scaleDownOnMemoryUtilization != nil {
				if currentMemoryUtilization < float64(scaleDownOnMemoryUtilization.Value) { // Check if scale down is a possibility
					if currentMemoryUtilization/2 < float64(scaleUpOnMemoryUtilization.Value) { // Check if eventual scale up is a possibility
						// Add periodSeconds logic here
						r.ManagerContext.ComputedTypes[igName] = instanceTypeRange.InstanceTypes[currInstanceTypeIndex-1]
						continue
					}
				}
			}
		}
	}

	// check if behavior conditions are met + validations

	// Update computed type on shared data structure

	// reconcile instance-group if there is drift
	for _, ig := range r.ManagerContext.ComputedTypes {
		r.Log.Info("Reconciling instance group %s to instanceType %s", ig, r.ManagerContext.ComputedTypes[ig])
		// reconcile ig TODO: Ask Eytan

		vsp.Status.TargetStatuses[ig] = &v1alpha1.TargetStatus{
			LastTransitionTime:  metav1.Time{Time: time.Now()},
			DesiredInstanceType: r.ManagerContext.ComputedTypes[ig],
			// State: ig reconcilation state TODO: Ask Eytan
		}

		// Once IG is reconciled, we need to remove it from sharedContext.ComputedTypes
		delete(r.ManagerContext.ComputedTypes, ig)
	}

	// Update vsp status to done

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
	// Filter instanceTypesInfo by instanceFamily to find range of instanceTypes suitable for resource requests and limits
	// Sort the instance family by first by cpu and then by memory

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
	targets := vsp.Spec.Targets
	for _, target := range targets {
		r.ManagerContext.InstanceGroupEvents <- event.GenericEvent{
			Object: &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:      target.Name,
					Namespace: target.Namespace,
				},
			},
		}
	}
}

func getBehaviorPolicy(policies []*v1alpha1.PolicySpec, name string) *v1alpha1.PolicySpec {
	for _, policy := range policies {
		if policy.Type == name {
			return policy
		}
	}
	return nil
}
