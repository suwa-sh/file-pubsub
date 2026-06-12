package metricsreg

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scrape は /metrics をリクエストしてレスポンス本文を返すヘルパー。
func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/metrics status = %d", rec.Code)
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRegistry_各メトリクスを記録した場合_topicラベルつきの5系列が出力されること(t *testing.T) {
	// Arrange
	r := New()
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.UTC)
	r.SetLastCollected("orders", at)
	r.IncProcessed("orders")
	r.IncProcessed("orders")
	r.IncDeliveryFailure("orders")
	r.SetDLQCount("invoices", 1)
	r.SetBacklogCount("orders", 3)

	// Act
	body := scrape(t, r)

	// Assert
	wants := []string{
		`file_pubsub_last_collect_timestamp_seconds{topic="orders"} 1.781256601e+09`,
		`file_pubsub_processed_total{topic="orders"} 2`,
		`file_pubsub_delivery_failure_total{topic="orders"} 1`,
		`file_pubsub_dlq_count{topic="invoices"} 1`,
		`file_pubsub_backlog_count{topic="orders"} 3`,
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("metrics output missing %q\n%s", w, body)
		}
	}
}

func TestSetDLQCount_小さい値を再設定した場合_gaugeとして最新値が反映されること(t *testing.T) {
	// Arrange
	r := New()
	r.SetDLQCount("invoices", 2)

	// Act
	r.SetDLQCount("invoices", 1) // gauge: 減少方向にも設定できる (オペレータが DLQ を捌いた場合)

	// Assert
	if !strings.Contains(scrape(t, r), `file_pubsub_dlq_count{topic="invoices"} 1`) {
		t.Error("dlq_count must reflect the latest set value")
	}
}
