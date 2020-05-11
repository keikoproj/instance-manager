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
	"flag"
	"os"
	runt "runtime"

	instancemgrv1alpha1 "github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const controllerVersion = "instancemgr-0.5.0"

func init() {
	instancemgrv1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	setupLog.Info("controller starting",
		"go-version", runt.Version(),
		"os", runt.GOOS,
		"arch", runt.GOARCH,
		"version", controllerVersion,
	)
}

func main() {
	printVersion()

	var (
		metricsAddr            string
		enableLeaderElection   bool
		controllerConfPath     string
		controllerTemplatePath string
		maxParallel            int
		err                    error
	)

	flag.IntVar(&maxParallel, "max-workers", 5, "The number of maximum parallel reconciles")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&controllerConfPath, "controller-config", "/etc/config/controller.conf", "The controller config file")
	flag.StringVar(&controllerTemplatePath, "controller-template", "/etc/config/cloudformation.template", "The controller template file")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.Parse()
	ctrl.SetLogger(zap.Logger(true))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	awsRegion, err := aws.GetRegion()
	if err != nil {
		setupLog.Error(err, "unable to get AWS region")
		os.Exit(1)
	}

	client, err := kubeprovider.GetKubernetesClient()
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		os.Exit(1)
	}

	dynClient, err := kubeprovider.GetKubernetesDynamicClient()
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes dynamic client")
		os.Exit(1)
	}

	awsWorker := aws.AwsWorker{
		IamClient: aws.GetAwsIamClient(awsRegion),
		CfClient:  aws.GetAwsCloudformationClient(awsRegion),
		AsgClient: aws.GetAwsAsgClient(awsRegion),
		EksClient: aws.GetAwsEksClient(awsRegion),
	}

	kube := kubeprovider.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	err = (&controllers.InstanceGroupReconciler{
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("instancegroup"),
		ControllerConfPath:     controllerConfPath,
		ControllerTemplatePath: controllerTemplatePath,
		MaxParallel:            maxParallel,
		Auth: &controllers.InstanceGroupAuthenticator{
			Aws:        awsWorker,
			Kubernetes: kube,
		},
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "instancegroup")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
