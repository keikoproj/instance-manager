package provisioners

import (
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
)

type ProvisionerInput struct {
	AwsWorker     awsprovider.AwsWorker
	Kubernetes    common.KubernetesClientSet
	InstanceGroup *v1alpha1.InstanceGroup
	Configuration ProvisionerConfiguration
	Log           logr.Logger
}

type ProvisionerConfiguration struct {
	DefaultClusterName string
	DefaultSubnets     []string
}
