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

package kubernetes

import (
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	RollingUpdateStrategyName = "rollingupdate"
)

var (
	log = ctrl.Log.WithName("kubernetes-provider")
)

type RollingUpdateRequest struct {
	AwsWorker        awsprovider.AwsWorker
	Kubernetes       kubernetes.Interface
	ScalingGroupName string
	MaxUnavailable   int
	DesiredCapacity  int
	AllInstances     []string
	UpdateTargets    []string
}

func ProcessRollingUpgradeStrategy(req *RollingUpdateRequest) (bool, error) {

	log.Info("starting rolling update",
		"scalinggroup", req.ScalingGroupName,
		"targets", req.UpdateTargets,
		"maxunavailable", req.MaxUnavailable,
	)
	if len(req.UpdateTargets) == 0 {
		log.Info("no updatable instances", "scalinggroup", req.ScalingGroupName)
		return true, nil
	}

	// cannot rotate if maxUnavailable is greater than number of desired
	if req.MaxUnavailable > req.DesiredCapacity {
		log.Info("maxUnavailable exceeds desired capacity, setting maxUnavailable match desired",
			"scalinggroup", req.ScalingGroupName,
			"maxunavailable", req.MaxUnavailable,
			"desiredcapacity", req.DesiredCapacity,
		)
		req.MaxUnavailable = req.DesiredCapacity
	}

	ok, err := IsMinNodesReady(req.Kubernetes, req.AllInstances, req.MaxUnavailable)
	if err != nil {
		return false, err
	}

	if !ok {
		log.Info("desired nodes are not ready", "scalinggroup", req.ScalingGroupName)
		return false, nil
	}

	var terminateTargets []string
	if req.MaxUnavailable <= len(req.UpdateTargets) {
		terminateTargets = req.UpdateTargets[:req.MaxUnavailable]
	} else {
		terminateTargets = req.UpdateTargets
	}

	log.Info("terminating targets", "scalinggroup", req.ScalingGroupName, "targets", terminateTargets)
	if err := req.AwsWorker.TerminateScalingInstances(terminateTargets); err != nil {
		return false, err
	}

	return false, nil
}
