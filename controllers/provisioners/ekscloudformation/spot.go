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

package ekscloudformation

import (
	"reflect"

	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
)

func (ctx *EksCfInstanceGroupContext) discoverSpotPrice() {
	var (
		instanceGroup     = ctx.GetInstanceGroup()
		status            = &instanceGroup.Status
		spec              = &instanceGroup.Spec
		provisionerConfig = spec.EKSCFSpec
		specConfig        = provisionerConfig.EKSCFConfiguration
		scalingGroupName  = status.GetActiveScalingGroupName()
	)

	// get recommendations from events
	recommendation, err := kubeprovider.GetSpotRecommendation(ctx.KubernetesClient.Kubernetes, scalingGroupName)
	if err != nil {
		specConfig.SetSpotPrice("")
		return
	}

	// if no recommendations found
	if reflect.DeepEqual(recommendation, kubeprovider.SpotRecommendation{}) {
		// if it was using a recommendation before, set to false and leave price manually set
		if status.GetUsingSpotRecommendation() {
			status.SetUsingSpotRecommendation(false)
		} else {
			// if spotPrice is set without recommendation in custom resource
			if specConfig.GetSpotPrice() != "" {
				// keep custom resource provided value
				log.Warnf("using spot-price '%v' without recommendations, use a recommendations controller or risk losing all instances", specConfig.GetSpotPrice())
			}
		}
		return
	}

	// process event message
	status.SetUsingSpotRecommendation(true)

	if recommendation.UseSpot {
		log.Infof("spot enabled with current bid: %v", recommendation.SpotPrice)
		specConfig.SetSpotPrice(recommendation.SpotPrice)
	} else {
		log.Infoln("use-spot is disabled")
		specConfig.SetSpotPrice("")
	}
	ctx.reloadCloudformationConfiguration()
}
