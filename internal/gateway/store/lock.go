package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrAlreadyLocked は生存中のプロセスがロックを保持していることを表す: 呼び出し側
// は二重起動として扱い、exit code 3 で終了しなければならない (SR-006)。
var ErrAlreadyLocked = errors.New("lock held by a live process")

// lockRecord は data_dir/lock ファイルの内容: stale 検出のための保持者プロセス
// 情報と取得時刻 (E-011)。
type lockRecord struct {
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_at"`
}

// LockManager は二重起動防止ロックの取得・解放を行う。プロセス生存確認による
// stale 回収つき (SR-006, LR-002)。
type LockManager struct {
	path  string
	alive func(pid int) bool
}

// NewLockManager は dataDir/lock を管理するマネージャを返す。
func NewLockManager(dataDir string) *LockManager {
	return &LockManager{path: filepath.Join(dataDir, "lock"), alive: processAlive}
}

// Acquire は pid のためにロックを取得する。死んだプロセスが保持するロックは
// stale として安全に回収する (削除して再取得)。生存中のプロセスが保持するロックは、
// 既存のロックファイルに触れずに ErrAlreadyLocked を返す。
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
				continue // create と read の間に保持者が解放した場合: リトライ
			}
			// ロック内容が読めない場合: 生存中かもしれないロックを奪わない。
			return fmt.Errorf("acquire lock: unreadable lock file %s: %w", l.path, readErr)
		}
		if l.alive(rec.PID) {
			return fmt.Errorf("acquire lock: held by pid %d since %s: %w", rec.PID, rec.AcquiredAt.Format(time.RFC3339), ErrAlreadyLocked)
		}
		// stale ロック: 保持者は死んでいるので安全に回収する。
		if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("acquire lock: recover stale lock: %w", rmErr)
		}
	}
	return fmt.Errorf("acquire lock: contention on %s: %w", l.path, ErrAlreadyLocked)
}

// create はロックファイルを排他的に書き、2 つの起動が同時に勝てないようにする。
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

// Release はロックファイルを削除する (graceful shutdown)。ファイルが無いことは
// エラーではない。
func (l *LockManager) Release() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// Holder は記録された保持者の pid と取得時刻を返す。
func (l *LockManager) Holder() (pid int, acquiredAt time.Time, err error) {
	var rec lockRecord
	if err := readJSON(l.path, &rec); err != nil {
		return 0, time.Time{}, fmt.Errorf("read lock: %w", err)
	}
	return rec.PID, rec.AcquiredAt, nil
}

// processAlive は pid が生存中のプロセスを指すかどうかを返す (signal 0 による確認)。
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
