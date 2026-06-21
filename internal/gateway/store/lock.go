package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrAlreadyLocked は生存中のプロセス/同一ホストがロックを保持していることを表す:
// 呼び出し側は二重起動として扱い、exit code 3 で終了しなければならない (SR-006)。
var ErrAlreadyLocked = errors.New("lock held by a live process")

// ErrLeaseHeld は他ホストが有効な lease を保持していることを表す (方式B)。呼び出し側
// は二重 serve を起こさず standby 待機に入る (SPEC-015-02)。複数 standby が同時に stale
// 奪取を試みて O_EXCL の EEXIST で敗北した場合もこれを返す (敗者は standby を継続)。
var ErrLeaseHeld = errors.New("lease held by another host")

// ErrLeaseLost は heartbeat の所有者検証 / generation CAS が不一致で、他ノード/他世代
// が既に lease を奪取済みであることを表す。呼び出し側は renewed_at を更新せず active を
// 継続せず standby 待機へ自発降格する (SPEC-015-03、spec-decision-009)。
var ErrLeaseLost = errors.New("lease lost: held by another host or generation")

// lockRecord は data_dir/lock ファイルの内容: stale 検出のための保持者情報。単一
// インスタンスモードでは PID + AcquiredAt のみ、lease モードでは hostname + boot_id +
// acquired_at + renewed_at + ttl + generation を用いる (E-011、SPEC-015-01)。
type lockRecord struct {
	PID        int       `json:"pid,omitempty"`
	AcquiredAt time.Time `json:"acquired_at"`
	// lease モード用フィールド (high_availability 設定時)。
	Hostname   string    `json:"hostname,omitempty"`
	BootID     string    `json:"boot_id,omitempty"`
	RenewedAt  time.Time `json:"renewed_at,omitempty"`
	TTL        int       `json:"ttl,omitempty"`
	Generation int       `json:"generation,omitempty"`
}

// LockManager は二重起動防止ロックの取得・解放を行う。プロセス生存確認による
// stale 回収つき (SR-006, LR-002)。
type LockManager struct {
	path  string
	alive func(pid int) bool
	// beforeHeartbeatRecheck は Heartbeat の二重チェック (staging 書き込み後・再読込前)
	// に呼ばれるテスト専用フック。本番では nil。read→write の隙に lease を奪取される
	// 競合 (fix C) を決定的に再現するために用いる。
	beforeHeartbeatRecheck func()
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

// isStale は lease レコードが stale(`now - renewed_at > ttl`)かどうかを返す。
// renewed_at がゼロ値(旧スキーマ=PID のみの lock)は lease 情報を持たないため
// stale 相当として扱い、lease 形式へ移行する (SPEC-015-01)。
func isStale(rec lockRecord, ttl int, now time.Time) bool {
	if rec.RenewedAt.IsZero() {
		return true // 旧スキーマ lock: stale 相当として奪取し移行
	}
	return now.Sub(rec.RenewedAt) > time.Duration(ttl)*time.Second
}

// AcquireLease は lease モードでロックを取得する (方式B、SPEC-015-02/03)。
//   - lock 無し → O_CREATE|O_EXCL で作成し generation=1 を返す。
//   - 有効な lease を自ホストが保持 → 同一ホスト二重起動として ErrAlreadyLocked。
//   - 有効な lease を他ホストが保持 → standby 契機として ErrLeaseHeld。
//   - stale lease(または旧スキーマ lock)→ read→remove→O_CREATE|O_EXCL 再作成で
//     奪取し generation を旧+1、boot_id を引数 bootID へ更新する。再作成が EEXIST で
//     失敗したら別 standby が先に奪取したとみなし ErrLeaseHeld を返す(敗者は standby
//     継続)。これにより複数 standby 同時奪取は勝者1人に収束する。
func (l *LockManager) AcquireLease(hostname, bootID string, ttl int, now time.Time) (generation int, err error) {
	gen, err := l.createLease(hostname, bootID, ttl, 1, now)
	if err == nil {
		return gen, nil // lock 無し → 作成成功
	}
	if !os.IsExist(err) {
		return 0, fmt.Errorf("acquire lease: %w", err)
	}

	// lock が既に存在する: 内容を read して active/stale を判定する。
	var rec lockRecord
	if readErr := readJSON(l.path, &rec); readErr != nil {
		if os.IsNotExist(readErr) {
			// create と read の間に保持者が解放した: 安全側に倒し奪取しない。
			return 0, fmt.Errorf("acquire lease: %w", ErrLeaseHeld)
		}
		// 読めない lock は生存中かもしれないため奪わない (fail-closed)。
		return 0, fmt.Errorf("acquire lease: unreadable lock file %s: %w", l.path, readErr)
	}

	if !isStale(rec, ttl, now) {
		// 有効な lease: 自ホストなら二重起動、他ホストなら standby 契機。
		if rec.Hostname == hostname {
			return 0, fmt.Errorf("acquire lease: held by self %s gen %d: %w", hostname, rec.Generation, ErrAlreadyLocked)
		}
		return 0, fmt.Errorf("acquire lease: held by %s gen %d: %w", rec.Hostname, rec.Generation, ErrLeaseHeld)
	}

	// stale(または旧スキーマ): read→remove→O_CREATE|O_EXCL 再作成で奪取する。
	if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) {
		return 0, fmt.Errorf("acquire lease: recover stale lease: %w", rmErr)
	}
	gen, err = l.createLease(hostname, bootID, ttl, rec.Generation+1, now)
	if err != nil {
		if os.IsExist(err) {
			// remove と再作成の間に別 standby が先取りした: 奪取敗北。
			return 0, fmt.Errorf("acquire lease: lost takeover race: %w", ErrLeaseHeld)
		}
		return 0, fmt.Errorf("acquire lease: recreate after takeover: %w", err)
	}
	return gen, nil
}

// AcquireLeaseForced は方式A(外部クラスタ委譲)用に lease を強制的に書き込む
// (tier-daemon-worker.md 処理フロー 2 方式A、SPEC-015-02)。外部クラスタの fencing で
// 旧 active は既に停止済みである前提のため、稼働中 lease の hostname/boot_id が自分と
// 異なれば lease の有効・stale を問わず read→remove→O_CREATE|O_EXCL で自分の lease を
// 書き直す(boot_id を更新し generation を進める)。自分自身(hostname/boot_id 一致)が
// 既に保持していれば二重起動として ErrAlreadyLocked を返す。「lease 有効を見て standby
// に落ちる」判定は行わず、TTL 失効による自動奪取(方式B)も行わない。
func (l *LockManager) AcquireLeaseForced(hostname, bootID string, ttl int, now time.Time) (generation int, err error) {
	gen, err := l.createLease(hostname, bootID, ttl, 1, now)
	if err == nil {
		return gen, nil // lock 無し → 作成成功
	}
	if !os.IsExist(err) {
		return 0, fmt.Errorf("acquire lease forced: %w", err)
	}

	// lock が既に存在する: 内容を read して所有者を確認する。
	var rec lockRecord
	if readErr := readJSON(l.path, &rec); readErr != nil {
		if os.IsNotExist(readErr) {
			// create と read の間に保持者が解放した: 空きとみなし再作成を試みる。
			gen, err = l.createLease(hostname, bootID, ttl, 1, now)
			if err != nil {
				return 0, fmt.Errorf("acquire lease forced: recreate after release: %w", err)
			}
			return gen, nil
		}
		// 読めない lock は奪わない (fail-closed)。
		return 0, fmt.Errorf("acquire lease forced: unreadable lock file %s: %w", l.path, readErr)
	}

	// 自分自身が既に保持している → 同一ホスト/同一世代の二重起動。
	if rec.Hostname == hostname && rec.BootID == bootID {
		return 0, fmt.Errorf("acquire lease forced: held by self %s gen %d: %w", hostname, rec.Generation, ErrAlreadyLocked)
	}

	// 他ホスト/他 boot_id の残留 lease → 有効・stale を問わず read→remove→再作成で奪取する。
	if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) {
		return 0, fmt.Errorf("acquire lease forced: remove stale lease: %w", rmErr)
	}
	gen, err = l.createLease(hostname, bootID, ttl, rec.Generation+1, now)
	if err != nil {
		return 0, fmt.Errorf("acquire lease forced: recreate after takeover: %w", err)
	}
	return gen, nil
}

// createLease は lease レコードを O_CREATE|O_EXCL で排他的に作成する。2 つの起動/奪取
// が同時に勝てないようにし、勝者1人に収束させる。
func (l *LockManager) createLease(hostname, bootID string, ttl, generation int, now time.Time) (int, error) {
	rec := lockRecord{
		Hostname:   hostname,
		BootID:     bootID,
		AcquiredAt: now,
		RenewedAt:  now,
		TTL:        ttl,
		Generation: generation,
	}
	if err := l.writeExclusive(rec); err != nil {
		return 0, err
	}
	return generation, nil
}

// writeExclusive は rec を O_CREATE|O_EXCL で書く。EEXIST はそのまま返す(呼び出し側が
// 奪取敗北/二重起動を判定する)。
func (l *LockManager) writeExclusive(rec lockRecord) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(l.path)
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
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

// Heartbeat は lease の renewed_at を更新する (active のみ、SPEC-015-03)。更新前に
// lock を read して (a)hostname/boot_id が自分自身 かつ (b)generation が expectedGen と
// 一致(generation CAS)するか確認し、両方一致時のみ renewed_at=now・generation+1 で
// AtomicWrite する。不一致(他ノード/他世代が奪取済み=lease lost)なら更新せず
// ErrLeaseLost を返す。これにより旧 active が新 active の lease を read→update の隙に
// 奪い返す TOCTOU を generation 不一致で検出する (spec-decision-009)。
func (l *LockManager) Heartbeat(hostname, bootID string, expectedGen int, now time.Time) (generation int, err error) {
	var rec lockRecord
	if readErr := readJSON(l.path, &rec); readErr != nil {
		// read 不能/lock 無しは保持を楽観視せず lease lost 扱い (fail-closed)。
		return 0, fmt.Errorf("heartbeat: read lock %s: %w", l.path, ErrLeaseLost)
	}
	if rec.Hostname != hostname || rec.BootID != bootID || rec.Generation != expectedGen {
		// 所有者検証 / generation CAS 不一致: 他ノード/他世代が奪取済み。
		return 0, fmt.Errorf("heartbeat: owner=%s gen=%d, want %s gen=%d: %w", rec.Hostname, rec.Generation, hostname, expectedGen, ErrLeaseLost)
	}

	// 更新後レコードを staging に書き出し、rename 直前に再読込して観測時と owner/generation
	// が一致する場合のみ確定する二重チェック (fix C / spec-decision-009)。これにより read→
	// write の隙に他ノード/他世代が lease を奪取していた場合、旧 active が新 active の lease を
	// 上書きするのを防ぐ。NFS では完全な原子性は保証しない (既知制約、案Z) が窓を狭める。
	updated := rec
	updated.RenewedAt = now
	updated.Generation = expectedGen + 1
	staging := l.path + ".hb"
	if err := WriteJSONAtomic(staging, updated); err != nil {
		return 0, fmt.Errorf("heartbeat: %w", err)
	}
	if l.beforeHeartbeatRecheck != nil {
		l.beforeHeartbeatRecheck() // テスト専用: 再読込前に奪取を差し込む
	}
	var current lockRecord
	if readErr := readJSON(l.path, &current); readErr != nil {
		_ = os.Remove(staging)
		return 0, fmt.Errorf("heartbeat: re-read lock %s: %w", l.path, ErrLeaseLost)
	}
	if current.Hostname != hostname || current.BootID != bootID || current.Generation != expectedGen {
		// 観測時から変化: 他ノード/他世代が奪取済み。staging を破棄して降格させる。
		_ = os.Remove(staging)
		return 0, fmt.Errorf("heartbeat: lease taken during update: owner=%s gen=%d: %w", current.Hostname, current.Generation, ErrLeaseLost)
	}
	if err := os.Rename(staging, l.path); err != nil {
		_ = os.Remove(staging)
		return 0, fmt.Errorf("heartbeat: %w", err)
	}
	return updated.Generation, nil
}

// HoldsLease は自分自身(hostname/boot_id 一致)が ttl 以内に lease を保持しているかを
// 返す (lease 保持確認、メッセージ境界 / 永続化前のチェック、spec-decision-011)。lock
// 無し・不一致・ttl 超過は false。read I/O エラーは保持を楽観視せず false + error で
// 返し、呼び出し側が安全側に倒せるようにする (fail-closed)。
func (l *LockManager) HoldsLease(hostname, bootID string, now time.Time) (bool, error) {
	var rec lockRecord
	if err := readJSON(l.path, &rec); err != nil {
		if os.IsNotExist(err) {
			return false, nil // lock 無し: 保持していない
		}
		return false, fmt.Errorf("holds lease: read lock %s: %w", l.path, err)
	}
	if rec.Hostname != hostname || rec.BootID != bootID {
		return false, nil
	}
	if isStale(rec, rec.TTL, now) {
		return false, nil
	}
	return true, nil
}

// ReleaseLeaseIfOwner は lock が hostname + boot_id + generation のすべてに一致する
// (= このプロセスが現に保持している世代の lease である) 場合に限り削除する。二重起動で
// 取得に失敗したプロセスや、既に他世代へ奪取済み (heartbeat で世代が進んだ / 別ノードが
// 奪取した) の場合は削除せず、既存 active の lease を誤って消さない (SR-006・起動シーケンス
// の「既存 lock/lease を変更しない」)。generation<=0 は「未取得」を表し常に削除しない。
func (l *LockManager) ReleaseLeaseIfOwner(hostname, bootID string, generation int) error {
	if generation <= 0 {
		return nil // 未取得: 何も保持していないので解放しない
	}
	var rec lockRecord
	if err := readJSON(l.path, &rec); err != nil {
		if os.IsNotExist(err) {
			return nil // 既に無い
		}
		return fmt.Errorf("release lease: read lock %s: %w", l.path, err)
	}
	if rec.Hostname != hostname || rec.BootID != bootID || rec.Generation != generation {
		return nil // 自分の保持する世代ではない: 他者の lease を消さない
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release lease: %w", err)
	}
	return nil
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
