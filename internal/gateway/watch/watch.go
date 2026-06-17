// Package watch は push 受信モード (inbox) の受信ディレクトリを fsnotify で監視し、
// ファイル出現を即時の取り込み契機 (トリガ) に変換する (REQ-013, LR-003)。
// fsnotify を取りこぼす共有 FS (NFS/SMB 等) はフォールバックポーリングに委ねる前提で、
// 監視登録の失敗は致命ではなく通知のみ行う。
package watch

import (
	"context"

	"github.com/fsnotify/fsnotify"
)

// Watcher は受信ディレクトリ群を監視し、イベントを coalesce したトリガを提供する。
type Watcher struct {
	fsw  *fsnotify.Watcher
	trig chan struct{}
}

// New は dirs を監視する Watcher を生成する。個々のディレクトリの監視登録に失敗しても
// New 自体は成功し、失敗は onError(dir, err) で通知する (NFS 等はフォールバックに委ねる)。
func New(dirs []string, onError func(dir string, err error)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{fsw: fsw, trig: make(chan struct{}, 1)}
	for _, d := range dirs {
		if err := fsw.Add(d); err != nil && onError != nil {
			onError(d, err)
		}
	}
	return w, nil
}

// Trigger はファイルイベント発生を伝えるチャネルを返す。バッファ 1 で coalesce し、
// 保留中のトリガがあれば新規イベントは合流する (取り込みサイクルは冪等なため取りこぼさない)。
func (w *Watcher) Trigger() <-chan struct{} { return w.trig }

// Run は ctx がキャンセルされるまで fsnotify イベントを読み続け、イベントごとに
// Trigger を発火する。Errors は onError で通知する。
func (w *Watcher) Run(ctx context.Context, onError func(err error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.fire()
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			if onError != nil {
				onError(err)
			}
		}
	}
}

// fire はトリガを非ブロッキングで発火する。保留中なら合流する。
func (w *Watcher) fire() {
	select {
	case w.trig <- struct{}{}:
	default:
	}
}

// Close は監視を解放する。
func (w *Watcher) Close() error { return w.fsw.Close() }
