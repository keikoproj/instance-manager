package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
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

	DefaultSess *session.Session
	Config      *aws.Config
)

func GetDefaultAwsSsmClient(region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) ssmiface.SSMAPI {
	return GetAwsSsmClient("", region, cacheCfg, maxRetries, collector)
}

func GetAwsSsmClient(role, region string, cacheCfg *cache.Config, maxRetries int, collector *common.MetricsCollector) ssmiface.SSMAPI {
	Config := aws.NewConfig().WithRegion(region).WithCredentialsChainVerboseErrors(true)
	Config = request.WithRetryer(Config, NewRetryLogger(maxRetries, collector))
	DefaultSess, err := session.NewSession(Config)
	log.Info("Setting default session", "DefaultSess", DefaultSess)
	if err != nil {
		panic(err)
	}
	cache.AddCaching(DefaultSess, cacheCfg)
	cacheCfg.SetCacheTTL("ssm", "GetParameter", GetParameterTTL)
	DefaultSess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.V(1).Info("AWS API call",
			"cacheHit", cache.IsCacheHit(ctx),
			"service", r.ClientInfo.ServiceName,
			"operation", r.Operation.Name,
		)
	})
	if !strings.EqualFold(role, "") {
		creds := stscreds.NewCredentials(DefaultSess, role)
		return ssm.New(DefaultSess, &aws.Config{Credentials: creds})
	}
	return ssm.New(DefaultSess)
}

func (w *AwsWorker) GetCustomAmi(role, path string) (string, error) {
	log.Info("Inside GetCustomAmi")
	log.Info("Getting default session", "DefaultSess", DefaultSess)
	var ssmClient ssmiface.SSMAPI
	input := &ssm.GetParameterInput{
		Name: aws.String(path),
	}
	log.Info("Checking role")
	DefaultSess, _ := session.NewSession(Config)

	if !strings.EqualFold(role, "") {
		log.Info("Assuming Role", "role", role, "Session", DefaultSess)
		creds := stscreds.NewCredentials(DefaultSess, role)
		ssmClient = ssm.New(DefaultSess, &aws.Config{Credentials: creds})
	} else {
		log.Info("Using default client")
		ssmClient = w.SsmClient
	}

	output, err := ssmClient.GetParameter(input)
	if err != nil {
		return "", err
	}
	return aws.StringValue(output.Parameter.Value), nil
}

func (w *AwsWorker) GetEksLatestAmi(role, OSFamily, arch, kubernetesVersion string) (string, error) {
	path := fmt.Sprintf(EksAmis[OSFamily][arch], kubernetesVersion)
	return w.GetCustomAmi(role, path)
}
