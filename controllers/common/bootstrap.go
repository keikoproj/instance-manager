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

package common

import (
	awsauth "github.com/keikoproj/aws-auth/pkg/mapper"
	"k8s.io/client-go/kubernetes"
)

func GetNodeBootstrapUpsert(arn string) *awsauth.UpsertArguments {
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

func GetNodeBootstrapRemove(arn string) *awsauth.RemoveArguments {
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

func RemoveAuthConfigMap(kube kubernetes.Interface, arns []string) error {
	authMap := awsauth.New(kube, false)
	for _, arn := range arns {
		if arn == "" {
			continue
		}
		err := authMap.Remove(GetNodeBootstrapRemove(arn))
		if err != nil {
			return err
		}
	}
	return nil
}

func UpsertAuthConfigMap(kube kubernetes.Interface, arns []string) error {
	authMap := awsauth.New(kube, false)
	for _, arn := range arns {
		if arn == "" {
			continue
		}
		err := authMap.Upsert(GetNodeBootstrapUpsert(arn))
		if err != nil {
			return err
		}
	}
	return nil
}
