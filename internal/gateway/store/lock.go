package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrAlreadyLocked means a live process holds the lock: the caller must treat
// it as a duplicate start and exit with code 3 (SR-006).
var ErrAlreadyLocked = errors.New("lock held by a live process")

// lockRecord is the content of the data_dir/lock file: holder process info and
// acquisition time for stale detection (E-011).
type lockRecord struct {
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_at"`
}

// LockManager acquires and releases the duplicate-start prevention lock with
// stale recovery by process liveness check (SR-006, LR-002).
type LockManager struct {
	path  string
	alive func(pid int) bool
}

// NewLockManager manages dataDir/lock.
func NewLockManager(dataDir string) *LockManager {
	return &LockManager{path: filepath.Join(dataDir, "lock"), alive: processAlive}
}

// Acquire takes the lock for pid. A lock held by a dead process is stale and
// is safely recovered (removed and re-acquired); a lock held by a live
// process returns ErrAlreadyLocked without touching the existing lock file.
func (l *LockManager) Acquire(pid int, now time.Time) error {
	for attempt := 0; attempt < 2; attempt++ {
		err := l.create(pid, now)
		if err == nil {
			return nil
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire lock: %w", err)
		}

		var rec lockRecord
		if readErr := readJSON(l.path, &rec); readErr != nil {
			if os.IsNotExist(readErr) {
				continue // holder released between create and read; retry
			}
			// Unreadable lock content: do not steal a possibly live lock.
			return fmt.Errorf("acquire lock: unreadable lock file %s: %w", l.path, readErr)
		}
		if l.alive(rec.PID) {
			return fmt.Errorf("acquire lock: held by pid %d since %s: %w", rec.PID, rec.AcquiredAt.Format(time.RFC3339), ErrAlreadyLocked)
		}
		// Stale lock: holder is dead, recover safely.
		if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("acquire lock: recover stale lock: %w", rmErr)
		}
	}
	return fmt.Errorf("acquire lock: contention on %s: %w", l.path, ErrAlreadyLocked)
}

// create writes the lock file exclusively so two starters cannot both win.
func (l *LockManager) create(pid int, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	data := fmt.Sprintf("{\"pid\": %d, \"acquired_at\": %q}\n", pid, now.Format(time.RFC3339))
	if _, err := f.WriteString(data); err != nil {
		_ = f.Close()
		_ = os.Remove(l.path)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(l.path)
		return err
	}
	return f.Close()
}

// Release removes the lock file (graceful shutdown). A missing file is not an
// error.
func (l *LockManager) Release() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// Holder returns the recorded holder pid and acquisition time.
func (l *LockManager) Holder() (pid int, acquiredAt time.Time, err error) {
	var rec lockRecord
	if err := readJSON(l.path, &rec); err != nil {
		return 0, time.Time{}, fmt.Errorf("read lock: %w", err)
	}
	return rec.PID, rec.AcquiredAt, nil
}

// processAlive reports whether pid refers to a live process (signal 0 probe).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
