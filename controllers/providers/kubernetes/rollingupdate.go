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

	// awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"

	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

const (
	RollingUpdateStrategyName = "rollingupdate"
)

type RollingUpdateRequest struct {
	AwsWorker       awsprovider.AwsWorker
	Kubernetes      kubernetes.Interface
	MaxUnavailable  int
	DesiredCapacity int
	AllInstances    []string
	UpdateTargets   []string
}

func ProcessRollingUpgradeStrategy(req *RollingUpdateRequest) (bool, error) {

	log.Infof("starting rolling update on %v", req.UpdateTargets)

	if len(req.UpdateTargets) == 0 {
		log.Info("no updatable instances")
		return true, nil
	}

	// cannot rotate if maxUnavailable is greater than number of desired
	if req.MaxUnavailable > req.DesiredCapacity {
		log.Warnf("maxUnavailable '%v' exceeds desired capacity, setting maxUnavailable to '%v'", req.MaxUnavailable, req.DesiredCapacity)
		req.MaxUnavailable = req.DesiredCapacity
	}

	ok, err := IsMinNodesReady(req.Kubernetes, req.AllInstances, req.MaxUnavailable)
	if err != nil {
		return false, err
	}

	if !ok {
		log.Info("desired nodes are not ready")
		return false, nil
	}

	var terminateTargets []string
	if req.MaxUnavailable <= len(req.UpdateTargets) {
		terminateTargets = req.UpdateTargets[:req.MaxUnavailable]
	} else {
		terminateTargets = req.UpdateTargets
	}

	log.Infof("terminating %v targets -> %v", len(terminateTargets), terminateTargets)
	if err := req.AwsWorker.TerminateScalingInstances(terminateTargets); err != nil {
		return false, err
	}

	return false, nil
}
