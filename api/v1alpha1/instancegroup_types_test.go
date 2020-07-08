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
package v1alpha1

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
)

type EksUnitTest struct {
	InstanceGroup InstanceGroup
}

func (u *EksUnitTest) Run(t *testing.T) string {
	err := u.InstanceGroup.Validate()
	if err == nil {
		return aws.StringValue(nil)
	} else {
		return err.Error()
	}
}

func TestInstanceGroupSpecValidate(t *testing.T) {
	type args struct {
		instancegroup InstanceGroup
	}
	testFunction := func(t *testing.T, args args) string {
		testCase := EksUnitTest{
			InstanceGroup: args.instancegroup,
		}
		return testCase.Run(t)
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "eks-fargate with managed strategy",
			args: args{
				instancegroup: MockInstanceGroup("eks-fargate", "managed"),
			},
			want: "",
		},
		{
			name: "eks-bogus provisioner",
			args: args{
				instancegroup: MockInstanceGroup("eks-bogus", "managed"),
			},
			want: "validation failed, provisioner 'eks-bogus' is invalid",
		},
		{
			name: "eks-fargate with bad strategy",
			args: args{
				instancegroup: MockInstanceGroup("eks-fargate", "rollingUpdate"),
			},
			want: "validation failed, strategy 'rollingUpdate' is invalid for the eks-fargate provisioner",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testFunction(t, tt.args)
			if got != tt.want {
				t.Errorf("%v: got %v, want %v", tt.name, got, tt.want)
			}
		})

	}
}

func MockInstanceGroup(provisioner, strategy string) InstanceGroup {
	ig := InstanceGroup{
		Spec: InstanceGroupSpec{
			Provisioner: provisioner,
			AwsUpgradeStrategy: AwsUpgradeStrategy{
				Type: strategy,
			},
			EKSFargateSpec: &EKSFargateSpec{
				ClusterName:         "",
				PodExecutionRoleArn: "",
				Subnets:             []string{"subnet-1111111", "subnet-222222"},
				Tags: []map[string]string{
					{
						"key":   "a-key",
						"value": "a-value",
					},
				},
			},
		},
	}

	return ig
}
