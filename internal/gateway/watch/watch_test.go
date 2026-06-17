package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 注: 本テストはローカルディスク (t.TempDir) 上での fsnotify 動作を検証する。
// NFS/SMB ではイベント取りこぼしがありうるが、それは daemon のフォールバックポーリングで吸収する (LR-003)。
func TestWatcher_監視中のディレクトリにファイルが作成された場合_トリガが発火すること(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	w, err := New([]string{dir}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, nil)

	// Act
	if err := os.WriteFile(filepath.Join(dir, "invoices_0042.csv"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Assert
	select {
	case <-w.Trigger():
	case <-time.After(3 * time.Second):
		t.Fatal("expected a trigger on file creation")
	}
}

func TestNew_存在しないディレクトリを渡した場合_onErrorで通知し監視は継続すること(t *testing.T) {
	// Arrange
	missing := filepath.Join(t.TempDir(), "nonexistent")
	var reported string

	// Act
	w, err := New([]string{missing}, func(dir string, _ error) { reported = dir })

	// Assert
	if err != nil {
		t.Fatalf("New must succeed even if a dir cannot be watched: %v", err)
	}
	defer func() { _ = w.Close() }()
	if reported != missing {
		t.Errorf("Add failure must be reported via onError, got %q", reported)
	}
}

func TestWatcher_連続イベントの場合_トリガが合流して詰まらないこと(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	w, err := New([]string{dir}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, nil)

	// Act (コンシューマが受信する前に複数イベントを起こす)
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i))+".csv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Assert (少なくとも 1 回は発火し、バッファ 1 で coalesce されてブロックしない)
	select {
	case <-w.Trigger():
	case <-time.After(3 * time.Second):
		t.Fatal("expected at least one coalesced trigger")
	}
}
