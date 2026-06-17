package domain

import "strings"

// 完了検知 (push 受信モード) の純粋ロジック (REQ-014, SPEC-014-03, LR-204)。
// suffix は config の completion.suffix を呼び出し側から受け取る (domain は config に依存しない)。
// rename では一時拡張子、marker ではマーカー拡張子を表す。

// HasCompletionSuffix は name が完了検知サフィックスで終わるかを返す。
// rename では一時名 (取り込み対象外)、marker ではマーカーファイル (配信対象外) の判定に使う。
func HasCompletionSuffix(name, suffix string) bool {
	return strings.HasSuffix(name, suffix)
}

// MarkerOf は本体ファイル名に対応するマーカー名 (base + suffix) を返す。
func MarkerOf(base, suffix string) string {
	return base + suffix
}

// ReadyByMarker は names のうち、対応するマーカー (本体名 + suffix) が同じ一覧に存在する
// 本体ファイル名だけを true にしたセットを返す。マーカーファイル自身は対象に含めない。
func ReadyByMarker(names []string, suffix string) map[string]bool {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	ready := map[string]bool{}
	for _, n := range names {
		if HasCompletionSuffix(n, suffix) {
			continue // マーカーファイル自身は取り込み対象にしない
		}
		if present[MarkerOf(n, suffix)] {
			ready[n] = true
		}
	}
	return ready
}
