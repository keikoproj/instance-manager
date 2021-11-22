package provisioners

import (
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
)

var (
	log = ctrl.Log.WithName("provisioners")
)

const (
	TagClusterName            = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName      = "instancegroups.keikoproj.io/InstanceGroup"
	TagInstanceGroupNamespace = "instancegroups.keikoproj.io/Namespace"
	TagClusterOwnershipFmt    = "kubernetes.io/cluster/%s"
	TagKubernetesCluster      = "KubernetesCluster"

	ConfigurationExclusionAnnotationKey = "instancemgr.keikoproj.io/config-excluded"
	UpgradeLockedAnnotationKey          = "instancemgr.keikoproj.io/lock-upgrades"
)

type ProvisionerInput struct {
	AwsWorker                  awsprovider.AwsWorker
	Kubernetes                 kubeprovider.KubernetesClientSet
	InstanceGroup              *v1alpha1.InstanceGroup
	Configuration              *corev1.ConfigMap
	Log                        logr.Logger
	ConfigRetention            int
	Metrics                    *common.MetricsCollector
	DisableWinClusterInjection bool
}

var (
	NonRetryableStates = []v1alpha1.ReconcileState{v1alpha1.ReconcileErr, v1alpha1.ReconcileReady, v1alpha1.ReconcileDeleted, v1alpha1.ReconcileLocked}
)

func IsRetryable(instanceGroup *v1alpha1.InstanceGroup) bool {
	for _, state := range NonRetryableStates {
		if state == instanceGroup.GetState() {
			return false
		}
	}
	return true
}
