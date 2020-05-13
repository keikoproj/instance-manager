package provisioners

import (
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
)

const (
	TagClusterName            = "instancegroups.keikoproj.io/ClusterName"
	TagInstanceGroupName      = "instancegroups.keikoproj.io/InstanceGroup"
	TagInstanceGroupNamespace = "instancegroups.keikoproj.io/Namespace"
	TagClusterOwnershipFmt    = "kubernetes.io/cluster/%s"
	TagKubernetesCluster      = "KubernetesCluster"
)

type ProvisionerInput struct {
	AwsWorker     awsprovider.AwsWorker
	Kubernetes    kubeprovider.KubernetesClientSet
	InstanceGroup *v1alpha1.InstanceGroup
	Configuration ProvisionerConfiguration
	Log           logr.Logger
}

type ProvisionerConfiguration struct {
	DefaultClusterName string
	DefaultSubnets     []string
}
