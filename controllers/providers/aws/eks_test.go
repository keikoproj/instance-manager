package aws

import (
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/onsi/gomega"
	"testing"
)

func TestClusterDns(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
	)

	awsWorker := AwsWorker{}
	cidr := "172.16.0.0/12"
	ip := awsWorker.GetDNSClusterIP(&eks.Cluster{KubernetesNetworkConfig: &eks.KubernetesNetworkConfigResponse{ServiceIpv4Cidr: &cidr}})
	g.Expect(ip).To(gomega.Equal("172.16.0.10"))

}