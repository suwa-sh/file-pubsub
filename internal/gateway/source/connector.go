// Package source は収集コネクタのインターフェースを提供する。システム内で唯一の
// インターフェースであり (LP-301, CLP-001)、local / ftp / sftp / scp の各コネクタは
// この背後で交換可能で、下流の工程はソース種別に依存しない。
package source

import (
	"context"
	"fmt"
	"time"
)

// FileInfo はソースファイル 1 件の観測結果 (名前・サイズ・mtime) で、
// 安定判定に使う。
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Connector は収集コネクタの共通インターフェース (C-01)。
type Connector interface {
	// List はソースディレクトリ内のファイル (名前・サイズ・mtime) を返す。
	List(ctx context.Context) ([]FileInfo, error)
	// Fetch は name を一時名で destDir にダウンロードし、コピーを検証して
	// 最終名にリネームし (LR-303)、ローカルパスを返す。
	Fetch(ctx context.Context, name, destDir string) (string, error)
	// Remove は元ファイルを削除する。archive 保存の成功を確認した後にのみ
	// 呼び出すこと (delete 扱い, LR-303)。
	Remove(ctx context.Context, name string) error
	Close() error
}

// Options はコネクタ実装の選択と設定を行う。
type Options struct {
	Type      string // local / ftp / sftp / scp
	Host      string
	Port      int
	Directory string
	Username  string
	Password  string
	KeyFile   string
}

// New は o.Type に対応するコネクタを返す。リモートコネクタは初回利用時に遅延接続
// するため、接続失敗はここではなく List / Fetch / Remove で表面化する (topic の
// collect_failed としてログに記録される)。
func New(o Options) (Connector, error) {
	switch o.Type {
	case "local", "inbox":
		// inbox (push 受信モード) は受信ディレクトリへのローカル FS I/O であり、List/Fetch/Remove は
		// Local と同一。完了検知 (rename/marker) は usecase 層の責務のためコネクタは Local を再利用する。
		return NewLocal(o.Directory), nil
	case "ftp":
		return NewFTP(o), nil
	case "sftp":
		return NewSFTP(o), nil
	case "scp":
		return NewSCP(o), nil
	default:
		return nil, fmt.Errorf("unknown source type %q", o.Type)
	}
}
