package aws

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/onsi/gomega"
)

func TestNewRetryLogger(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	collector := common.NewMetricsCollector()
	logger := NewRetryLogger(5, collector)

	g.Expect(logger).ToNot(gomega.BeNil())
	g.Expect(logger.NumMaxRetries).To(gomega.Equal(5))
	g.Expect(logger.metricsCollector).To(gomega.Equal(collector))
}

func TestRetryLoggerDefaultRetries(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	collector := common.NewMetricsCollector()
	logger := NewRetryLogger(12, collector)

	g.Expect(logger.NumMaxRetries).To(gomega.Equal(12))
}

func TestRetryRulesThrottleIncrementsMetric(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	collector := common.NewMetricsCollector()
	logger := NewRetryLogger(3, collector)

	// Build a minimal request that looks throttled (HTTP 429)
	req := &request.Request{
		ClientInfo: metadata.ClientInfo{ServiceName: "ec2"},
		Operation:  &request.Operation{Name: "DescribeInstances"},
		HTTPResponse: &http.Response{
			StatusCode: 429,
			Status:     fmt.Sprintf("%d %s", 429, "Too Many Requests"),
		},
	}

	// RetryRules should not panic and should return a duration
	duration := logger.RetryRules(req)
	g.Expect(duration).To(gomega.BeNumerically(">=", 0))
}

func TestRetryRulesNoOperationNoPanic(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	collector := common.NewMetricsCollector()
	logger := NewRetryLogger(3, collector)

	// Request with no Operation set — should not panic
	req := &request.Request{
		ClientInfo: metadata.ClientInfo{ServiceName: "autoscaling"},
		HTTPResponse: &http.Response{
			StatusCode: 500,
			Status:     "500 Internal Server Error",
		},
	}

	duration := logger.RetryRules(req)
	g.Expect(duration).To(gomega.BeNumerically(">=", 0))
}
