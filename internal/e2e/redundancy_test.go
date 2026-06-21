// Package e2e の冗長構成 (high_availability) 受け入れテスト。
// 「デーモンを起動する」「冪等に処理を再開する」UC の spec BDD シナリオ
// (active/standby 自動フェイルオーバー、メッセージ境界 lease 確認) を、
// 実ファイル (t.TempDir) + 実 LockManager / ManifestStore / 実 Daemon.Run で
// end-to-end に検証する。複数ホストは同一プロセス内で別 hostname/boot_id の
// Daemon (または LockManager) を同一 data_dir に対して動かして再現し、実時間に
// 依存する lease 判定は SetClock / 小さい interval 注入で決定的にする。
//
// NFS 上の O_CREATE|O_EXCL の原子性そのものは実装依存の既知制約のためここでは
// テスト対象外 (spec に明記済み)。実機 NFS 検証は examples/docker-compose の
// 手順に委ねる。
package e2e

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/runtime"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

// haFixture は冗長構成の e2e で複数ホストが共有する 1 つの data_dir / source /
// subscription レイアウトを保持する。fixture を共有し、別 identity の Daemon を
// 複数構成することで「同一 NFS を複数ホストが見る」状況を 1 プロセス内で再現する。
type haFixture struct {
	cfg     *config.Config
	srcDir  string
	dataDir string
	curDir  string
	nextDir string
}

// newHAFixture は spec の具体値 (topic=orders、subscriptions=current/next、
// lease TTL=30、heartbeat=10) に沿った冗長構成 fixture を 1 つ作る。
// metrics_port=0 で OS に空きポートを割り当てさせ、テスト間のポート競合を避ける。
func newHAFixture(t *testing.T, method string) *haFixture {
	t.Helper()
	base := t.TempDir()
	f := &haFixture{
		srcDir:  filepath.Join(base, "src"),
		dataDir: filepath.Join(base, "data"),
		curDir:  filepath.Join(base, "subs", "current"),
		nextDir: filepath.Join(base, "subs", "next"),
	}
	for _, d := range []string{f.srcDir, f.dataDir, f.curDir, f.nextDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg = &config.Config{
		PollingInterval:  3600, // テスト中に自走ポーリングが走らないよう十分大きく
		ArchiveRetention: 90,
		RetryMaxCount:    3,
		MetricsPort:      0,
		DataDir:          f.dataDir,
		HighAvailability: &config.HighAvailability{
			UniquenessMethod:  method,
			LeaseTTL:          30,
			HeartbeatInterval: 10,
		},
		Topics: []config.Topic{{
			Name: "orders",
			Source: config.Source{
				Type:                 config.SourceTypeLocal,
				Directory:            f.srcDir,
				OriginalFileHandling: config.HandlingDelete,
				StabilityCheck:       config.StabilityCheck{Interval: 10},
			},
			Subscriptions: []config.Subscription{
				{Name: "current", Directory: f.curDir},
				{Name: "next", Directory: f.nextDir},
			},
		}},
	}
	return f
}

// newHADaemon は fixture を共有する 1 ノード分の Daemon を、指定 hostname/boot_id/
// clock を注入して構成する。同一 data_dir を見る複数ノードを 1 プロセスで作るための
// ヘルパ。
func newHADaemon(t *testing.T, f *haFixture, hostname, bootID string, now func() time.Time) *runtime.Daemon {
	t.Helper()
	lg := logging.New(io.Discard)
	metrics := metricsreg.New()
	pipe := usecase.NewPipeline(f.cfg, lg, metrics)
	d := runtime.New(f.cfg, pipe, lg, metrics, io.Discard)
	d.SetIdentity(hostname, bootID)
	d.SetClock(now)
	return d
}

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

// -------------------- デーモンを起動する (start daemon) --------------------

// SPEC「デーモン起動と active 昇格」正常系: 方式B で lock(lease) 無しから lease を
// 取得し active になること。
func TestHA_方式Bでlockが無い場合_leaseを取得してactiveになること(t *testing.T) {
	// Arrange (lock(lease) ファイルが存在しない host-a)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	d := newHADaemon(t, f, "host-a", "boot-a1", fixedClock(now))

	// Act (active 昇格を観測してから降ろす)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	held, _ := store.NewLockManager(f.dataDir).HoldsLease("host-a", "boot-a1", now)
	cancel()
	err := <-done

	// Assert (host-a が lease を保持して active 稼働した)
	if err != nil {
		t.Fatalf("method-B run returned error: %v", err)
	}
	if !held {
		t.Fatal("host-a must hold the lease after acquiring (active)")
	}
}

// SPEC「standby 待機と昇格」正常系前半: 方式B で他ホストの有効 lease があるとき
// standby 待機に入り、その有効 lease を奪わないこと。
func TestHA_方式Bで他ホストの有効leaseがある場合_standby待機に入り奪わないこと(t *testing.T) {
	// Arrange (host-b の有効 lease を data_dir に先に書く)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	lock := store.NewLockManager(f.dataDir)
	if _, err := lock.AcquireLease("host-b", "boot-b1", 30, now); err != nil {
		t.Fatalf("seed host-b lease failed: %v", err)
	}
	d := newHADaemon(t, f, "host-a", "boot-a1", fixedClock(now))

	// Act (host-a は奪取できず standby。ブロックするので ctx キャンセルで降ろす)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	stillB, _ := lock.HoldsLease("host-b", "boot-b1", now)
	aActive, _ := lock.HoldsLease("host-a", "boot-a1", now)
	cancel()
	err := <-done

	// Assert (有効 lease は host-b のまま、host-a は active にならない)
	if err != nil {
		t.Fatalf("standby run should return nil on ctx cancel, got %v", err)
	}
	if !stillB {
		t.Fatal("standby host-a must NOT take over host-b's valid lease")
	}
	if aActive {
		t.Fatal("host-a must not be active while host-b holds a valid lease")
	}
}

// SPEC「stale lease 奪取」正常系: 方式B で他ホストの stale lease (renewed_at + ttl 超過)
// を奪取し boot_id を更新して active へ昇格すること。
func TestHA_方式Bでstale_leaseがある場合_奪取してactiveへ昇格すること(t *testing.T) {
	// Arrange (host-b の lease を書き、ttl=30 を超える時刻に時計を進めて stale にする)
	seed := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	lock := store.NewLockManager(f.dataDir)
	if _, err := lock.AcquireLease("host-b", "boot-b1", 30, seed); err != nil {
		t.Fatalf("seed host-b lease failed: %v", err)
	}
	after := seed.Add(60 * time.Second) // renewed_at + ttl(30) を超過 → stale
	d := newHADaemon(t, f, "host-a", "boot-a1", fixedClock(after))

	// Act
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	aActive, _ := lock.HoldsLease("host-a", "boot-a1", after)
	cancel()
	err := <-done

	// Assert (host-a が stale lease を奪取して active へ昇格)
	if err != nil {
		t.Fatalf("takeover run returned error: %v", err)
	}
	if !aActive {
		t.Fatal("host-a must take over the stale lease and become active")
	}
}

// SPEC「lease 喪失の自発降格」異常系: heartbeat の所有者検証 + generation CAS により、
// 奪取された旧 active (host-a) が lease を奪い返さず降格すること。host-b が奪取済みの
// 状態で host-a が Heartbeat を試みると ErrLeaseLost になり、host-b の lease は保持される。
func TestHA_奪取された旧activeのheartbeatが_lease喪失で奪い返さず降格すること(t *testing.T) {
	// Arrange (host-a が gen=1 で取得 → 時刻を進めて host-b が stale 奪取し gen を進める)
	seed := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	lock := store.NewLockManager(f.dataDir)
	genA, err := lock.AcquireLease("host-a", "boot-a1", 30, seed)
	if err != nil {
		t.Fatalf("seed host-a lease failed: %v", err)
	}
	after := seed.Add(60 * time.Second)
	if _, err := lock.AcquireLease("host-b", "boot-b1", 30, after); err != nil {
		t.Fatalf("host-b takeover failed: %v", err)
	}

	// Act (旧 active host-a が古い世代で Heartbeat → 所有者/世代不一致を検知)
	_, hbErr := lock.Heartbeat("host-a", "boot-a1", genA, after.Add(time.Second))

	// Assert (ErrLeaseLost。host-b の lease を奪い返していない)
	if !errors.Is(hbErr, store.ErrLeaseLost) {
		t.Fatalf("stale active heartbeat must return ErrLeaseLost, got %v", hbErr)
	}
	bHolds, _ := lock.HoldsLease("host-b", "boot-b1", after.Add(time.Second))
	if !bHolds {
		t.Fatal("the new active host-b must keep its lease (old active must not steal it back)")
	}
	aHolds, _ := lock.HoldsLease("host-a", "boot-a1", after.Add(time.Second))
	if aHolds {
		t.Fatal("the demoted old active host-a must not hold the lease")
	}
}

// SPEC「同一ホスト二重起動」異常系: 同一ホストの有効 lease があるとき 2 つ目の起動が
// 終了コード 3 相当 (ErrAlreadyLocked) で弾かれること。
func TestHA_方式Bで同一ホストの二重起動が_終了コード3相当で弾かれること(t *testing.T) {
	// Arrange (host-a/boot-a1 の有効 lease を先に書く)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	if _, err := store.NewLockManager(f.dataDir).AcquireLease("host-a", "boot-a1", 30, now); err != nil {
		t.Fatalf("seed lease failed: %v", err)
	}
	d := newHADaemon(t, f, "host-a", "boot-a1", fixedClock(now))

	// Act (同一 identity の 2 つ目の起動)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := d.Run(ctx)

	// Assert (cmd 側で exit code 3 にマップされる ErrAlreadyLocked)
	if !errors.Is(err, store.ErrAlreadyLocked) {
		t.Fatalf("second start on same host must return ErrAlreadyLocked (exit 3), got %v", err)
	}
}

// SPEC「外部クラスタ委譲」異常系: 方式A は他ホストの残留 lease があっても強制奪取して
// active になること (TTL 失効を待たない)。
func TestHA_方式Aで他ホストの残留leaseがあっても_強制奪取してactiveになること(t *testing.T) {
	// Arrange (host-b の有効 lease が残留している = 旧 active の名残)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodExternalCluster)
	lock := store.NewLockManager(f.dataDir)
	if _, err := lock.AcquireLease("host-b", "boot-b1", 30, now); err != nil {
		t.Fatalf("seed residual lease failed: %v", err)
	}
	d := newHADaemon(t, f, "host-a", "boot-a1", fixedClock(now))

	// Act
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	aActive, _ := lock.HoldsLease("host-a", "boot-a1", now)
	stillB, _ := lock.HoldsLease("host-b", "boot-b1", now)
	cancel()
	err := <-done

	// Assert (host-a が強制奪取し active、host-b の残留 lease は上書きされる)
	if err != nil {
		t.Fatalf("method-A run returned error: %v", err)
	}
	if !aActive {
		t.Fatal("method-A host-a must forcibly take over and become active")
	}
	if stillB {
		t.Fatal("method-A must overwrite host-b's residual lease")
	}
}

// -------------------- 冪等に処理を再開する (idempotent resume) --------------------

// seedPartialDelivery は archive 実体 + current=delivered / next=failed の delivering
// マニフェストを実ストアで作り、「current は配信済み、next が未配信のまま異常終了した」
// 状態を再現する。再開サイクルが next にのみ配信することの前提を作る。
func seedPartialDelivery(t *testing.T, f *haFixture, messageID, fileName, payload string, at time.Time) {
	t.Helper()
	archive := store.NewArchiveStore(f.dataDir)
	if err := archive.PutWork("orders", messageID, strings.NewReader(payload)); err != nil {
		t.Fatalf("put archive work failed: %v", err)
	}
	if err := archive.Promote("orders", messageID); err != nil {
		t.Fatalf("promote archive failed: %v", err)
	}
	m := &store.Manifest{
		MessageID:        messageID,
		Topic:            "orders",
		OriginalFileName: fileName,
		CollectedAt:      at,
		Status:           domain.StatusDelivering,
		Subscriptions:    []store.SubscriptionDelivery{},
	}
	delivered := at
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &delivered, "")
	m.SetSubscriptionState("next", domain.SubscriptionFailed, nil, "seed: crashed before next delivery")
	if err := store.NewManifestStore(f.dataDir).Put(m); err != nil {
		t.Fatalf("put seed manifest failed: %v", err)
	}
}

// SPEC「未配信のみ再配信」正常系: 再起動後 (lease モード) に未配信の Subscription
// (next) にのみ配信し、配信済み (current) へは重複配置しないこと。
func TestHA_再起動後の再開サイクルが_未配信のnextにのみ配信しcurrentへ重複しないこと(t *testing.T) {
	// Arrange (current=delivered / next=failed の delivering を seed。current は既に
	// コンシューマが引き取った想定で配置先には置かない → 重複配置されないことの観測点)
	now := time.Date(2026, 6, 20, 17, 10, 1, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	seedPartialDelivery(t, f, "20260620T171001_orders_sales.csv", "sales.csv", "id,qty\n1,5\n", now)

	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return now }

	// Act (再開サイクル: lease モードでも fanout の冪等再入が成立することを確認する)
	pipe.Fanout(context.Background())

	// Assert (next にのみ配置、current へは重複配置されない、manifest は delivered)
	if !exists(filepath.Join(f.nextDir, "sales.csv")) {
		t.Fatal("the undelivered subscription next must receive the file on resume")
	}
	if exists(filepath.Join(f.curDir, "sales.csv")) {
		t.Fatal("the already-delivered subscription current must NOT be re-delivered")
	}
	m, err := store.NewManifestStore(f.dataDir).Get("20260620T171001_orders_sales.csv")
	if err != nil {
		t.Fatal(err)
	}
	states := m.SubscriptionStates()
	if states["current"] != domain.SubscriptionDelivered || states["next"] != domain.SubscriptionDelivered {
		t.Fatalf("after resume both must be delivered, got %v", states)
	}
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", m.Status)
	}
}

// SPEC「別ホスト昇格後の再開」正常系: standby から昇格した active (host-b) が、別ホスト
// (host-a) のクラッシュ再開と等価に未配信 (next) のみ配信すること。host-b が stale lease を
// 奪取して active になり、最初の RunCycle で next にのみ配信する経路を end-to-end に通す。
func TestHA_standbyから昇格したhostbが_別ホストのクラッシュ再開と等価に未配信のみ配信すること(t *testing.T) {
	// Arrange (host-a が active のまま異常終了 → 部分配信マニフェスト + host-a の lease 残留)
	seed := time.Date(2026, 6, 20, 17, 10, 1, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	seedPartialDelivery(t, f, "20260620T171001_orders_sales.csv", "sales.csv", "id,qty\n1,5\n", seed)
	lock := store.NewLockManager(f.dataDir)
	if _, err := lock.AcquireLease("host-a", "boot-a1", 30, seed); err != nil {
		t.Fatalf("seed host-a residual lease failed: %v", err)
	}

	// host-b は ttl 失効後に昇格する。時計を ttl 超過後へ進め、stale 奪取 → active で起動する。
	after := seed.Add(60 * time.Second)
	d := newHADaemon(t, f, "host-b", "boot-b1", fixedClock(after))

	// Act (host-b が昇格し最初の RunCycle で再開配信する。配信完了を待って降ろす)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	waitFor(t, 5*time.Second, "host-b resume delivery to next", func() bool {
		return exists(filepath.Join(f.nextDir, "sales.csv"))
	})
	// graceful shutdown 前に lease 保持を観測する (停止すると Release されるため)。
	bHolds, _ := lock.HoldsLease("host-b", "boot-b1", after)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("host-b run returned error: %v", err)
	}

	// Assert (host-b が lease を奪取して active、next にのみ配信、current へ重複なし)
	if !bHolds {
		t.Fatal("host-b must hold the lease after promotion")
	}
	if exists(filepath.Join(f.curDir, "sales.csv")) {
		t.Fatal("the already-delivered subscription current must NOT be re-delivered after promotion")
	}
	m, err := store.NewManifestStore(f.dataDir).Get("20260620T171001_orders_sales.csv")
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered (resume equivalent to crash recovery on another host)", m.Status)
	}
}

// SPEC「split-brain 被害局限」異常系: メッセージ境界 lease 確認により、途中で lease を
// 失ったら処理中の 1 メッセージで停止し以降を処理しないこと (split-brain の被害が高々
// 1 メッセージに限定される根拠)。real LockManager + leaseChecker 相当を Pipeline に注入し、
// 2 件 archived のうち 1 件目処理後に lease を失わせ、2 件目が未配信に留まることを確認する。
func TestHA_メッセージ境界lease確認で_途中喪失時に高々1メッセージで停止すること(t *testing.T) {
	// Arrange (2 件 archived。host-a が gen=1 で lease 保持 → 1 件目配信後に host-b が奪取)
	now := time.Date(2026, 6, 20, 17, 10, 1, 0, time.UTC)
	f := newHAFixture(t, config.UniquenessMethodLease)
	lock := store.NewLockManager(f.dataDir)
	genA, err := lock.AcquireLease("host-a", "boot-a1", 30, now)
	if err != nil {
		t.Fatalf("seed host-a lease failed: %v", err)
	}
	seedArchivedHA(t, f, "20260620T171001_orders_a.csv", "a.csv", "AAA", now)
	seedArchivedHA(t, f, "20260620T171002_orders_b.csv", "b.csv", "BBB", now)

	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return now }
	// メッセージ境界ごとに HoldsLease を呼ぶ実 leaseChecker 相当。最初の確認では host-a が
	// 保持しているが、最初の永続化点を通過した直後に host-b が奪取して host-a を失わせる。
	pipe.Lease = &takeoverOnNthLease{lock: lock, hostname: "host-a", bootID: "boot-a1", now: now, takeoverAt: 2, takeover: func() {
		// host-b が stale ではなく heartbeat 経由で奪取した状況を gen CAS で再現する。
		if _, herr := lock.Heartbeat("host-b", "boot-b1", genA, now); herr == nil {
			t.Fatal("host-b heartbeat should fail (not owner); takeover must go through stale path")
		}
		// 実際の奪取は force で行い、host-a の lease を確実に喪失させる。
		if _, aerr := lock.AcquireLeaseForced("host-b", "boot-b1", 30, now); aerr != nil {
			t.Fatalf("host-b forced takeover failed: %v", aerr)
		}
	}}

	// Act (fanout: 1 件目は lease 保持で配信、2 件目の境界で lease 喪失 → 停止)
	pipe.Fanout(context.Background())

	// Assert (1 件目 a.csv は配信完了、2 件目 b.csv は境界 lease 確認で停止し未配信)
	ms := store.NewManifestStore(f.dataDir)
	mA, gerr := ms.Get("20260620T171001_orders_a.csv")
	if gerr != nil {
		t.Fatal(gerr)
	}
	mB, gerr := ms.Get("20260620T171002_orders_b.csv")
	if gerr != nil {
		t.Fatal(gerr)
	}
	delivered := 0
	if mA.Status == domain.StatusDelivered {
		delivered++
	}
	if mB.Status == domain.StatusDelivered {
		delivered++
	}
	// 被害は高々 1 メッセージに局限される (複数を処理し続けない)。
	if delivered > 1 {
		t.Fatalf("split-brain damage must be at most one message, but %d were delivered", delivered)
	}
	// 2 件目は lease 喪失後の境界確認で停止しているため未配信に留まる。
	if mB.Status == domain.StatusDelivered {
		t.Fatal("the second message must NOT be delivered after losing the lease at the message boundary")
	}
	if exists(filepath.Join(f.nextDir, "b.csv")) || exists(filepath.Join(f.curDir, "b.csv")) {
		t.Fatal("the second message must not be placed after the lease is lost")
	}
}

// takeoverOnNthLease は HoldsLease の takeoverAt 回目の呼び出し直前に takeover を実行して
// 自ホストの lease を喪失させ、以降 false を返す実 LockManager ベースの LeaseChecker。
// 「メッセージ処理の途中で別ホストに奪取された」状況を実ファイルで再現する。
type takeoverOnNthLease struct {
	lock       *store.LockManager
	hostname   string
	bootID     string
	now        time.Time
	takeoverAt int
	takeover   func()
	calls      int
}

func (c *takeoverOnNthLease) HoldsLease() (bool, error) {
	c.calls++
	if c.calls == c.takeoverAt && c.takeover != nil {
		c.takeover()
		c.takeover = nil
	}
	return c.lock.HoldsLease(c.hostname, c.bootID, c.now)
}

// seedArchivedHA は archive 実体 + archived マニフェストを実ストアで作り、fanout 対象の
// 未配信メッセージを 1 件用意する。
func seedArchivedHA(t *testing.T, f *haFixture, messageID, fileName, payload string, at time.Time) {
	t.Helper()
	archive := store.NewArchiveStore(f.dataDir)
	if err := archive.PutWork("orders", messageID, strings.NewReader(payload)); err != nil {
		t.Fatalf("put archive work failed: %v", err)
	}
	if err := archive.Promote("orders", messageID); err != nil {
		t.Fatalf("promote archive failed: %v", err)
	}
	m := &store.Manifest{
		MessageID:        messageID,
		Topic:            "orders",
		OriginalFileName: fileName,
		CollectedAt:      at,
		Status:           domain.StatusArchived,
		Subscriptions:    []store.SubscriptionDelivery{},
	}
	if err := store.NewManifestStore(f.dataDir).Put(m); err != nil {
		t.Fatalf("put seed manifest failed: %v", err)
	}
}
