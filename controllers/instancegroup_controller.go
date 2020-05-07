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

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/go-logr/logr"
	v1alpha "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/keikoproj/instance-manager/controllers/provisioners/ekscloudformation"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eksfargate"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eksmanaged"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// InstanceGroupReconciler reconciles an InstanceGroup object
type InstanceGroupReconciler struct {
	client.Client
	ScalingGroups          autoscalingiface.AutoScalingAPI
	Log                    logr.Logger
	ControllerConfPath     string
	ControllerTemplatePath string
}

// CloudDeployer is a common interface that should be fulfilled by each provisioner
type CloudDeployer interface {
	CloudDiscovery() error            // Discover cloud resources
	StateDiscovery()                  // Derive state
	Create() error                    // CREATE Operation
	Update() error                    // UPDATE Operation
	Delete() error                    // DELETE Operation
	UpgradeNodes() error              // Process upgrade strategy
	BootstrapNodes() error            // Bootstrap Provisioned Resources
	GetState() v1alpha.ReconcileState // Gets the current state type of the instance group
	SetState(v1alpha.ReconcileState)  // Sets the current state of the instance group
	IsReady() bool                    // Returns true if state is Ready
}

func HandleReconcileRequest(d CloudDeployer) error {
	// Cloud Discovery
	log.Infoln("starting cloud discovery")
	err := d.CloudDiscovery()
	if err != nil {
		log.Error(err)
		return err
	}

	// State Discovery
	log.Infoln("starting state discovery")
	d.StateDiscovery()

	// CRUD Delete
	if d.GetState() == v1alpha.ReconcileInitDelete {
		log.Infoln("starting delete")
		err = d.Delete()
		if err != nil {
			log.Infoln(err)
			return err
		}
	}

	// CRUD Create
	if d.GetState() == v1alpha.ReconcileInitCreate {
		log.Infoln("starting create")
		err = d.Create()
		if err != nil {
			log.Infoln(err)
			return err
		}
	}

	// CRUD Update
	if d.GetState() == v1alpha.ReconcileInitUpdate {
		log.Infoln("starting update")
		err = d.Update()
		if err != nil {
			log.Infoln(err)
			return err
		}
	}

	// CRUD Nodes Upgrade Strategy
	if d.GetState() == v1alpha.ReconcileInitUpgrade {
		// CF Update finished & upgrade is not 'rollingUpdate'.
		log.Infoln("starting nodes upgrade")
		err = d.UpgradeNodes()
		if err != nil {
			log.Infoln(err)
			return err
		}
	}

	// CRUD Error
	if d.GetState() == v1alpha.ReconcileErr {
		err = fmt.Errorf("failed to converge cloud resources")
		return err
	}

	// Bootstrap Nodes
	if d.IsReady() {
		log.Infoln("starting bootstrap")
		err = d.BootstrapNodes()
		if err != nil {
			log.Infoln(err)
			return err
		}

		if d.GetState() == v1alpha.ReconcileInitUpgrade {
			err = d.UpgradeNodes()
			if err != nil {
				log.Infoln(err)
				return err
			}
		}

		// Set Ready state (external end state)
		if d.GetState() == v1alpha.ReconcileModified {
			d.SetState(v1alpha.ReconcileReady)
		}
	}
	return nil
}

func (r *InstanceGroupReconciler) Finalize(instanceGroup *v1alpha.InstanceGroup, finalizerName string) {
	// Resource is being deleted
	meta := &instanceGroup.ObjectMeta
	deletionTimestamp := meta.GetDeletionTimestamp()
	if !deletionTimestamp.IsZero() {
		// And state is "Deleted"
		if instanceGroup.GetState() == v1alpha.ReconcileDeleted {
			// Unset Finalizer if present
			if common.ContainsString(meta.GetFinalizers(), finalizerName) {
				meta.SetFinalizers(common.RemoveString(instanceGroup.ObjectMeta.Finalizers, finalizerName))
			}
		}
	}
}

func (r *InstanceGroupReconciler) SetFinalizer(instanceGroup *v1alpha.InstanceGroup, finalizerName string) {
	// Resource is not being deleted
	if instanceGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		// And does not contain finalizer
		if !common.ContainsString(instanceGroup.ObjectMeta.Finalizers, finalizerName) {
			// Set Finalizer
			instanceGroup.ObjectMeta.Finalizers = append(instanceGroup.ObjectMeta.Finalizers, finalizerName)
		}
	}
}

func addressOf(s string) *string {
	return &s
}

func (r *InstanceGroupReconciler) ReconcileEKSFargate(instanceGroup *v1alpha.InstanceGroup, finalizerName string) error {
	awsRegion, err := aws.GetRegion()
	if err != nil {
		return err
	}
	spec := instanceGroup.Spec.EKSFargateSpec
	worker := &aws.AwsFargateWorker{
		IamClient:   aws.GetAwsIAMClient(awsRegion),
		EksClient:   aws.GetAwsEksClient(awsRegion),
		ClusterName: spec.GetClusterName(),
		ProfileName: spec.GetProfileName(),
		Selectors:   eksfargate.CreateFargateSelectors(spec.GetSelectors()),
		Tags:        eksfargate.CreateFargateTags(spec.GetTags()),
		Subnets:     spec.GetSubnets(),
	}
	ctx, err := eksfargate.New(instanceGroup, worker)
	if err != nil {
		log.Errorf("Allocation of EKSFargate context failed: %v\n", err)
		ctx.SetState(v1alpha.ReconcileErr)
		r.Update(context.Background(), ctx.GetInstanceGroup())
		return err
	}
	err = HandleReconcileRequest(ctx)
	if err != nil {
		ctx.SetState(v1alpha.ReconcileErr)
		r.Update(context.Background(), ctx.GetInstanceGroup())
		return err
	}
	r.Finalize(instanceGroup, finalizerName)
	return r.Update(context.Background(), instanceGroup)
}

func (r *InstanceGroupReconciler) ReconcileEKSManaged(instanceGroup *v1alpha.InstanceGroup, finalizerName string) error {
	log.Infof("upgrade strategy: %v", strings.ToLower(instanceGroup.Spec.AwsUpgradeStrategy.Type))

	client, err := common.GetKubernetesClient()
	if err != nil {
		return err
	}

	dynClient, err := common.GetKubernetesDynamicClient()
	if err != nil {
		return err
	}

	awsRegion, err := aws.GetRegion()
	if err != nil {
		return err
	}

	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	if _, err := os.Stat(r.ControllerConfPath); os.IsNotExist(err) {
		log.Errorf("controller config file not found: %v", err)
		return err
	}

	controllerConfig, err := common.ReadFile(r.ControllerConfPath)
	if err != nil {
		return err
	}

	_, err = eksmanaged.LoadControllerConfiguration(instanceGroup, controllerConfig)
	if err != nil {
		log.Errorf("failed to load controller configuration: %v", err)
		return err
	}

	awsWorker := aws.AwsWorker{
		AsgClient: aws.GetAwsAsgClient(awsRegion),
		EksClient: aws.GetAwsEksClient(awsRegion),
	}

	ctx, err := eksmanaged.New(instanceGroup, kube, awsWorker)
	if err != nil {
		return fmt.Errorf("failed to create a new eksmanaged provisioner: %v", err)
	}
	ctx.ControllerRegion = awsRegion

	err = HandleReconcileRequest(ctx)
	if err != nil {
		ctx.SetState(v1alpha.ReconcileErr)
		r.Update(context.Background(), ctx.GetInstanceGroup())
		return err
	}
	// Remove finalizer if deleted
	r.Finalize(instanceGroup, finalizerName)

	// Update resource with changes
	err = r.Update(context.Background(), ctx.GetInstanceGroup())
	if err != nil {
		return err
	}
	return nil
}

func (r *InstanceGroupReconciler) ReconcileEKSCF(instanceGroup *v1alpha.InstanceGroup, finalizerName string) error {
	log.Infof("upgrade strategy: %v", strings.ToLower(instanceGroup.Spec.AwsUpgradeStrategy.Type))
	var specConfig = instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration

	client, err := common.GetKubernetesClient()
	if err != nil {
		return err
	}

	dynClient, err := common.GetKubernetesDynamicClient()
	if err != nil {
		return err
	}

	awsRegion, err := aws.GetRegion()
	if err != nil {
		return err
	}

	kube := common.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	if _, err := os.Stat(r.ControllerConfPath); os.IsNotExist(err) {
		log.Errorf("controller config file not found: %v", err)
		return err
	}

	controllerConfig, err := common.ReadFile(r.ControllerConfPath)
	if err != nil {
		return err
	}

	defaultConfiguration, err := ekscloudformation.LoadControllerConfiguration(instanceGroup, controllerConfig)
	if err != nil {
		log.Errorf("failed to load controller configuration: %v", err)
		return err
	}

	template, err := ekscloudformation.LoadCloudformationConfiguration(instanceGroup, r.ControllerTemplatePath)
	if err != nil {
		log.Errorf("failed to load cloudformation configuration: %v", err)
		return err
	}

	var stackName string
	if defaultConfiguration.StackNamePrefix != "" {
		stackName = fmt.Sprintf("%v-%v-%v", defaultConfiguration.StackNamePrefix, specConfig.GetClusterName(), instanceGroup.GetName())
	} else {
		stackName = fmt.Sprintf("%v-%v", specConfig.GetClusterName(), instanceGroup.GetName())
	}

	// set the stack name if it is unset
	if instanceGroup.Status.GetStackName() == "" {
		instanceGroup.Status.SetStackName(stackName)
	}

	awsWorker := aws.AwsWorker{
		CfClient:     aws.GetAwsCloudformationClient(awsRegion),
		AsgClient:    aws.GetAwsAsgClient(awsRegion),
		EksClient:    aws.GetAwsEksClient(awsRegion),
		StackName:    instanceGroup.Status.GetStackName(),
		TemplateBody: template,
	}

	ctx, err := ekscloudformation.New(instanceGroup, kube, awsWorker)
	if err != nil {
		return fmt.Errorf("failed to create a new ekscloudformation provisioner: %v", err)
	}

	ctx.ControllerRegion = awsRegion
	if len(defaultConfiguration.DefaultARNs) != 0 {
		ctx.DefaultARNList = defaultConfiguration.DefaultARNs
	}

	ctx.TemplatePath = r.ControllerTemplatePath

	// Init State is set when handling reconcile
	// Reconcile Handler
	err = HandleReconcileRequest(ctx)
	if err != nil {
		ctx.SetState(v1alpha.ReconcileErr)
		r.Update(context.Background(), ctx.GetInstanceGroup())
		return err
	}
	// Remove finalizer if deleted
	r.Finalize(instanceGroup, finalizerName)

	// Update resource with changes
	err = r.Update(context.Background(), ctx.GetInstanceGroup())
	if err != nil {
		return err
	}

	return nil
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=instancemgr.keikoproj.io,resources=instancegroups/status,verbs=get;update;patch

func (r *InstanceGroupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("instancegroup", req.NamespacedName)

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Read InstanceGroup Resource, requeue if not found
	fmt.Println()
	log.Infoln("reconcile started")

	ig := &v1alpha.InstanceGroup{}
	err := r.Get(context.Background(), req.NamespacedName, ig)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Warnf("instancegroup document not found: '%v'", req.Name)
			log.Infoln("reconcile completed")
			return ctrl.Result{}, nil
		}
		log.Errorln(err)
		log.Errorln("reconcile failed")
		return ctrl.Result{}, err
	}

	log.Infof("resource document version: %v", ig.ObjectMeta.ResourceVersion)
	log.Infof("resource namespace: %v", ig.ObjectMeta.Namespace)
	log.Infof("resource name: %v", ig.ObjectMeta.Name)
	log.Infof("resource provisioner: %v", strings.ToLower(ig.Spec.Provisioner))

	finalizerName := fmt.Sprintf("finalizers.%v.instancegroups.keikoproj.io", ig.Spec.Provisioner)
	r.SetFinalizer(ig, finalizerName)

	switch strings.ToLower(ig.Spec.Provisioner) {
	// Provisioner EKS-Cloudformation Reconciler
	case "eks-managed":
		err := r.ReconcileEKSManaged(ig, finalizerName)
		if err != nil {
			log.Errorln(err)
		}
		currentState := ig.GetState()
		if currentState == v1alpha.ReconcileErr {
			log.Errorln("reconcile failed")
			return ctrl.Result{}, nil
		} else if currentState == v1alpha.ReconcileDeleted || currentState == v1alpha.ReconcileReady {
			log.Infoln("reconcile completed")
			return ctrl.Result{}, nil
		} else {
			log.Infoln("reconcile completed with requeue")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case "eks-cf":
		err := r.ReconcileEKSCF(ig, finalizerName)
		if err != nil {
			log.Errorln(err)
		}
		currentState := ig.GetState()
		if currentState == v1alpha.ReconcileErr {
			log.Errorln("reconcile failed")
			return ctrl.Result{}, nil
		} else if currentState == v1alpha.ReconcileDeleted || currentState == v1alpha.ReconcileReady {
			log.Infoln("reconcile completed")
			return ctrl.Result{}, nil
		} else {
			log.Infoln("reconcile completed with requeue")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case "eks-fargate":
		err := r.ReconcileEKSFargate(ig, finalizerName)
		if err != nil {
			log.Errorln(err)
		}
		currentState := ig.GetState()
		if currentState == v1alpha.ReconcileErr {
			log.Errorln("fargate reconcile failed")
			return ctrl.Result{}, nil
		} else if currentState == v1alpha.ReconcileDeleted || currentState == v1alpha.ReconcileReady {
			log.Infoln("fargate reconcile completed")
			return ctrl.Result{}, nil
		} else {
			log.Infoln("fargate reconcile completed with requeue")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

	default:
		log.Errorln("provisioner not implemented")
		return ctrl.Result{}, fmt.Errorf("provisioner '%v' not implemented", ig.Spec.Provisioner)
	}
}

func (r *InstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha.InstanceGroup{}).
		Watches(&source.Kind{Type: &corev1.Event{}}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.spotEventReconciler),
		}).
		Complete(r)
}

func (r *InstanceGroupReconciler) spotEventReconciler(obj handler.MapObject) []ctrl.Request {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.Object)
	if err != nil {
		return nil
	}

	reason, exists, err := unstructured.NestedString(unstructuredObj, "reason")
	if !exists || err != nil {
		return nil
	}
	if reason != ekscloudformation.SpotRecommendationReason {
		return nil
	}

	ctrl.Log.Info(fmt.Sprintf("spot recommendation %v/%v", obj.Meta.GetNamespace(), obj.Meta.GetName()))

	involvedObjectName, exists, err := unstructured.NestedString(unstructuredObj, "involvedObject", "name")
	if err != nil || !exists {
		log.Warnf("failed to process event '%v': %v", obj.Meta.GetName(), err)
		return nil
	}

	tags, err := aws.GetScalingGroupTagsByName(involvedObjectName, r.ScalingGroups)
	if err != nil {
		log.Warnf("failed to process event '%v': could not find scaling group", obj.Meta.GetName())
		return nil
	}
	instanceGroup := types.NamespacedName{}
	instanceGroup.Name = aws.GetTagValueByKey(tags, ekscloudformation.TagInstanceGroupName)
	instanceGroup.Namespace = aws.GetTagValueByKey(tags, ekscloudformation.TagClusterNamespace)
	if instanceGroup.Name == "" || instanceGroup.Namespace == "" {
		log.Warnf("failed to process event '%v': could not derive instancegroup", obj.Meta.GetName())
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: instanceGroup,
		},
	}
}
