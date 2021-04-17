package aws

import (
	"fmt"
	"time"

	"github.com/keikoproj/instance-manager/controllers/common"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
)

type RetryLogger struct {
	client.DefaultRetryer
	metricsCollector *common.MetricsCollector
}

var _ request.Retryer = &RetryLogger{}
var DefaultRetryer = client.DefaultRetryer{
	NumMaxRetries:    12,
	MinThrottleDelay: time.Second * 5,
	MaxThrottleDelay: time.Second * 60,
	MinRetryDelay:    time.Second * 1,
	MaxRetryDelay:    time.Second * 5,
}

func NewRetryLogger(maxRetries int, metrics *common.MetricsCollector) *RetryLogger {
	retryer := DefaultRetryer
	retryer.NumMaxRetries = maxRetries
	return &RetryLogger{
		DefaultRetryer:   retryer,
		metricsCollector: metrics,
	}
}

func (l RetryLogger) RetryRules(r *request.Request) time.Duration {
	var (
		duration = l.DefaultRetryer.RetryRules(r)
		service  = r.ClientInfo.ServiceName
		name     string
		err      string
	)

	if r.Operation != nil {
		name = r.Operation.Name
	}
	method := fmt.Sprintf("%v/%v", service, name)

	if r.IsErrorThrottle() {
		l.metricsCollector.IncThrottle(service, name)
	}

	if r.Error != nil {
		err = fmt.Sprintf("%v", r.Error)
	} else {
		err = fmt.Sprintf("%d %s", r.HTTPResponse.StatusCode, r.HTTPResponse.Status)
	}
	log.V(1).Info("retryable failure", "error", err, "method", method, "backoff", duration)

	return duration
}
