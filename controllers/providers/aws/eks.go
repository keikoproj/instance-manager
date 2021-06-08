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
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
)

// GetAwsEksClient returns an EKS client
func GetAwsEksClient(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) eksiface.EKSAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries, collector))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("eks", "DescribeCluster", DescribeClusterTTL)
	cacheCfg.SetCacheTTL("eks", "DescribeNodegroup", DescribeNodegroupTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return eks.New(sess, config)
}

// TODO: Move logic to provisioner
func (w *AwsWorker) IsNodeGroupExist() bool {
	input := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	_, err := w.EksClient.DescribeNodegroup(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == eks.ErrCodeResourceNotFoundException {
				return false
			}
		}
		log.Error(err, "failed to describe nodegroup")
		return false
	}

	return true
}

func (w *AwsWorker) DescribeEKSCluster(clusterName string) (*eks.Cluster, error) {
	cluster := &eks.Cluster{}
	input := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	output, err := w.EksClient.DescribeCluster(input)
	if err != nil {
		return cluster, err
	}
	return output.Cluster, nil
}

// TODO: Rename - GetNodeGroup
func (w *AwsWorker) GetSelfNodeGroup() (error, *eks.Nodegroup) {
	input := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	output, err := w.EksClient.DescribeNodegroup(input)
	if err != nil {
		return err, &eks.Nodegroup{}
	}
	return nil, output.Nodegroup
}

func (w *AwsWorker) DeleteManagedNodeGroup() error {
	input := &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(w.Parameters["ClusterName"].(string)),
		NodegroupName: aws.String(w.Parameters["NodegroupName"].(string)),
	}
	_, err := w.EksClient.DeleteNodegroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) UpdateManagedNodeGroup(nodeGroup *eks.Nodegroup, desired int64, nodeLabels map[string]string) error {
	input := &eks.UpdateNodegroupConfigInput{}

	if labels, ok := w.GetLabelsUpdatePayload(aws.StringValueMap(nodeGroup.Labels), nodeLabels); ok {
		input.Labels = labels
	}

	input.ClusterName = aws.String(w.Parameters["ClusterName"].(string))
	input.NodegroupName = aws.String(w.Parameters["NodegroupName"].(string))
	input.ScalingConfig = &eks.NodegroupScalingConfig{
		MaxSize:     aws.Int64(w.Parameters["MaxSize"].(int64)),
		MinSize:     aws.Int64(w.Parameters["MinSize"].(int64)),
		DesiredSize: aws.Int64(desired),
	}

	_, err := w.EksClient.UpdateNodegroupConfig(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) CreateManagedNodeGroup() error {
	input := &eks.CreateNodegroupInput{
		AmiType:        aws.String(w.Parameters["AmiType"].(string)),
		ClusterName:    aws.String(w.Parameters["ClusterName"].(string)),
		DiskSize:       aws.Int64(w.Parameters["DiskSize"].(int64)),
		InstanceTypes:  aws.StringSlice(w.Parameters["InstanceTypes"].([]string)),
		Labels:         aws.StringMap(w.Parameters["Labels"].(map[string]string)),
		NodeRole:       aws.String(w.Parameters["NodeRole"].(string)),
		NodegroupName:  aws.String(w.Parameters["NodegroupName"].(string)),
		ReleaseVersion: aws.String(w.Parameters["ReleaseVersion"].(string)),
		RemoteAccess: &eks.RemoteAccessConfig{
			Ec2SshKey:            aws.String(w.Parameters["Ec2SshKey"].(string)),
			SourceSecurityGroups: aws.StringSlice(w.Parameters["SourceSecurityGroups"].([]string)),
		},
		ScalingConfig: &eks.NodegroupScalingConfig{
			MaxSize:     aws.Int64(w.Parameters["MaxSize"].(int64)),
			MinSize:     aws.Int64(w.Parameters["MinSize"].(int64)),
			DesiredSize: aws.Int64(w.Parameters["MinSize"].(int64)),
		},
		Subnets: aws.StringSlice(w.Parameters["Subnets"].([]string)),
		Tags:    aws.StringMap(w.compactTags(w.Parameters["Tags"].([]map[string]string))),
		Version: aws.String(w.Parameters["Version"].(string)),
	}

	_, err := w.EksClient.CreateNodegroup(input)
	if err != nil {
		return err
	}
	return nil
}

func (w *AwsWorker) DeriveEksVpcID(clusterName string) (string, error) {
	out, err := w.EksClient.DescribeCluster(&eks.DescribeClusterInput{Name: aws.String(clusterName)})
	if err != nil {
		return "", err
	}
	return aws.StringValue(out.Cluster.ResourcesVpcConfig.VpcId), nil
}

func (w *AwsWorker) CreateFargateProfile(arn string) error {
	tags := w.Parameters["Tags"].(map[string]*string)
	if len(tags) == 0 {
		tags = nil
	}
	selectors := w.Parameters["Selectors"].([]*eks.FargateProfileSelector)
	if len(selectors) == 0 {
		selectors = nil
	}

	fargateInput := &eks.CreateFargateProfileInput{
		ClusterName:         aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName:  aws.String(w.Parameters["ProfileName"].(string)),
		PodExecutionRoleArn: aws.String(arn),
		Selectors:           selectors,
		Subnets:             aws.StringSlice(w.Parameters["Subnets"].([]string)),
		Tags:                tags,
	}

	_, err := w.EksClient.CreateFargateProfile(fargateInput)
	return err
}

func (w *AwsWorker) DeleteFargateProfile() error {
	deleteInput := &eks.DeleteFargateProfileInput{
		ClusterName:        aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName: aws.String(w.Parameters["ProfileName"].(string)),
	}
	_, err := w.EksClient.DeleteFargateProfile(deleteInput)
	return err
}

func (w *AwsWorker) DescribeFargateProfile() (*eks.FargateProfile, error) {
	describeInput := &eks.DescribeFargateProfileInput{
		ClusterName:        aws.String(w.Parameters["ClusterName"].(string)),
		FargateProfileName: aws.String(w.Parameters["ProfileName"].(string)),
	}
	output, err := w.EksClient.DescribeFargateProfile(describeInput)
	if err != nil {
		return nil, err
	}
	return output.FargateProfile, nil
}

func (w *AwsWorker) GetDNSClusterIP(cluster *eks.Cluster) string {
	if cluster != nil {
		serviceCidr := aws.StringValue(cluster.KubernetesNetworkConfig.ServiceIpv4Cidr)
		// addresses assigned from either the 10.100.0.0/16 or 172.20.0.0/16 CIDR blocks
		return strings.ReplaceAll(serviceCidr, "0/16", "10")
	} else {
		// if cluster information is not available get instance's ipv4 cidr from metadata
		macAddress, err := w.Ec2Metadata.GetMetadata(MetadataMACAddressPath)
		if err != nil {
			return ""
		}

		cidrMetadataPath := fmt.Sprintf(MetadataInterfaceCidrPathFmt, macAddress)
		ipv4Cidr, err := w.Ec2Metadata.GetMetadata(cidrMetadataPath)
		if err != nil {
			return ""
		}

		// if instance ipv4 cidr starts with 10. service IP is 172.20.0.10, otherwise its 10.100.0.10
		if strings.HasPrefix(ipv4Cidr, "10.") {
			return "172.20.0.10"
		}

		return "10.100.0.10"
	}
}
