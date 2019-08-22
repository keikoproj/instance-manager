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

	instancemgrv1alpha1 "github.com/orkaproj/instance-manager/api/v1alpha1"
	log "github.com/sirupsen/logrus"

	"github.com/orkaproj/instance-manager/controllers"

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

const controllerVersion = "instancemgr-0.3.1"

func init() {

	instancemgrv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	log.Printf("Go Version: %s", runt.Version())
	log.Printf("Go OS/Arch: %s/%s", runt.GOOS, runt.GOARCH)
	log.Printf("Controller Version: %s", controllerVersion)
	log.Println()
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	printVersion()

	var metricsAddr string
	var enableLeaderElection bool
	var controllerConfPath string
	var controllerTemplatePath string
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

	err = (&controllers.InstanceGroupReconciler{
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("InstanceGroup"),
		ControllerConfPath:     controllerConfPath,
		ControllerTemplatePath: controllerTemplatePath,
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "InstanceGroup")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
