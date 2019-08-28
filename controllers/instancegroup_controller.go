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
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/go-logr/logr"
	log "github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	"github.com/keikoproj/instance-manager/controllers/provisioners/ekscloudformation"
	"k8s.io/apimachinery/pkg/api/errors"
)

// InstanceGroupReconciler reconciles an InstanceGroup object
type InstanceGroupReconciler struct {
	client.Client
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

func (r *InstanceGroupReconciler) ReconcileEKSCF(instanceGroup *v1alpha.InstanceGroup, finalizerName string) error {
	log.Infof("upgrade strategy: %v", strings.ToLower(instanceGroup.Spec.AwsUpgradeStrategy.Type))
	var specConfig = &instanceGroup.Spec.EKSCFSpec.EKSCFConfiguration

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

	defaultARNList, err := r.loadControllerConfiguration(instanceGroup)
	if err != nil {
		log.Errorf("failed to load controller configuration: %v", err)
		return err
	}

	template, err := r.loadCloudformationConfiguration(instanceGroup)
	if err != nil {
		log.Errorf("failed to load cloudformation configuration: %v", err)
		return err
	}

	awsWorker := aws.AwsWorker{
		CfClient:     aws.GetAwsCloudformationClient(awsRegion),
		AsgClient:    aws.GetAwsAsgClient(awsRegion),
		EksClient:    aws.GetAwsEksClient(awsRegion),
		StackName:    fmt.Sprintf("%v-%v-%v", specConfig.GetClusterName(), instanceGroup.GetNamespace(), instanceGroup.GetName()),
		TemplateBody: template,
	}

	ctx, err := ekscloudformation.New(instanceGroup, kube, awsWorker)
	if err != nil {
		return fmt.Errorf("failed to create a new ekscloudformation provisioner: %v", err)
	}

	ctx.ControllerRegion = awsRegion
	ctx.DefaultARNList = defaultARNList

	// Init State is set when handling reconcile
	// Reconcile Handler
	err = HandleReconcileRequest(&ctx)
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

	// Add Finalizer if not present, and set the initial state
	finalizerName := fmt.Sprintf("finalizers.%v.instancegroups.keikoproj.io", ig.Spec.Provisioner)
	r.SetFinalizer(ig, finalizerName)

	switch strings.ToLower(ig.Spec.Provisioner) {
	// Provisioner EKS-Cloudformation Reconciler
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
	default:
		log.Errorln("provisioner not implemented")
		return ctrl.Result{}, fmt.Errorf("provisioner '%v' not implemented", ig.Spec.Provisioner)
	}
}

func (r *InstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha.InstanceGroup{}).
		Complete(r)
}

func (r *InstanceGroupReconciler) loadCloudformationConfiguration(ig *v1alpha.InstanceGroup) (string, error) {
	var renderBuffer bytes.Buffer

	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
	}

	if _, err := os.Stat(r.ControllerTemplatePath); os.IsNotExist(err) {
		log.Errorf("controller cloudformation template file not found: %v", err)
		return "", err
	}

	rawTemplate, err := common.ReadFile(r.ControllerTemplatePath)
	if err != nil {
		return "", err
	}

	template, err := template.New("InstanceGroup").Funcs(funcMap).Parse(string(rawTemplate))
	if err != nil {
		return "", err
	}

	err = template.Execute(&renderBuffer, ig)
	if err != nil {
		return "", err
	}

	return renderBuffer.String(), nil
}

func (r *InstanceGroupReconciler) loadControllerConfiguration(ig *v1alpha.InstanceGroup) ([]string, error) {
	var defaultConfig ekscloudformation.EksCfDefaultConfiguration
	var specConfig = &ig.Spec.EKSCFSpec.EKSCFConfiguration
	var defaultARNs []string

	if _, err := os.Stat(r.ControllerConfPath); os.IsNotExist(err) {
		log.Errorf("controller config file not found: %v", err)
		return nil, err
	}

	controllerConfig, err := common.ReadFile(r.ControllerConfPath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(controllerConfig, &defaultConfig)
	if err != nil {
		return nil, err
	}

	if len(defaultConfig.DefaultSubnets) != 0 {
		specConfig.SetSubnets(defaultConfig.DefaultSubnets)
	}

	if defaultConfig.EksClusterName != "" {
		specConfig.SetClusterName(defaultConfig.EksClusterName)
	}

	if len(defaultConfig.DefaultARNs) != 0 {
		defaultARNs = defaultConfig.DefaultARNs
	}

	return defaultARNs, nil
}
