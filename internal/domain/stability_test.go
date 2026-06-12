package domain

import (
	"testing"
	"time"
)

func obs(size int64, mod, observed time.Time) Observation {
	return Observation{Name: "f.csv", Size: size, ModTime: mod, ObservedAt: observed}
}

func TestIsStable(t *testing.T) {
	base := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	mod := base.Add(-time.Minute)
	interval := 10 * time.Second

	tests := []struct {
		name       string
		prev, curr Observation
		want       bool
	}{
		{
			name: "unchanged size and mtime after interval: stable",
			prev: obs(100, mod, base),
			curr: obs(100, mod, base.Add(interval)),
			want: true,
		},
		{
			name: "size changed: still being written",
			prev: obs(100, mod, base),
			curr: obs(200, mod, base.Add(interval)),
			want: false,
		},
		{
			name: "mtime changed: still being written",
			prev: obs(100, mod, base),
			curr: obs(100, mod.Add(time.Second), base.Add(interval)),
			want: false,
		},
		{
			name: "interval not yet elapsed",
			prev: obs(100, mod, base),
			curr: obs(100, mod, base.Add(interval-time.Second)),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStable(tt.prev, tt.curr, interval); got != tt.want {
				t.Errorf("IsStable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsExcluded(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"orders.csv", nil, false},
		{"orders.csv.tmp", nil, true}, // temp names are always excluded
		{"orders.tmp", nil, true},
		{"orders.bak", []string{"*.bak"}, true},
		{"orders.csv", []string{"*.bak"}, false},
		{"skip_orders.csv", []string{"skip_*"}, true},
		{"orders.csv", []string{"[invalid"}, false}, // invalid pattern never matches
	}
	for _, tt := range tests {
		if got := IsExcluded(tt.name, tt.patterns); got != tt.want {
			t.Errorf("IsExcluded(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
		}
	}
}
