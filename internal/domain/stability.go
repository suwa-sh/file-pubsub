package domain

import (
	"path"
	"strings"
	"time"
)

// Observation はソースファイルのポーリング 1 周期分の観測結果で、
// 書き込み完了 (安定) 判定に使う (SP-003, LR-203)。
type Observation struct {
	Name       string
	Size       int64
	ModTime    time.Time
	ObservedAt time.Time
}

// IsStable はファイルの書き込みが完了したとみなせるかどうかを返す:
// 2 回の観測でサイズと mtime が変化せず、観測間隔が stabilityInterval 以上
// 経過していること。書き込み途中のファイルは次の周期に持ち越される。
func IsStable(prev, curr Observation, stabilityInterval time.Duration) bool {
	if prev.Size != curr.Size || !prev.ModTime.Equal(curr.ModTime) {
		return false
	}
	return curr.ObservedAt.Sub(prev.ObservedAt) >= stabilityInterval
}

// IsExcluded はそのファイル名を収集対象から除外すべきかどうかを返す。
// 一時ファイル名 (*.tmp) は常に除外し、書き込み途中のファイルがパイプラインに
// 入らないようにする (LR-303)。その他の glob パターンはソース設定に由来する。
// 不正なパターンはマッチしない (config バリデーションで拒否される)。
func IsExcluded(name string, patterns []string) bool {
	if strings.HasSuffix(name, ".tmp") {
		return true
	}
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}
