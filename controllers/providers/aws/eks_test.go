package aws

import (
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/onsi/gomega"
	"testing"
)

func TestClusterDns(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	awsWorker := AwsWorker{}
	cidr := "172.16.0.0/12"
	ip := awsWorker.GetDNSClusterIP(&eks.Cluster{KubernetesNetworkConfig: &eks.KubernetesNetworkConfigResponse{ServiceIpv4Cidr: &cidr}})
	g.Expect(ip).To(gomega.Equal("172.16.0.10"))

	// IPv6 cluster: ServiceIpv4Cidr is empty, ServiceIpv6Cidr is set.
	// DNS IP is the service CIDR network address with the last byte as 0x0a.
	cidrV6 := "fd31:8cc0:942b::/108"
	ipV6 := awsWorker.GetDNSClusterIP(&eks.Cluster{KubernetesNetworkConfig: &eks.KubernetesNetworkConfigResponse{ServiceIpv6Cidr: &cidrV6}})
	g.Expect(ipV6).To(gomega.Equal("fd31:8cc0:942b::a"))

	// Malformed/empty service CIDR must not panic; caller omits --dns-cluster-ip.
	empty := ""
	ipEmpty := awsWorker.GetDNSClusterIP(&eks.Cluster{KubernetesNetworkConfig: &eks.KubernetesNetworkConfigResponse{ServiceIpv4Cidr: &empty}})
	g.Expect(ipEmpty).To(gomega.Equal(""))

	// nil network config must not panic.
	g.Expect(awsWorker.GetDNSClusterIP(&eks.Cluster{})).To(gomega.Equal(""))
}
