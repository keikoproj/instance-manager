package aws

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type RetryLogger struct {
	client.DefaultRetryer
	throttleCollector prometheus.Counter
}

var _ request.Retryer = &RetryLogger{}
var DefaultRetryer = client.DefaultRetryer{
	NumMaxRetries:    12,
	MinThrottleDelay: time.Second * 5,
	MaxThrottleDelay: time.Second * 60,
	MinRetryDelay:    time.Second * 1,
	MaxRetryDelay:    time.Second * 5,
}

func NewRetryLogger(maxRetries int, service string) *RetryLogger {
	retryer := DefaultRetryer
	retryer.NumMaxRetries = maxRetries
	throttles := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("aws_api_%v_throttle_total", service),
			Help: "number of aws API calls throttles",
		},
	)
	metrics.Registry.MustRegister(throttles)
	return &RetryLogger{
		DefaultRetryer:    retryer,
		throttleCollector: throttles,
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
		l.throttleCollector.Add(1)
	}

	if r.Error != nil {
		err = fmt.Sprintf("%v", r.Error)
	} else {
		err = fmt.Sprintf("%d %s", r.HTTPResponse.StatusCode, r.HTTPResponse.Status)
	}
	log.V(1).Info("retryable failure", "error", err, "method", method, "backoff", duration)

	return duration
}
