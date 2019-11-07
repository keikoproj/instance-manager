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
	awsauth "github.com/keikoproj/aws-auth/pkg/mapper"
	"github.com/keikoproj/instance-manager/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getNodeUpsert(arn string) *awsauth.UpsertArguments {
	return &awsauth.UpsertArguments{
		MapRoles: true,
		RoleARN:  arn,
		Username: "system:node:{{EC2PrivateDNSName}}",
		Groups: []string{
			"system:bootstrappers",
			"system:nodes",
		},
	}
}

func getNodeRemove(arn string) *awsauth.RemoveArguments {
	return &awsauth.RemoveArguments{
		MapRoles: true,
		RoleARN:  arn,
		Username: "system:node:{{EC2PrivateDNSName}}",
		Groups: []string{
			"system:bootstrappers",
			"system:nodes",
		},
	}
}

func (ctx *EksCfInstanceGroupContext) updateAuthConfigMap() error {
	var (
		discovery      = ctx.GetDiscoveredState()
		instanceGroups = discovery.GetInstanceGroups()
		selfARN        = discovery.GetSelfGroup().GetARN()
	)

	// create aws-auth config map if it doesnt already exist
	_, err := getConfigMap(ctx.KubernetesClient.Kubernetes, "kube-system", "aws-auth", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infoln("auth configmap not found, creating it")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kube-system",
					Name:      "aws-auth",
				},
				Data: nil,
			}
			err := createConfigMap(ctx.KubernetesClient.Kubernetes, cm)
			if err != nil {
				return err
			}
		}
	}

	authMap := awsauth.New(ctx.KubernetesClient.Kubernetes, false)

	// Upsert ARNs from discovered state
	for _, instanceGroup := range instanceGroups.Items {
		if instanceGroup.ARN == "" {
			continue
		}
		err = authMap.Upsert(getNodeUpsert(instanceGroup.ARN))
		if err != nil {
			return err
		}
	}
	// Upsert ARNs from controller config
	for _, arn := range ctx.DefaultARNList {
		if arn == "" {
			continue
		}
		err = authMap.Upsert(getNodeUpsert(arn))
		if err != nil {
			return err
		}
	}
	// Remove selfARN in case of deletion event
	if ctx.GetState() == v1alpha1.ReconcileInitDelete && selfARN != "" {
		err = authMap.Remove(getNodeRemove(selfARN))
		if err != nil {
			return err
		}
	}

	return nil
}
