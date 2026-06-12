package domain

import "testing"

func TestShouldIsolate(t *testing.T) {
	tests := []struct {
		retryCount, retryMaxCount int
		want                      bool
	}{
		{0, 5, false},
		{4, 5, false},
		{5, 5, true}, // BDD: retry_max_count=5 and retry_count=5 isolates
		{6, 5, true},
	}
	for _, tt := range tests {
		if got := ShouldIsolate(tt.retryCount, tt.retryMaxCount); got != tt.want {
			t.Errorf("ShouldIsolate(%d, %d) = %v, want %v", tt.retryCount, tt.retryMaxCount, got, tt.want)
		}
	}
}
