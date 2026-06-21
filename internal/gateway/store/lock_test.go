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

// readLeaseRecord はテスト用に lock ファイルの lease レコードを直接読む。
func readLeaseRecord(t *testing.T, dataDir string) lockRecord {
	t.Helper()
	var rec lockRecord
	if err := readJSON(filepath.Join(dataDir, "lock"), &rec); err != nil {
		t.Fatalf("read lease record: %v", err)
	}
	return rec
}

func TestLockManager_AcquireLease_lockが無い場合_作成されgeneration1でhostnameやttlが記録されること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)

	// Assert
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if gen != 1 {
		t.Errorf("generation = %d, want 1", gen)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.BootID != "boot-1" || rec.TTL != 30 || rec.Generation != 1 {
		t.Errorf("record = %+v", rec)
	}
	if !rec.AcquiredAt.Equal(now) || !rec.RenewedAt.Equal(now) {
		t.Errorf("timestamps = acquired %v renewed %v, want %v", rec.AcquiredAt, rec.RenewedAt, now)
	}
}

func TestLockManager_AcquireLease_他ホストの有効leaseの場合_ErrLeaseHeldになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act: host-b が ttl 以内に取得を試みる
	_, err := l.AcquireLease("host-b", "boot-2", 30, now.Add(10*time.Second))

	// Assert
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("AcquireLease = %v, want ErrLeaseHeld", err)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.Generation != 1 {
		t.Errorf("active lease must not change: %+v", rec)
	}
}

func TestLockManager_AcquireLease_同一ホストの有効leaseの場合_ErrAlreadyLockedになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act: 同一 host-a の 2 つ目が起動を試みる
	_, err := l.AcquireLease("host-a", "boot-2", 30, now.Add(10*time.Second))

	// Assert
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("AcquireLease = %v, want ErrAlreadyLocked", err)
	}
}

func TestLockManager_AcquireLease_staleleaseの場合_奪取されgenerationが進み新bootidになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-b", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act: ttl=30 超過(60 秒後)に host-a が奪取する
	gen, err := l.AcquireLease("host-a", "boot-2", 30, now.Add(60*time.Second))

	// Assert
	if err != nil {
		t.Fatalf("stale lease must be stolen: %v", err)
	}
	if gen != 2 {
		t.Errorf("generation = %d, want 2 (旧+1)", gen)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.BootID != "boot-2" || rec.Generation != 2 {
		t.Errorf("record = %+v", rec)
	}
}

func TestLockManager_AcquireLease_旧スキーマのlockが残る場合_stale相当で奪取されlease形式へ移行すること(t *testing.T) {
	// Arrange: 旧スキーマ(PID + acquired_at のみ。renewed_at/ttl 無し)を残す
	dataDir := t.TempDir()
	oldLock := []byte(`{"pid": 4321, "acquired_at": "2026-06-12T08:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(dataDir, "lock"), oldLock, 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)

	// Assert: renewed_at ゼロ値=stale 相当として奪取し lease 形式へ移行
	if err != nil {
		t.Fatalf("old-schema lock must be migrated: %v", err)
	}
	if gen != 1 {
		t.Errorf("generation = %d, want 1 (旧 generation 0 + 1)", gen)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.BootID != "boot-1" || rec.TTL != 30 {
		t.Errorf("record = %+v", rec)
	}
}

func TestLockManager_Heartbeat_self一致かつgeneration一致の場合_renewedatが更新されgenerationが進むこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	later := now.Add(10 * time.Second)
	newGen, err := l.Heartbeat("host-a", "boot-1", gen, later)

	// Assert
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if newGen != gen+1 {
		t.Errorf("generation = %d, want %d", newGen, gen+1)
	}
	rec := readLeaseRecord(t, dataDir)
	if !rec.RenewedAt.Equal(later) || rec.Generation != gen+1 {
		t.Errorf("record = %+v", rec)
	}
}

func TestLockManager_Heartbeat_他ホストが奪取済みの場合_ErrLeaseLostになり更新されないこと(t *testing.T) {
	// Arrange: host-a が取得後、host-b が stale 奪取する
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcquireLease("host-b", "boot-2", 30, now.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Act: host-a が古い generation で heartbeat する
	_, err = l.Heartbeat("host-a", "boot-1", gen, now.Add(70*time.Second))

	// Assert
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("Heartbeat = %v, want ErrLeaseLost", err)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-b" {
		t.Errorf("lease must remain host-b: %+v", rec)
	}
}

func TestLockManager_Heartbeat_generation不一致の場合_ErrLeaseLostになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatal(err)
	}

	// Act: self 一致だが expectedGen が実際より進んでいる(他世代が書いたと仮定)
	_, err = l.Heartbeat("host-a", "boot-1", gen+5, now.Add(10*time.Second))

	// Assert
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("Heartbeat = %v, want ErrLeaseLost", err)
	}
}

func TestLockManager_Heartbeat_read後write前に他ホストが奪取した場合_二重チェックで旧activeが上書きせずErrLeaseLostになること(t *testing.T) {
	// Arrange: host-a が lease を保持。Heartbeat の staging 書き込み後・再読込前に host-b が
	// stale 奪取する状況をフックで差し込む (fix C の read→write 競合)。
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatal(err)
	}
	// 別 LockManager (host-b 視点) で同一 data_dir の lease を stale 奪取する。
	takeover := NewLockManager(dataDir)
	once := false
	l.beforeHeartbeatRecheck = func() {
		if once {
			return
		}
		once = true
		if _, tErr := takeover.AcquireLease("host-b", "boot-2", 30, now.Add(60*time.Second)); tErr != nil {
			t.Fatalf("takeover by host-b: %v", tErr)
		}
	}

	// Act: host-a は初回 read では自世代一致だが、再読込で host-b の奪取を検知するはず。
	_, err = l.Heartbeat("host-a", "boot-1", gen, now.Add(5*time.Second))

	// Assert: 旧 active は降格 (ErrLeaseLost) し、lease は host-b のまま (上書きされない)。
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("Heartbeat = %v, want ErrLeaseLost", err)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-b" {
		t.Errorf("旧 active が host-b の lease を上書きした: %+v", rec)
	}
	if _, statErr := os.Stat(l.path + ".hb"); !os.IsNotExist(statErr) {
		t.Errorf("二重チェック失敗時は staging を破棄すべき: stat=%v", statErr)
	}
}

func TestLockManager_ReleaseLeaseIfOwner_自世代の場合のみ削除し他世代未取得は残すこと(t *testing.T) {
	// Arrange: host-a が gen1 で lease を保持。
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	gen, err := l.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatal(err)
	}

	// Act/Assert 1: 未取得 (gen<=0) は何も削除しない。
	if err := l.ReleaseLeaseIfOwner("host-a", "boot-1", 0); err != nil {
		t.Fatalf("gen<=0: %v", err)
	}
	if held, _ := l.HoldsLease("host-a", "boot-1", now); !held {
		t.Fatal("gen<=0 で lease が削除された")
	}

	// Act/Assert 2: 同一 host/boot でも別世代 (二重起動失敗プロセスが旧世代を持つ等) は削除しない。
	if err := l.ReleaseLeaseIfOwner("host-a", "boot-1", gen+5); err != nil {
		t.Fatalf("別世代: %v", err)
	}
	if held, _ := l.HoldsLease("host-a", "boot-1", now); !held {
		t.Fatal("別世代指定で既存 lease が削除された")
	}

	// Act/Assert 3: 自世代一致のときのみ削除する。
	if err := l.ReleaseLeaseIfOwner("host-a", "boot-1", gen); err != nil {
		t.Fatalf("自世代: %v", err)
	}
	if held, _ := l.HoldsLease("host-a", "boot-1", now); held {
		t.Fatal("自世代一致で lease が解放されていない")
	}
}

func TestLockManager_HoldsLease_self保持かつttl以内の場合_trueになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act
	held, err := l.HoldsLease("host-a", "boot-1", now.Add(10*time.Second))

	// Assert
	if err != nil {
		t.Fatalf("HoldsLease: %v", err)
	}
	if !held {
		t.Error("self holder within ttl must hold the lease")
	}
}

func TestLockManager_HoldsLease_ttl超過の場合_falseになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act: ttl=30 を超過
	held, err := l.HoldsLease("host-a", "boot-1", now.Add(60*time.Second))

	// Assert
	if err != nil {
		t.Fatalf("HoldsLease: %v", err)
	}
	if held {
		t.Error("ttl 超過時は保持していないと判定されること")
	}
}

func TestLockManager_HoldsLease_他ホスト保持の場合_falseになること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act
	held, err := l.HoldsLease("host-b", "boot-2", now.Add(10*time.Second))

	// Assert
	if err != nil {
		t.Fatalf("HoldsLease: %v", err)
	}
	if held {
		t.Error("他ホストは保持していないと判定されること")
	}
}

func TestLockManager_AcquireLease_奪取時にlockが先取りされた場合_ErrLeaseHeldで敗北すること(t *testing.T) {
	// Arrange: stale lease を作り、奪取の read 後・再作成前に別 standby が先取りした状況を
	// O_CREATE|O_EXCL の EEXIST 敗北として確認する(逐次再現)。
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-old", "boot-0", 30, now); err != nil {
		t.Fatal(err)
	}
	steal := now.Add(60 * time.Second) // ttl 超過 → stale

	// 勝者 host-b が先に奪取に成功する(read→remove→O_EXCL 再作成)
	if _, err := l.AcquireLease("host-b", "boot-b", 30, steal); err != nil {
		t.Fatalf("winner steal: %v", err)
	}

	// Act: 敗者 host-c の奪取試行を、再作成だけ EEXIST で失敗させて検証する
	rec := readLeaseRecord(t, dataDir) // host-b の有効 lease(steal 時点ではまだ ttl 以内)
	_ = rec
	// host-c から見て host-b の lease は steal 時点では有効なので ErrLeaseHeld を返す
	_, err := l.AcquireLease("host-c", "boot-c", 30, steal)

	// Assert
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("loser AcquireLease = %v, want ErrLeaseHeld", err)
	}
}

func TestLockManager_AcquireLeaseForced_lockが無い場合_作成されること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act
	gen, err := l.AcquireLeaseForced("host-a", "boot-1", 30, now)

	// Assert
	if err != nil {
		t.Fatalf("AcquireLeaseForced on empty lock: %v", err)
	}
	if gen != 1 {
		t.Errorf("generation = %d, want 1", gen)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.BootID != "boot-1" {
		t.Errorf("record = %+v", rec)
	}
}

func TestLockManager_AcquireLeaseForced_他ホストの有効leaseが残る場合_有効でも強制奪取されること(t *testing.T) {
	// Arrange: 他ホスト host-b の有効 lease (旧 active の残留) を残す。
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-b", "boot-b", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act: ttl 以内 (有効) でも方式A は強制奪取する。
	gen, err := l.AcquireLeaseForced("host-a", "boot-1", 30, now.Add(5*time.Second))

	// Assert
	if err != nil {
		t.Fatalf("forced takeover of valid lease must succeed: %v", err)
	}
	if gen != 2 {
		t.Errorf("generation = %d, want 2 (旧+1)", gen)
	}
	rec := readLeaseRecord(t, dataDir)
	if rec.Hostname != "host-a" || rec.BootID != "boot-1" || rec.Generation != 2 {
		t.Errorf("record = %+v", rec)
	}
}

func TestLockManager_AcquireLeaseForced_自分自身が保持中の場合_ErrAlreadyLockedになること(t *testing.T) {
	// Arrange: 自分自身 (host-a/boot-1) の lease を残す。同一構成の 2 つ目は弾く。
	dataDir := t.TempDir()
	l := NewLockManager(dataDir)
	now := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if _, err := l.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := l.AcquireLeaseForced("host-a", "boot-1", 30, now.Add(5*time.Second))

	// Assert
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("AcquireLeaseForced = %v, want ErrAlreadyLocked", err)
	}
}
