package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/instance-manager/controllers/common"
)

type architectureMap map[string]string

const (
	EksOptimisedAmiPath           = "/aws/service/eks/optimized-ami/%s/amazon-linux-2/recommended/image_id"
	EksOptimisedAmazonLinux2Arm64 = "/aws/service/eks/optimized-ami/%s/amazon-linux-2-arm64/recommended/image_id"
	EksOptimisedBottlerocket      = "/aws/service/bottlerocket/aws-k8s-%s/x86_64/latest/image_id"
	EksOptimisedBottlerocketArm64 = "/aws/service/bottlerocket/aws-k8s-%s/arm64/latest/image_id"
	EksOptimisedWindowsCore       = "/aws/service/ami-windows-latest/Windows_Server-2019-English-Core-EKS_Optimized-%s/image_id"
	EksOptimisedWindowsFull       = "/aws/service/ami-windows-latest/Windows_Server-2019-English-Full-EKS_Optimized-%s/image_id"
)

var (
	EksAmis = map[string]architectureMap{
		"amazonlinux2": architectureMap{
			"x86_64": EksOptimisedAmiPath,
			"arm64":  EksOptimisedAmazonLinux2Arm64,
		},
		"bottlerocket": architectureMap{
			"x86_64": EksOptimisedBottlerocket,
			"arm64":  EksOptimisedBottlerocketArm64,
		},
		"windows": architectureMap{
			"x86_64": EksOptimisedWindowsCore,
		},
	}
)

func GetAwsSsmClient(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) ssmiface.SSMAPI {
	config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, NewRetryLogger(maxRetries, collector))
	sess, err := session.NewSession(config)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("ssm", "GetParameter", GetParameterTTL)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	return ssm.New(sess)
}

func (w *AwsWorker) GetEksLatestAmi(OSFamily string, arch string, kubernetesVersion string) (string, error) {
	input := &ssm.GetParameterInput{
		Name: aws.String(fmt.Sprintf(EksAmis[OSFamily][arch], kubernetesVersion)),
	}

	output, err := w.SsmClient.GetParameter(input)
	if err != nil {
		return "", err
	}
	return aws.StringValue(output.Parameter.Value), nil
}
