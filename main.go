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
	"context"
	"flag"
	"os"
	runt "runtime"
	"sync"

	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const controllerVersion = "instancemgr-0.14.1"

func init() {
	v1alpha1.AddToScheme(scheme)
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
		metricsAddr                string
		configNamespace            string
		spotRecommendationTime     float64
		enableLeaderElection       bool
		nodeRelabel                bool
		disableWinClusterInjection bool
		withVsp                    bool
		maxParallel                int
		maxAPIRetries              int
		configRetention            int
		err                        error
	)

	flag.IntVar(&maxParallel, "max-workers", 5, "The number of maximum parallel reconciles")
	flag.IntVar(&maxAPIRetries, "max-api-retries", 12, "The number of maximum retries for failed AWS API calls")
	flag.IntVar(&configRetention, "config-retention", 2, "The number of launch configuration/template versions to retain")
	flag.Float64Var(&spotRecommendationTime, "spot-recommendation-time", 10.0, "The maximum age of spot recommendation events to consider in minutes")
	flag.StringVar(&configNamespace, "config-namespace", "instance-manager", "the namespace to watch for instance-manager configmap")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&nodeRelabel, "node-relabel", true, "relabel nodes as they join with kubernetes.io/role label via controller")
	flag.BoolVar(&withVsp, "with-vertical-scaling-policy", true, "Run the verticalscalingpolicy controller (only supported with EKS provisioner)")
	flag.BoolVar(&disableWinClusterInjection, "disable-windows-cluster-ca-injection", false, "Setting this to true will cause the ClusterCA and Endpoint to not be injected for Windows nodes")

	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	metadata := aws.GetAwsEc2MetadataClient()
	awsRegion, err := aws.GetRegion(metadata)
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

	cacheCfg := cache.NewConfig(aws.CacheDefaultTTL, aws.CacheBackgroundPruningInterval, aws.CacheMaxItems, aws.CacheItemsToPrune)
	cacheCollector := cacheCfg.NewCacheCollector("instance_manager")
	controllerCollector := common.NewMetricsCollector()
	awsWorker := aws.AwsWorker{
		Ec2Client:   aws.GetAwsEc2Client(awsRegion, cacheCfg, maxAPIRetries, controllerCollector),
		IamClient:   aws.GetAwsIamClient(awsRegion, cacheCfg, maxAPIRetries, controllerCollector),
		AsgClient:   aws.GetAwsAsgClient(awsRegion, cacheCfg, maxAPIRetries, controllerCollector),
		EksClient:   aws.GetAwsEksClient(awsRegion, cacheCfg, maxAPIRetries, controllerCollector),
		SsmClient:   aws.GetAwsSsmClient(awsRegion, cacheCfg, maxAPIRetries, controllerCollector),
		Ec2Metadata: metadata,
	}

	metrics.Registry.MustRegister(cacheCollector, controllerCollector)
	kube := kubeprovider.KubernetesClientSet{
		Kubernetes:  client,
		KubeDynamic: dynClient,
	}

	var cm *corev1.ConfigMap
	cm, err = client.CoreV1().ConfigMaps(configNamespace).Get(context.Background(), controllers.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			setupLog.Error(err, "could not get instance-manager configmap")
			os.Exit(1)
		}
		cm = &corev1.ConfigMap{}
		setupLog.Info("instance-manager configmap does not exist, will not load defaults/boundaries")
	}

	sharedContext := &controllers.SharedContext{
		Policies:            make(map[string]v1alpha1.VerticalScalingPolicy),
		ComputedTypes:       make(map[string]string),
		InstanceGroupEvents: make(chan event.GenericEvent),
		RWMutex:             sync.RWMutex{},
	}

	err = (&controllers.InstanceGroupReconciler{
		Metrics:                    controllerCollector,
		ConfigMap:                  cm,
		ConfigRetention:            configRetention,
		SpotRecommendationTime:     spotRecommendationTime,
		ConfigNamespace:            configNamespace,
		Namespaces:                 make(map[string]corev1.Namespace),
		NamespacesLock:             &sync.RWMutex{},
		NodeRelabel:                nodeRelabel,
		DisableWinClusterInjection: disableWinClusterInjection,
		Client:                     mgr.GetClient(),
		Log:                        ctrl.Log.WithName("controllers").WithName("instancegroup"),
		MaxParallel:                maxParallel,
		ManagerContext:             sharedContext,
		Auth: &controllers.InstanceGroupAuthenticator{
			Aws:        awsWorker,
			Kubernetes: kube,
		},
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "instancegroup")
		os.Exit(1)
	}

	if withVsp {
		if err = (&controllers.VerticalScalingPolicyReconciler{
			Client:         mgr.GetClient(),
			Log:            ctrl.Log.WithName("controllers").WithName("verticalscalingpolicy"),
			ManagerContext: sharedContext,
			Auth: &controllers.InstanceGroupAuthenticator{
				Aws:        awsWorker,
				Kubernetes: kube,
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "verticalscalingpolicy")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
