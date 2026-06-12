package domain

import (
	"testing"
	"time"
)

func TestNewMessageID_収集時刻とtopicと元ファイル名を与えた場合_所定形式のIDが生成されること(t *testing.T) {
	// Arrange
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local)

	// Act
	got := NewMessageID(at, "orders", "sales.csv")

	// Assert
	want := "20260612T093001_orders_sales.csv"
	if got != want {
		t.Errorf("NewMessageID = %q, want %q", got, want)
	}
}

func TestNewMessageID_同名ファイルを別時刻に再収集した場合_別のIDが採番されること(t *testing.T) {
	// Arrange & Act
	first := NewMessageID(time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local), "orders", "sales.csv")
	second := NewMessageID(time.Date(2026, 6, 13, 9, 30, 1, 0, time.Local), "orders", "sales.csv")

	// Assert
	if first == second {
		t.Errorf("same-name re-export must get a distinct message_id, both were %q", first)
	}
}

func TestNewMessage_生成した場合_IDと各フィールドが設定されること(t *testing.T) {
	// Arrange
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local)

	// Act
	m := NewMessage(at, "orders", "sales.csv")

	// Assert
	if m.MessageID != "20260612T093001_orders_sales.csv" {
		t.Errorf("MessageID = %q", m.MessageID)
	}
	if m.Topic != "orders" || m.OriginalFileName != "sales.csv" || !m.CollectedAt.Equal(at) {
		t.Errorf("unexpected message: %+v", m)
	}
}
