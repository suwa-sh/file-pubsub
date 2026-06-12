package domain

import (
	"testing"
	"time"
)

// obs はテスト用の Observation を生成するヘルパー。
func obs(size int64, mod, observed time.Time) Observation {
	return Observation{Name: "f.csv", Size: size, ModTime: mod, ObservedAt: observed}
}

func TestIsStable_観測結果の組み合わせごとに_書き込み完了のみ安定と判定されること(t *testing.T) {
	// Arrange
	base := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	mod := base.Add(-time.Minute)
	interval := 10 * time.Second

	tests := []struct {
		name       string
		prev, curr Observation
		want       bool
	}{
		{
			name: "間隔経過後もサイズとmtimeが不変の場合_安定と判定されること",
			prev: obs(100, mod, base),
			curr: obs(100, mod, base.Add(interval)),
			want: true,
		},
		{
			name: "サイズが変化した場合_書き込み途中と判定されること",
			prev: obs(100, mod, base),
			curr: obs(200, mod, base.Add(interval)),
			want: false,
		},
		{
			name: "mtimeが変化した場合_書き込み途中と判定されること",
			prev: obs(100, mod, base),
			curr: obs(100, mod.Add(time.Second), base.Add(interval)),
			want: false,
		},
		{
			name: "間隔が未経過の場合_安定と判定されないこと",
			prev: obs(100, mod, base),
			curr: obs(100, mod, base.Add(interval-time.Second)),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := IsStable(tt.prev, tt.curr, interval)

			// Assert
			if got != tt.want {
				t.Errorf("IsStable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsExcluded_ファイル名とパターンの組み合わせごとに_除外対象のみtrueになること(t *testing.T) {
	// Arrange
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"orders.csv", nil, false},
		{"orders.csv.tmp", nil, true}, // 一時ファイル名は常に除外される
		{"orders.tmp", nil, true},
		{"orders.bak", []string{"*.bak"}, true},
		{"orders.csv", []string{"*.bak"}, false},
		{"skip_orders.csv", []string{"skip_*"}, true},
		{"orders.csv", []string{"[invalid"}, false}, // 不正なパターンはマッチしない
	}
	for _, tt := range tests {
		// Act
		got := IsExcluded(tt.name, tt.patterns)

		// Assert
		if got != tt.want {
			t.Errorf("IsExcluded(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
		}
	}
}
