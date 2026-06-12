// Package metricsreg aggregates the five per-topic metrics in memory and
// exposes them in Prometheus format (LR-302, SP-005). Values are not
// persisted: a restart resets them and the external monitoring stack keeps
// history. Metric names are an external contract (data-visualization.md):
// keep them backward compatible.
package metricsreg

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds the five per-topic metric series (label: topic only).
type Registry struct {
	registry        *prometheus.Registry
	lastCollect     *prometheus.GaugeVec
	processed       *prometheus.CounterVec
	deliveryFailure *prometheus.CounterVec
	dlqCount        *prometheus.GaugeVec
	backlogCount    *prometheus.GaugeVec
}

// New builds the registry with the five metric series registered.
func New() *Registry {
	r := &Registry{registry: prometheus.NewRegistry()}
	r.lastCollect = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "file_pubsub_last_collect_timestamp_seconds",
		Help: "Unix time of the last successful collect per topic.",
	}, []string{"topic"})
	r.processed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "file_pubsub_processed_total",
		Help: "Number of processed messages per topic.",
	}, []string{"topic"})
	r.deliveryFailure = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "file_pubsub_delivery_failure_total",
		Help: "Number of failed subscription deliveries per topic.",
	}, []string{"topic"})
	r.dlqCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "file_pubsub_dlq_count",
		Help: "Number of messages isolated in the DLQ per topic.",
	}, []string{"topic"})
	r.backlogCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "file_pubsub_backlog_count",
		Help: "Number of undelivered (backlog) messages per topic.",
	}, []string{"topic"})
	r.registry.MustRegister(r.lastCollect, r.processed, r.deliveryFailure, r.dlqCount, r.backlogCount)
	return r
}

// SetLastCollected records the last successful collect time of a topic.
func (r *Registry) SetLastCollected(topic string, t time.Time) {
	r.lastCollect.WithLabelValues(topic).Set(float64(t.Unix()))
}

// IncProcessed increments the processed message count of a topic.
func (r *Registry) IncProcessed(topic string) {
	r.processed.WithLabelValues(topic).Inc()
}

// IncDeliveryFailure increments the delivery failure count of a topic.
func (r *Registry) IncDeliveryFailure(topic string) {
	r.deliveryFailure.WithLabelValues(topic).Inc()
}

// SetDLQCount sets the current DLQ message count of a topic.
func (r *Registry) SetDLQCount(topic string, n int) {
	r.dlqCount.WithLabelValues(topic).Set(float64(n))
}

// SetBacklogCount sets the current backlog (undelivered) count of a topic.
func (r *Registry) SetBacklogCount(topic string, n int) {
	r.backlogCount.WithLabelValues(topic).Set(float64(n))
}

// Handler returns the /metrics handler serving Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}
