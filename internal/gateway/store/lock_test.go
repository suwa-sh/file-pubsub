package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockManager_AcquireAndRelease(t *testing.T) {
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	if err := l.Acquire(12345, now); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	pid, acquiredAt, err := l.Holder()
	if err != nil {
		t.Fatalf("Holder: %v", err)
	}
	if pid != 12345 || !acquiredAt.Equal(now) {
		t.Errorf("holder = %d at %v", pid, acquiredAt)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "lock")); !os.IsNotExist(err) {
		t.Error("lock file must be removed on release")
	}
}

func TestLockManager_DuplicateStartFails(t *testing.T) {
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return true } // holder is alive

	if err := l.Acquire(12345, time.Now()); err != nil {
		t.Fatal(err)
	}
	err := l.Acquire(12346, time.Now())
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("second acquire = %v, want ErrAlreadyLocked", err)
	}
	// The live holder's lock file must be untouched (SR-006).
	pid, _, err := l.Holder()
	if err != nil || pid != 12345 {
		t.Errorf("holder changed: pid=%d err=%v", pid, err)
	}
}

func TestLockManager_StaleLockIsRecovered(t *testing.T) {
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return false } // holder is dead

	if err := l.Acquire(99999, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(12346, time.Now()); err != nil {
		t.Fatalf("stale lock must be recovered: %v", err)
	}
	pid, _, err := l.Holder()
	if err != nil || pid != 12346 {
		t.Errorf("holder = %d, err=%v, want new pid 12346", pid, err)
	}
}

func TestLockManager_UnreadableLockIsNotStolen(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "lock"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLockManager(dataDir)
	l.alive = func(pid int) bool { return false }
	if err := l.Acquire(12346, time.Now()); err == nil {
		t.Error("unreadable lock must not be stolen")
	}
}

func TestLockManager_ReleaseWithoutLock(t *testing.T) {
	l := NewLockManager(t.TempDir())
	if err := l.Release(); err != nil {
		t.Errorf("release without lock must not fail: %v", err)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("current process must be alive")
	}
	if processAlive(0) || processAlive(-1) {
		t.Error("non-positive pid must be dead")
	}
}
