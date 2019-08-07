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
	"sort"

	"github.com/orkaproj/instance-manager/controllers/common"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (ctx *EksCfInstanceGroupContext) getActiveNodeArns() []string {
	var arnList []string
	discovery := ctx.GetDiscoveredState()
	instanceGroups := discovery.GetInstanceGroups()
	// Append ARNs from discovered state
	for _, instanceGroup := range instanceGroups.Items {
		if !common.ContainsString(arnList, instanceGroup.ARN) {
			arnList = append(arnList, instanceGroup.ARN)
		}
	}
	// Append ARNs from controller config
	for _, arn := range ctx.DefaultARNList {
		if !common.ContainsString(arnList, arn) {
			arnList = append(arnList, arn)
		}
	}
	sort.Strings(arnList)
	return arnList
}

func (ctx *EksCfInstanceGroupContext) updateAuthConfigMap() error {
	arnList := ctx.getActiveNodeArns()
	configList := []AwsAuthConfig{}
	for _, arn := range arnList {
		log.Infof("bootstrapping: %v\n", arn)
		authConfig := AwsAuthConfig{
			RoleARN:  arn,
			Username: "system:node:{{EC2PrivateDNSName}}",
			Groups: []string{
				"system:bootstrappers",
				"system:nodes",
			},
		}
		configList = append(configList, authConfig)
	}

	maproles := AwsAuthConfigMapRolesData{
		MapRoles: configList,
	}

	d, err := yaml.Marshal(&maproles.MapRoles)
	if err != nil {
		log.Errorf("error: %v", err)
	}
	data := map[string]string{
		"mapRoles": string(d),
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "aws-auth",
		},
		Data: data,
	}

	err = updateConfigMap(ctx.KubernetesClient.Kubernetes, cm)
	if err != nil {
		log.Errorf("error: %v", err)
	}
	return nil

}

func (ctx *EksCfInstanceGroupContext) createEmptyNodesAuthConfigMap() error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "aws-auth",
		},
		Data: nil,
	}
	createConfigMap(ctx.KubernetesClient.Kubernetes, cm)
	return nil
}
