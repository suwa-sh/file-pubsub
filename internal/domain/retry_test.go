package domain

import "testing"

func TestShouldIsolate_リトライ回数が上限以上の場合_隔離対象になること(t *testing.T) {
	// Arrange
	tests := []struct {
		retryCount, retryMaxCount int
		want                      bool
	}{
		{0, 5, false},
		{4, 5, false},
		{5, 5, true}, // BDD: retry_max_count=5 かつ retry_count=5 で隔離される
		{6, 5, true},
	}
	for _, tt := range tests {
		// Act
		got := ShouldIsolate(tt.retryCount, tt.retryMaxCount)

		// Assert
		if got != tt.want {
			t.Errorf("ShouldIsolate(%d, %d) = %v, want %v", tt.retryCount, tt.retryMaxCount, got, tt.want)
		}
	}
}
