package domain

import (
	"testing"
	"time"
)

func TestRetentionDeadline(t *testing.T) {
	savedAt := time.Date(2026, 6, 12, 9, 30, 5, 0, time.UTC)
	got := RetentionDeadline(savedAt, 90)
	want := time.Date(2026, 9, 10, 9, 30, 5, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("RetentionDeadline = %v, want %v", got, want)
	}
}

func TestIsExpired(t *testing.T) {
	deadline := time.Date(2026, 9, 10, 9, 30, 5, 0, time.UTC)
	tests := []struct {
		now  time.Time
		want bool
	}{
		{deadline.Add(-time.Second), false},
		{deadline, false}, // exactly at the deadline: still kept
		{deadline.Add(time.Second), true},
	}
	for _, tt := range tests {
		if got := IsExpired(deadline, tt.now); got != tt.want {
			t.Errorf("IsExpired(deadline, %v) = %v, want %v", tt.now, got, tt.want)
		}
	}
}
