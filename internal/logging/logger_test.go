package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEmit_配信失敗イベントの場合_契約フィールドを持つ1行JSONになること(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	l := New(&buf)
	at := time.Date(2026, 6, 12, 9, 30, 12, 0, time.UTC)
	l.now = func() time.Time { return at }

	// Act
	l.Emit(Event{
		MessageID:    "20260612T093001_orders_sales.csv",
		Topic:        "orders",
		Subscription: "next",
		EventType:    "delivery_failed",
		ErrorDetail:  "write to target directory failed (permission denied); check directory permissions and the running user",
	})

	// Assert
	line := strings.TrimRight(buf.String(), "\n")
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("one event must be one line: %q", line)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, line)
	}
	// schemas.log_line_json のフィールド名。
	if doc["message_id"] != "20260612T093001_orders_sales.csv" || doc["topic"] != "orders" || doc["subscription"] != "next" {
		t.Errorf("delivery event must carry message_id + topic + subscription: %v", doc)
	}
	if doc["event_type"] != "delivery_failed" {
		t.Errorf("event_type = %v", doc["event_type"])
	}
	if doc["error_detail"] == "" || doc["error_detail"] == nil {
		t.Errorf("error_detail missing: %v", doc)
	}
	loggedAt, ok := doc["logged_at"].(string)
	if !ok {
		t.Fatalf("logged_at missing: %v", doc)
	}
	parsed, err := time.Parse(time.RFC3339, loggedAt)
	if err != nil || !parsed.Equal(at) {
		t.Errorf("logged_at = %q, want %v (ISO 8601)", loggedAt, at)
	}
	// slog の既定キーが契約に漏れ出さないこと。
	for _, key := range []string{"time", "level", "msg"} {
		if _, exists := doc[key]; exists {
			t.Errorf("unexpected field %q in log line: %v", key, doc)
		}
	}
}

func TestEmit_メッセージに紐づかないイベントの場合_空のnullableフィールドが省略されること(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	l := New(&buf)

	// Act
	l.Emit(Event{EventType: "daemon_started"})

	// Assert
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["event_type"] != "daemon_started" {
		t.Errorf("event_type = %v", doc["event_type"])
	}
	for _, key := range []string{"message_id", "topic", "subscription", "error_detail"} {
		if _, exists := doc[key]; exists {
			t.Errorf("nullable field %q must be omitted when empty: %v", key, doc)
		}
	}
}

func TestEmit_複数イベントを出力した場合_1イベント1行のJSONになること(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	l := New(&buf)

	// Act
	l.Emit(Event{EventType: "collect", Topic: "orders"})
	l.Emit(Event{EventType: "archive", Topic: "orders"})

	// Assert
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Errorf("line is not standalone JSON: %q", line)
		}
	}
}
