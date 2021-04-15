package common

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "instance_manager"
)

type MetricsCollector struct {
	prometheus.Collector

	successCounter  *prometheus.CounterVec
	failureCounter  *prometheus.CounterVec
	throttleCounter *prometheus.CounterVec
	statusGauge     *prometheus.GaugeVec
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		successCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "reconcile_success_total",
				Help:      `total successful reconciles`,
			},
			[]string{"instancegroup"},
		),
		failureCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "reconcile_fail_total",
				Help:      `total failed reconciles`,
			},
			[]string{"instancegroup", "reason"},
		),
		throttleCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "aws_api_throttle_total",
				Help:      "number of aws API calls throttles",
			},
			[]string{"service", "operation"},
		),
		statusGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "instance_group_status",
				Help:      "number of instance groups and their status",
			},
			[]string{"instancegroup", "status"},
		),
	}
}

func (c MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.successCounter.Collect(ch)
	c.failureCounter.Collect(ch)
	c.throttleCounter.Collect(ch)
	c.statusGauge.Collect(ch)
}

func (c MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	c.successCounter.Describe(ch)
	c.failureCounter.Describe(ch)
	c.throttleCounter.Describe(ch)
	c.statusGauge.Describe(ch)
}

func (c *MetricsCollector) SetInstanceGroup(instanceGroup, oldState, newState string) {
	fmt.Printf("%v -> %v\n", oldState, newState)
	c.statusGauge.Delete(prometheus.Labels{"instancegroup": instanceGroup, "status": oldState})
	c.statusGauge.With(prometheus.Labels{"instancegroup": instanceGroup, "status": newState}).Set(1)
}

func (c *MetricsCollector) UnsetInstanceGroup(instanceGroup, status string) {
	c.statusGauge.With(prometheus.Labels{"instancegroup": instanceGroup, "status": status}).Set(0)
}

func (c *MetricsCollector) IncSuccess(instanceGroup string) {
	c.successCounter.With(prometheus.Labels{"instancegroup": instanceGroup}).Inc()
}

func (c *MetricsCollector) IncFail(instanceGroup, reason string) {
	c.successCounter.With(prometheus.Labels{"instancegroup": instanceGroup}).Inc()
}

func (c *MetricsCollector) IncThrottle(serviceName, operationName string) {
	c.throttleCounter.With(prometheus.Labels{"service": serviceName, "operation": operationName}).Inc()
}
