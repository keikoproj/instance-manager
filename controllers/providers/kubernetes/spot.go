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
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	SpotRecommendationReason  = "SpotRecommendationGiven"
	SpotRecommendationVersion = "v1alpha1"
)

type SpotRecommendation struct {
	APIVersion string `yaml:"apiVersion"`
	SpotPrice  string `yaml:"spotPrice"`
	UseSpot    bool   `yaml:"useSpot"`
	EventTime  time.Time
}

type SpotReccomendationList []SpotRecommendation

func GetSpotRecommendation(kube kubernetes.Interface, identifier string) (SpotRecommendation, error) {
	var recommendations SpotReccomendationList

	fieldSelector := fmt.Sprintf("reason=%v,involvedObject.name=%v", SpotRecommendationReason, identifier)

	eventList, err := kube.CoreV1().Events("").List(context.Background(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return SpotRecommendation{}, err
	}

	recommendation := &SpotRecommendation{}
	for _, event := range eventList.Items {
		err := json.Unmarshal([]byte(event.Message), recommendation)
		if err != nil {
			return SpotRecommendation{}, err
		}
		recommendation.EventTime = event.LastTimestamp.Time
		recommendations = append(recommendations, *recommendation)
	}
	sort.Sort(sort.Reverse(recommendations))

	if len(recommendations) == 0 {
		return SpotRecommendation{}, nil
	}

	return recommendations[0], nil
}

func (p SpotReccomendationList) Len() int {
	return len(p)
}

func (p SpotReccomendationList) Less(i, j int) bool {
	return p[i].EventTime.Before(p[j].EventTime)
}

func (p SpotReccomendationList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}
