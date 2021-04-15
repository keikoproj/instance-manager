package common

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "instance_manager"
)

type MetricsCollector struct {
	prometheus.Collector

	successCounter   *prometheus.CounterVec
	failureCounter   *prometheus.CounterVec
	throttleCounter  *prometheus.CounterVec
	statusGauge      *prometheus.GaugeVec
	lastUpgradeGauge *prometheus.GaugeVec
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
		lastUpgradeGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "instance_group_last_upgrade_seconds",
				Help:      "number of seconds since last upgrade completed",
			},
			[]string{"instancegroup"},
		),
	}
}

func (c MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.successCounter.Collect(ch)
	c.failureCounter.Collect(ch)
	c.throttleCounter.Collect(ch)
	c.statusGauge.Collect(ch)
	c.lastUpgradeGauge.Collect(ch)
}

func (c MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	c.successCounter.Describe(ch)
	c.failureCounter.Describe(ch)
	c.throttleCounter.Describe(ch)
	c.statusGauge.Describe(ch)
	c.lastUpgradeGauge.Describe(ch)
}

func (c *MetricsCollector) SetLastUpgradeSeconds(instanceGroup string, t float64) {
	c.lastUpgradeGauge.With(prometheus.Labels{"instancegroup": instanceGroup}).Set(t)
}

func (c *MetricsCollector) SetInstanceGroup(instanceGroup, state string) {
	externalStates := []string{"ReconcileModifying", "InitUpgrade", "Deleting", "Ready", "Error"}
	if !ContainsEqualFold(externalStates, state) {
		return
	}
	for _, s := range externalStates {
		c.statusGauge.With(prometheus.Labels{"instancegroup": instanceGroup, "status": s}).Set(0)
	}
	c.statusGauge.With(prometheus.Labels{"instancegroup": instanceGroup, "status": state}).Set(1)
}

func (c *MetricsCollector) UnsetInstanceGroup() {
	c.successCounter.Reset()
	c.failureCounter.Reset()
	c.throttleCounter.Reset()
	c.lastUpgradeGauge.Reset()
	c.statusGauge.Reset()
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
