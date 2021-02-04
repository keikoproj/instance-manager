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
	InstanceGroup *InstanceGroup
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
		instancegroup *InstanceGroup
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
				instancegroup: MockInstanceGroup("eks-fargate", "managed", nil, nil, basicFargateSpec()),
			},
			want: "",
		},
		{
			name: "eks-bogus provisioner",
			args: args{
				instancegroup: MockInstanceGroup("eks-bogus", "managed", nil, nil, nil),
			},
			want: "validation failed, provisioner 'eks-bogus' is invalid",
		},
		{
			name: "eks-fargate with bad strategy",
			args: args{
				instancegroup: MockInstanceGroup("eks-fargate", "rollingUpdate", nil, nil, basicFargateSpec()),
			},
			want: "validation failed, strategy 'rollingUpdate' is invalid for the eks-fargate provisioner",
		},
		{
			name: "eks with empty strings in licenseSpecifications",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:        "my-eks-cluster",
						NodeSecurityGroups:    []string{"sg-123456789"},
						Image:                 "ami-12345",
						InstanceType:          "m5.large",
						KeyPairName:           "thisShouldBeOptional",
						Subnets:               []string{"subnet-1111111", "subnet-222222"},
						LicenseSpecifications: []string{""},
					},
				}, nil, nil),
			},
			want: "validation failed, 'LicenseSpecifications[0]' must be a valid IAM role ARN",
		},
		{
			name: "eks with invalid licenseSpecification",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:        "my-eks-cluster",
						NodeSecurityGroups:    []string{"sg-123456789"},
						Image:                 "ami-12345",
						InstanceType:          "m5.large",
						KeyPairName:           "thisShouldBeOptional",
						Subnets:               []string{"subnet-1111111", "subnet-222222"},
						LicenseSpecifications: []string{"thisShouldBeAnARN"},
					},
				}, nil, nil),
			},
			want: "validation failed, 'LicenseSpecifications[0]' must be a valid IAM role ARN",
		},
		{
			name: "eks with valid Placement",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:     "my-eks-cluster",
						NodeSecurityGroups: []string{"sg-123456789"},
						Image:              "ami-12345",
						InstanceType:       "m5.large",
						KeyPairName:        "thisShouldBeOptional",
						Subnets:            []string{"subnet-1111111", "subnet-222222"},
						Placement: &PlacementSpec{
							AvailabilityZone:     "us-west-2a",
							HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1122334455:group/resourceName",
							Tenancy:              "host",
						},
					},
				}, nil, nil),
			},
			want: "",
		},
		{
			name: "eks with invalid combination of HostResourceGroupArn and Tenancy in Placement",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:     "my-eks-cluster",
						NodeSecurityGroups: []string{"sg-123456789"},
						Image:              "ami-12345",
						InstanceType:       "m5.large",
						KeyPairName:        "thisShouldBeOptional",
						Subnets:            []string{"subnet-1111111", "subnet-222222"},
						Placement: &PlacementSpec{
							HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1122334455:group/resourceName",
							Tenancy:              "default",
						},
					},
				}, nil, nil),
			},
			want: "validation failed, Tenancy must be \"host\" when HostResourceGroupArn is set",
		},
		{
			name: "eks with invalid HostResourceGroupArn in Placement",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:     "my-eks-cluster",
						NodeSecurityGroups: []string{"sg-123456789"},
						Image:              "ami-12345",
						InstanceType:       "m5.large",
						KeyPairName:        "thisShouldBeOptional",
						Subnets:            []string{"subnet-1111111", "subnet-222222"},
						Placement: &PlacementSpec{
							HostResourceGroupArn: "notAnARN",
							Tenancy:              "host",
						},
					},
				}, nil, nil),
			},
			want: "validation failed, HostResourceGroupArn must be a valid dedicated HostResourceGroup ARN",
		},
		{
			name: "eks with invalid Tenancy in Placement",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:     "my-eks-cluster",
						NodeSecurityGroups: []string{"sg-123456789"},
						Image:              "ami-12345",
						InstanceType:       "m5.large",
						KeyPairName:        "thisShouldBeOptional",
						Subnets:            []string{"subnet-1111111", "subnet-222222"},
						Placement: &PlacementSpec{
							Tenancy: "invalid",
						},
					},
				}, nil, nil),
			},
			want: "validation failed, Tenancy must be one of default, dedicated, host",
		},
		{
			name: "eks with gp3 volume validates",
			args: args{
				instancegroup: MockInstanceGroup("eks", "rollingUpdate", &EKSSpec{
					MaxSize: 1,
					MinSize: 1,
					Type:    "LaunchTemplate",
					EKSConfiguration: &EKSConfiguration{
						EksClusterName:     "my-eks-cluster",
						NodeSecurityGroups: []string{"sg-123456789"},
						Image:              "ami-12345",
						InstanceType:       "m5.large",
						KeyPairName:        "thisShouldBeOptional",
						Subnets:            []string{"subnet-1111111", "subnet-222222"},
						Placement: &PlacementSpec{
							AvailabilityZone:     "us-west-2a",
							HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1122334455:group/resourceName",
							Tenancy:              "host",
						},
						Volumes: []NodeVolume{
							{
								Type: "gp3",
								Iops: 230,
							},
						},
					},
				}, nil, nil),
			},
			want: "",
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

func basicFargateSpec() *EKSFargateSpec {
	return &EKSFargateSpec{
		ClusterName:         "",
		PodExecutionRoleArn: "",
		Subnets:             []string{"subnet-1111111", "subnet-222222"},
		Tags: []map[string]string{
			{
				"key":   "a-key",
				"value": "a-value",
			},
		},
	}
}

func MockInstanceGroup(provisioner, strategy string, eksSpec *EKSSpec, eksManagedSpec *EKSManagedSpec, eksFargateSpec *EKSFargateSpec) *InstanceGroup {
	return &InstanceGroup{
		Spec: InstanceGroupSpec{
			Provisioner: provisioner,
			AwsUpgradeStrategy: AwsUpgradeStrategy{
				Type: strategy,
			},
			EKSSpec:        eksSpec,
			EKSManagedSpec: eksManagedSpec,
			EKSFargateSpec: eksFargateSpec,
		},
	}

}
