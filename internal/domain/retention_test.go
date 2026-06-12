package domain

import (
	"testing"
	"time"
)

func TestRetentionDeadline_保持日数90日の場合_保存時刻の90日後が期限になること(t *testing.T) {
	// Arrange
	savedAt := time.Date(2026, 6, 12, 9, 30, 5, 0, time.UTC)

	// Act
	got := RetentionDeadline(savedAt, 90)

	// Assert
	want := time.Date(2026, 9, 10, 9, 30, 5, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("RetentionDeadline = %v, want %v", got, want)
	}
}

func TestIsExpired_期限前後の時刻を与えた場合_期限超過のみ削除対象になること(t *testing.T) {
	// Arrange
	deadline := time.Date(2026, 9, 10, 9, 30, 5, 0, time.UTC)
	tests := []struct {
		now  time.Time
		want bool
	}{
		{deadline.Add(-time.Second), false},
		{deadline, false}, // 期限ちょうどの場合: まだ保持する
		{deadline.Add(time.Second), true},
	}
	for _, tt := range tests {
		// Act
		got := IsExpired(deadline, tt.now)

		// Assert
		if got != tt.want {
			t.Errorf("IsExpired(deadline, %v) = %v, want %v", tt.now, got, tt.want)
		}
	}
}
