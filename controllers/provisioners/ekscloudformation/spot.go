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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SpotRecommendationReason  = "SpotRecommendationGiven"
	SpotRecommendationVersion = "v1alpha1"
)

func (ctx *EksCfInstanceGroupContext) discoverSpotPrice() {
	var (
		instanceGroup     = ctx.GetInstanceGroup()
		status            = &instanceGroup.Status
		spec              = &instanceGroup.Spec
		provisionerConfig = spec.EKSCFSpec
		specConfig        = &provisionerConfig.EKSCFConfiguration
		scalingGroupName  = status.GetActiveScalingGroupName()
	)

	// get recommendations from events
	recommendation, err := ctx.getLatestSpotRecommendation(scalingGroupName)
	if err != nil {
		specConfig.SetSpotPrice("")
		return
	}

	// if no recommendations found
	if reflect.DeepEqual(*recommendation, SpotRecommendation{}) {
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

	if recommendation.APIVersion != SpotRecommendationVersion {
		err := fmt.Sprintf("apiVersion '%v' is unknown", recommendation.APIVersion)
		log.Warnf("failed to process spot recommendation, will override to on-demand: %v", err)
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

func (ctx *EksCfInstanceGroupContext) getLatestSpotRecommendation(scalingGroupName string) (*SpotRecommendation, error) {
	var recommendations SpotReccomendations

	fieldSelector := fmt.Sprintf("reason=%v,involvedObject.name=%v", SpotRecommendationReason, scalingGroupName)

	listOpts := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	eventList, err := ctx.KubernetesClient.Kubernetes.CoreV1().Events("").List(listOpts)
	if err != nil {
		return &SpotRecommendation{}, err
	}

	for _, event := range eventList.Items {
		recommendation := &SpotRecommendation{
			EventTime: event.LastTimestamp.Time,
		}
		err := json.Unmarshal([]byte(event.Message), recommendation)
		if err != nil {
			return &SpotRecommendation{}, err
		}
		recommendations = append(recommendations, *recommendation)
	}
	sort.Sort(sort.Reverse(recommendations))

	if len(recommendations) < 1 {
		return &SpotRecommendation{}, nil
	}

	latestRecommendation := &recommendations[0]

	return latestRecommendation, nil

}
