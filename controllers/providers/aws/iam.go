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

package aws

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
)

// GetAwsIAMClient returns an IAM client
func GetAwsIamClient(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) iamiface.IAMAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries, collector))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("iam", "GetInstanceProfile", GetInstanceProfileTTL)
	cacheCfg.SetCacheTTL("iam", "GetRole", GetRoleTTL)
	cacheCfg.SetCacheTTL("iam", "ListAttachedRolePolicies", ListAttachedRolePoliciesTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return iam.New(sess, config)
}

func (w *AwsWorker) InstanceProfileExist(name string) (*iam.InstanceProfile, bool) {
	var (
		instanceProfile *iam.InstanceProfile
		input           = &iam.GetInstanceProfileInput{
			InstanceProfileName: aws.String(name),
		}
	)

	out, err := w.IamClient.GetInstanceProfile(input)
	if err != nil {
		return instanceProfile, false
	}
	return out.InstanceProfile, true
}

func (w *AwsWorker) RoleExist(name string) (*iam.Role, bool) {
	out, err := w.GetRole(name)
	if err != nil {
		var role *iam.Role
		return role, false
	}
	return out, true
}

func (w *AwsWorker) DeleteScalingGroupRole(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return err
		}
	}

	_, err := w.IamClient.RemoveRoleFromInstanceProfile(&iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(name),
		RoleName:            aws.String(name),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	_, err = w.IamClient.DeleteInstanceProfile(&iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != iam.ErrCodeNoSuchEntityException {
				return err
			}
		}
	}

	// must wait until all policies are detached
	err = w.WithRetries(func() bool {
		_, err := w.IamClient.DeleteRole(&iam.DeleteRoleInput{
			RoleName: aws.String(name),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() != iam.ErrCodeNoSuchEntityException {
					log.Error(err, "failed to delete role")
					return false
				}
			}
		}
		return true
	})
	if err != nil {
		return errors.Wrap(err, "role deletion failed")
	}

	return nil
}

func (w *AwsWorker) AttachManagedPolicies(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return errors.Wrap(err, "failed to attach role policies")
		}
	}
	return nil
}

func (w *AwsWorker) DetachManagedPolicies(name string, managedPolicies []string) error {
	for _, policy := range managedPolicies {
		_, err := w.IamClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
			RoleName:  aws.String(name),
			PolicyArn: aws.String(policy),
		})
		if err != nil {
			return errors.Wrap(err, "failed to detach role policies")
		}
	}
	return nil
}

func (w *AwsWorker) ListRolePolicies(name string) ([]*iam.AttachedPolicy, error) {
	policies := []*iam.AttachedPolicy{}
	err := w.IamClient.ListAttachedRolePoliciesPages(
		&iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(name),
		},
		func(page *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
			policies = append(policies, page.AttachedPolicies...)
			return page.Marker != nil
		})
	if err != nil {
		return policies, err
	}
	return policies, nil
}

func (w *AwsWorker) CreateScalingGroupRole(name string) (*iam.Role, *iam.InstanceProfile, error) {
	var (
		assumeRolePolicyDocument = `{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}]
		}`
		createdRole    = &iam.Role{}
		createdProfile = &iam.InstanceProfile{}
	)
	if role, ok := w.RoleExist(name); !ok {
		out, err := w.IamClient.CreateRole(&iam.CreateRoleInput{
			RoleName:                 aws.String(name),
			AssumeRolePolicyDocument: aws.String(assumeRolePolicyDocument),
		})
		if err != nil {
			return createdRole, createdProfile, errors.Wrap(err, "failed to create role")
		}
		createdRole = out.Role
	} else {
		createdRole = role
	}

	if instanceProfile, ok := w.InstanceProfileExist(name); !ok {
		out, err := w.IamClient.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
			InstanceProfileName: aws.String(name),
		})
		if err != nil {
			return createdRole, createdProfile, errors.Wrap(err, "failed to create instance-profile")
		}
		createdProfile = out.InstanceProfile
		time.Sleep(DefaultInstanceProfilePropagationDelay)

	} else {
		createdProfile = instanceProfile
	}

	if createdProfile != nil && len(createdProfile.Roles) == 0 {
		_, err := w.IamClient.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: aws.String(name),
			RoleName:            aws.String(name),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() != iam.ErrCodeLimitExceededException {
					return createdRole, createdProfile, errors.Wrap(err, "failed to attach instance-profile")
				}
			}
		} else {
			createdProfile.Roles = append(createdProfile.Roles, createdRole)
		}
	}

	return createdRole, createdProfile, nil
}

func (w *AwsWorker) DetachDefaultPolicyFromDefaultRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	rolePolicy := &iam.DetachRolePolicyInput{
		PolicyArn: aws.String(defaultPolicyArn),
		RoleName:  aws.String(roleName),
	}
	_, err := w.IamClient.DetachRolePolicy(rolePolicy)
	return err
}

func (w *AwsWorker) DeleteDefaultFargateRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	role := &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}
	_, err := w.IamClient.DeleteRole(role)
	return err
}

func (w *AwsWorker) GetDefaultFargateRole() (*iam.Role, error) {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	return w.GetRole(roleName)
}
func (w *AwsWorker) GetRole(roleName string) (*iam.Role, error) {
	role := &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}
	resp, err := w.IamClient.GetRole(role)
	if err != nil {
		return nil, err
	}

	return resp.Role, nil
}
func (w *AwsWorker) CreateDefaultFargateRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	var template = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks-fargate-pods.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	role := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: &template,
		Path:                     aws.String("/"),
		RoleName:                 aws.String(roleName),
	}
	_, err := w.IamClient.CreateRole(role)
	return err
}

func (w *AwsWorker) AttachDefaultPolicyToDefaultRole() error {
	var roleName = w.Parameters["DefaultRoleName"].(string)
	rolePolicy := &iam.AttachRolePolicyInput{
		PolicyArn: aws.String(defaultPolicyArn),
		RoleName:  aws.String(roleName),
	}
	_, err := w.IamClient.AttachRolePolicy(rolePolicy)
	if err == nil {
		time.Sleep(DefaultInstanceProfilePropagationDelay)
	}
	return err
}
