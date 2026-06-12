package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockManager_AcquireしてReleaseした場合_保持者が記録されロックファイルが消えること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act (Acquire)
	if err := l.Acquire(12345, now); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Assert (Acquire)
	pid, acquiredAt, err := l.Holder()
	if err != nil {
		t.Fatalf("Holder: %v", err)
	}
	if pid != 12345 || !acquiredAt.Equal(now) {
		t.Errorf("holder = %d at %v", pid, acquiredAt)
	}

	// Act (Release)
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Assert (Release)
	if _, err := os.Stat(filepath.Join(dataDir, "lock")); !os.IsNotExist(err) {
		t.Error("lock file must be removed on release")
	}
}

func TestLockManager_Acquire_保持者が生存中の場合_ErrAlreadyLockedになりロックが奪われないこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return true } // 保持者は生存中
	if err := l.Acquire(12345, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Act
	err := l.Acquire(12346, time.Now())

	// Assert
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("second acquire = %v, want ErrAlreadyLocked", err)
	}
	// 生存中の保持者のロックファイルには触れてはならない (SR-006)
	pid, _, err := l.Holder()
	if err != nil || pid != 12345 {
		t.Errorf("holder changed: pid=%d err=%v", pid, err)
	}
}

func TestLockManager_Acquire_保持者が死んでいる場合_staleロックが回収され再取得できること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return false } // 保持者は死亡
	if err := l.Acquire(99999, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Act
	err := l.Acquire(12346, time.Now())

	// Assert
	if err != nil {
		t.Fatalf("stale lock must be recovered: %v", err)
	}
	pid, _, err := l.Holder()
	if err != nil || pid != 12346 {
		t.Errorf("holder = %d, err=%v, want new pid 12346", pid, err)
	}
}

func TestLockManager_Acquire_ロック内容が読めない場合_奪わずエラーになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "lock"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return false }

	// Act & Assert
	if err := l.Acquire(12346, time.Now()); err == nil {
		t.Error("unreadable lock must not be stolen")
	}
}

func TestLockManager_Release_ロックが無い場合_エラーにならないこと(t *testing.T) {
	// Arrange
	l := NewLockManager(t.TempDir())

	// Act & Assert
	if err := l.Release(); err != nil {
		t.Errorf("release without lock must not fail: %v", err)
	}
}

func TestProcessAlive_自プロセスと不正pidを与えた場合_自プロセスだけ生存と判定されること(t *testing.T) {
	// Arrange & Act & Assert
	if !processAlive(os.Getpid()) {
		t.Error("current process must be alive")
	}
	if processAlive(0) || processAlive(-1) {
		t.Error("non-positive pid must be dead")
	}
}
