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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ConfigMapName = "instance-manager"
)

func (r *InstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	switch r.NodeRelabel {
	case true:
		return ctrl.NewControllerManagedBy(mgr).
			For(&v1alpha1.InstanceGroup{}).
			Watches(&source.Kind{Type: &corev1.Event{}}, &handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.spotEventReconciler),
			}).
			Watches(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.nodeReconciler),
			}).
			Watches(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.configMapReconciler),
			}).
			WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxParallel}).
			Complete(r)
	default:
		return ctrl.NewControllerManagedBy(mgr).
			For(&v1alpha1.InstanceGroup{}).
			Watches(&source.Kind{Type: &corev1.Event{}}, &handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.spotEventReconciler),
			}).
			Watches(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.configMapReconciler),
			}).
			WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxParallel}).
			Complete(r)
	}
}

func (r *InstanceGroupReconciler) configMapReconciler(obj handler.MapObject) []ctrl.Request {
	var (
		name      = obj.Meta.GetName()
		namespace = obj.Meta.GetNamespace()
	)

	if strings.EqualFold(name, ConfigMapName) && strings.EqualFold(namespace, r.ConfigNamespace) {
		ctrl.Log.Info("configmap watch event", "name", obj.Meta.GetName(), "namespace", obj.Meta.GetNamespace())

		if !obj.Meta.GetDeletionTimestamp().IsZero() {
			r.ConfigMap = &corev1.ConfigMap{}
			return nil
		}

		r.ConfigMap = obj.Object.(*corev1.ConfigMap)
		configHash := kubeprovider.ConfigmapHash(r.ConfigMap)

		ctrl.Log.Info("configmap MD5", "hash", configHash)

		var instanceGroupList v1alpha1.InstanceGroupList
		err := r.List(context.Background(), &instanceGroupList)
		if err != nil {
			ctrl.Log.Error(err, "failed to convert to configmap")
			return nil
		}

		requests := make([]ctrl.Request, 0)
		for _, instanceGroup := range instanceGroupList.Items {
			if instanceGroup.Status.ConfigHash != configHash {
				namespacedName := types.NamespacedName{}
				namespacedName.Name = instanceGroup.GetName()
				namespacedName.Namespace = instanceGroup.GetNamespace()
				ctrl.Log.Info("found config diff for instancegroup", "instancegroup", namespacedName, "old", instanceGroup.Status.ConfigHash, "new", configHash)
				r := ctrl.Request{
					NamespacedName: namespacedName,
				}
				requests = append(requests, r)
			}
		}

		return requests
	}
	return nil
}

type NodeLabels struct {
	Labels map[string]string `json:"labels,omitempty"`
}

type LabelPatch struct {
	Metadata *NodeLabels `json:"metadata,omitempty"`
}

func (r *InstanceGroupReconciler) nodeReconciler(obj handler.MapObject) []ctrl.Request {
	var (
		nodeName          = obj.Meta.GetName()
		nodeLabels        = obj.Meta.GetLabels()
		roleLabelKey      = "kubernetes.io/role"
		bootstrapLabelKey = "node.kubernetes.io/role"
	)

	// if node already has a role label, don't modify it
	if _, ok := nodeLabels[roleLabelKey]; ok {
		return nil
	}

	// if node does not have the bootstrap label, don't modify it
	var val string
	var ok bool
	if val, ok = nodeLabels[bootstrapLabelKey]; !ok {
		return nil
	}

	nodeLabels[roleLabelKey] = val

	labelPatch := &LabelPatch{
		Metadata: &NodeLabels{
			Labels: nodeLabels,
		},
	}

	patchJSON, err := json.Marshal(labelPatch)
	if err != nil {
		r.Log.Error(err, "failed to marshal node labels", "node", nodeName, "patch", string(patchJSON))
		return nil
	}

	if _, err = r.Auth.Kubernetes.Kubernetes.CoreV1().Nodes().Patch(nodeName, types.StrategicMergePatchType, patchJSON); err != nil {
		r.Log.Error(err, "failed to patch node labels", "node", nodeName)
	}

	return nil
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
