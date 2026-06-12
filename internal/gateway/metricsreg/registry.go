// Package metricsreg は topic 単位の 5 系列のメトリクスをメモリ上で集計し、
// Prometheus 形式で公開する (LR-302, SP-005)。値は永続化されない: 再起動で
// リセットされ、履歴は外部の監視スタックが保持する。メトリクス名は外部契約
// (data-visualization.md) なので後方互換を維持すること。
package metricsreg

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry は topic 単位の 5 系列のメトリクスを保持する (ラベルは topic のみ)。
type Registry struct {
	registry        *prometheus.Registry
	lastCollect     *prometheus.GaugeVec
	processed       *prometheus.CounterVec
	deliveryFailure *prometheus.CounterVec
	dlqCount        *prometheus.GaugeVec
	backlogCount    *prometheus.GaugeVec
}

// New は 5 系列のメトリクスを登録済みのレジストリを生成する。
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

// SetLastCollected は topic の最終収集成功時刻を記録する。
func (r *Registry) SetLastCollected(topic string, t time.Time) {
	r.lastCollect.WithLabelValues(topic).Set(float64(t.Unix()))
}

// IncProcessed は topic の処理済みメッセージ数をインクリメントする。
func (r *Registry) IncProcessed(topic string) {
	r.processed.WithLabelValues(topic).Inc()
}

// IncDeliveryFailure は topic の配信失敗数をインクリメントする。
func (r *Registry) IncDeliveryFailure(topic string) {
	r.deliveryFailure.WithLabelValues(topic).Inc()
}

// SetDLQCount は topic の現在の DLQ メッセージ数を設定する。
func (r *Registry) SetDLQCount(topic string, n int) {
	r.dlqCount.WithLabelValues(topic).Set(float64(n))
}

// SetBacklogCount は topic の現在の未配信 (backlog) 数を設定する。
func (r *Registry) SetBacklogCount(topic string, n int) {
	r.backlogCount.WithLabelValues(topic).Set(float64(n))
}

// Handler は Prometheus テキスト形式を返す /metrics ハンドラを返す。
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}
