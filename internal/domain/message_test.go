package domain

import (
	"testing"
	"time"
)

func TestNewMessageID(t *testing.T) {
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local)
	got := NewMessageID(at, "orders", "sales.csv")
	want := "20260612T093001_orders_sales.csv"
	if got != want {
		t.Errorf("NewMessageID = %q, want %q", got, want)
	}
}

func TestNewMessageID_SameNameDifferentTimeIsDistinct(t *testing.T) {
	first := NewMessageID(time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local), "orders", "sales.csv")
	second := NewMessageID(time.Date(2026, 6, 13, 9, 30, 1, 0, time.Local), "orders", "sales.csv")
	if first == second {
		t.Errorf("same-name re-export must get a distinct message_id, both were %q", first)
	}
}

func TestNewMessage(t *testing.T) {
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.Local)
	m := NewMessage(at, "orders", "sales.csv")
	if m.MessageID != "20260612T093001_orders_sales.csv" {
		t.Errorf("MessageID = %q", m.MessageID)
	}
	if m.Topic != "orders" || m.OriginalFileName != "sales.csv" || !m.CollectedAt.Equal(at) {
		t.Errorf("unexpected message: %+v", m)
	}
}
