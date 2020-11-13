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
	"strings"
	"time"

	awsauth "github.com/keikoproj/aws-auth/pkg/mapper"
	"k8s.io/client-go/kubernetes"
)

func GetGroupsForOsFamily(osFamily string) []string {
	if strings.EqualFold(osFamily, "windows") {
		return []string{
			"system:bootstrappers",
			"system:nodes",
			"eks:kube-proxy-windows",
		}
	} else {
		return []string{
			"system:bootstrappers",
			"system:nodes",
		}
	}
}

func GetNodeBootstrapUpsert(arn string, osFamily string) *awsauth.MapperArguments {
	return &awsauth.MapperArguments{
		MapRoles:      true,
		RoleARN:       arn,
		Username:      "system:node:{{EC2PrivateDNSName}}",
		Groups:        GetGroupsForOsFamily(osFamily),
		WithRetries:   true,
		MinRetryTime:  time.Millisecond * 100,
		MaxRetryTime:  time.Second * 30,
		MaxRetryCount: 12,
	}
}

func GetNodeBootstrapRemove(arn string, osFamily string) *awsauth.MapperArguments {
	return &awsauth.MapperArguments{
		MapRoles:      true,
		RoleARN:       arn,
		Username:      "system:node:{{EC2PrivateDNSName}}",
		Groups:        GetGroupsForOsFamily(osFamily),
		WithRetries:   true,
		MinRetryTime:  time.Millisecond * 100,
		MaxRetryTime:  time.Second * 30,
		MaxRetryCount: 12,
	}
}

func RemoveAuthConfigMap(kube kubernetes.Interface, arns []string, osFamilies []string) error {
	authMap := awsauth.New(kube, false)
	for index, arn := range arns {
		if arn == "" {
			continue
		}
		err := authMap.Remove(GetNodeBootstrapRemove(arn, osFamilies[index]))
		if err != nil {
			return err
		}
	}
	return nil
}

func UpsertAuthConfigMap(kube kubernetes.Interface, arns []string, osFamilies []string) error {
	authMap := awsauth.New(kube, false)
	for index, arn := range arns {
		if arn == "" {
			continue
		}
		err := authMap.Upsert(GetNodeBootstrapUpsert(arn, osFamilies[index]))
		if err != nil {
			return err
		}
	}
	return nil
}
