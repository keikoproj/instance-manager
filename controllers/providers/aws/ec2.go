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
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
)

// GetAwsEc2Client returns an EC2 client
func GetAwsEc2Client(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) ec2iface.EC2API {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries, collector))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}

	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("ec2", "DescribeSecurityGroups", DescribeSecurityGroupsTTL)
	cacheCfg.SetCacheTTL("ec2", "DescribeSubnets", DescribeSubnetsTTL)
	cacheCfg.SetCacheTTL("ec2", "DescribeInstanceTypes", DescribeInstanceTypesTTL)
	cacheCfg.SetExcludeFlushing("ec2", "DescribeInstanceTypes", true)
	cacheCfg.SetCacheTTL("ec2", "DescribeInstanceTypeOfferings", DescribeInstanceTypeOfferingTTL)
	cacheCfg.SetExcludeFlushing("ec2", "DescribeInstanceTypeOfferings", true)
	cacheCfg.SetCacheTTL("ec2", "DescribeLaunchTemplates", DescribeLaunchTemplatesTTL)
	cacheCfg.SetCacheTTL("ec2", "DescribeLaunchTemplateVersions", DescribeLaunchTemplateVersionsTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return ec2.New(sess)
}

func (w *AwsWorker) DescribeInstanceOfferings() ([]*ec2.InstanceTypeOffering, error) {
	offerings := []*ec2.InstanceTypeOffering{}
	err := w.Ec2Client.DescribeInstanceTypeOfferingsPages(&ec2.DescribeInstanceTypeOfferingsInput{}, func(page *ec2.DescribeInstanceTypeOfferingsOutput, lastPage bool) bool {
		offerings = append(offerings, page.InstanceTypeOfferings...)
		return page.NextToken != nil
	})
	if err != nil {
		return offerings, err
	}
	return offerings, nil
}

func (w *AwsWorker) DescribeInstanceTypes() ([]*ec2.InstanceTypeInfo, error) {
	types := []*ec2.InstanceTypeInfo{}
	err := w.Ec2Client.DescribeInstanceTypesPages(&ec2.DescribeInstanceTypesInput{}, func(page *ec2.DescribeInstanceTypesOutput, lastPage bool) bool {
		types = append(types, page.InstanceTypes...)
		return page.NextToken != nil
	})
	if err != nil {
		return types, err
	}
	return types, nil
}

func (w *AwsWorker) DescribeLaunchTemplates() ([]*ec2.LaunchTemplate, error) {
	launchTemplates := []*ec2.LaunchTemplate{}
	err := w.Ec2Client.DescribeLaunchTemplatesPages(&ec2.DescribeLaunchTemplatesInput{}, func(page *ec2.DescribeLaunchTemplatesOutput, lastPage bool) bool {
		launchTemplates = append(launchTemplates, page.LaunchTemplates...)
		return page.NextToken != nil
	})
	if err != nil {
		return launchTemplates, err
	}
	return launchTemplates, nil
}

func (w *AwsWorker) DescribeLaunchTemplateVersions(templateName string) ([]*ec2.LaunchTemplateVersion, error) {
	versions := []*ec2.LaunchTemplateVersion{}
	err := w.Ec2Client.DescribeLaunchTemplateVersionsPages(&ec2.DescribeLaunchTemplateVersionsInput{LaunchTemplateName: aws.String(templateName)}, func(page *ec2.DescribeLaunchTemplateVersionsOutput, lastPage bool) bool {
		versions = append(versions, page.LaunchTemplateVersions...)
		return page.NextToken != nil
	})
	if err != nil {
		return versions, err
	}
	return versions, nil
}

func (w *AwsWorker) CreateLaunchTemplate(input *ec2.CreateLaunchTemplateInput) error {
	_, err := w.Ec2Client.CreateLaunchTemplate(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) UpdateLaunchTemplateDefaultVersion(name, defaultVersion string) (*ec2.LaunchTemplate, error) {
	out, err := w.Ec2Client.ModifyLaunchTemplate(&ec2.ModifyLaunchTemplateInput{
		LaunchTemplateName: aws.String(name),
		DefaultVersion:     aws.String(defaultVersion),
	})
	if err != nil {
		return &ec2.LaunchTemplate{}, err
	}
	return out.LaunchTemplate, nil
}

func (w *AwsWorker) CreateLaunchTemplateVersion(input *ec2.CreateLaunchTemplateVersionInput) (*ec2.LaunchTemplateVersion, error) {
	v, err := w.Ec2Client.CreateLaunchTemplateVersion(input)
	if err != nil {
		return nil, err
	}
	return v.LaunchTemplateVersion, nil
}

func (w *AwsWorker) DeleteLaunchTemplate(name string) error {
	_, err := w.Ec2Client.DeleteLaunchTemplate(&ec2.DeleteLaunchTemplateInput{
		LaunchTemplateName: aws.String(name),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DeleteLaunchTemplateVersions(name string, versions []string) error {
	_, err := w.Ec2Client.DeleteLaunchTemplateVersions(&ec2.DeleteLaunchTemplateVersionsInput{
		LaunchTemplateName: aws.String(name),
		Versions:           aws.StringSlice(versions),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) SubnetByName(name, vpc string) (*ec2.Subnet, error) {
	subnets := []*ec2.Subnet{}
	filteredSubnets := []*ec2.Subnet{}

	err := w.Ec2Client.DescribeSubnetsPages(
		&ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{aws.String(vpc)},
				},
			},
		},
		func(page *ec2.DescribeSubnetsOutput, lastPage bool) bool {
			for _, p := range page.Subnets {
				subnets = append(subnets, p)
			}
			return page.NextToken != nil
		},
	)
	if err != nil {
		return nil, err
	}

	for _, s := range subnets {
		for _, tag := range s.Tags {
			k := aws.StringValue(tag.Key)
			v := aws.StringValue(tag.Value)
			if strings.EqualFold(k, "Name") && strings.EqualFold(v, name) {
				filteredSubnets = append(filteredSubnets, s)
			}
		}
	}

	if len(filteredSubnets) == 0 {
		return nil, nil
	}
	return filteredSubnets[0], nil
}

func (w *AwsWorker) SecurityGroupByName(name, vpc string) (*ec2.SecurityGroup, error) {
	groups := []*ec2.SecurityGroup{}
	filteredGroups := []*ec2.SecurityGroup{}
	err := w.Ec2Client.DescribeSecurityGroupsPages(
		&ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{aws.String(vpc)},
				},
			},
		},
		func(page *ec2.DescribeSecurityGroupsOutput, lastPage bool) bool {
			for _, p := range page.SecurityGroups {
				groups = append(groups, p)
			}
			return page.NextToken != nil
		},
	)
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		for _, tag := range g.Tags {
			k := aws.StringValue(tag.Key)
			v := aws.StringValue(tag.Value)
			if strings.EqualFold(k, "Name") && strings.EqualFold(v, name) {
				filteredGroups = append(filteredGroups, g)
			}
		}
	}
	if len(filteredGroups) == 0 {
		return nil, nil
	}
	return filteredGroups[0], nil
}
