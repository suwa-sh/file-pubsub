package runtime

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

// newTestDaemon は t.TempDir の実体ファイルで動く Daemon を構築する。
// metrics_port=0 で OS に空きポートを割り当てさせ、テスト間のポート競合を避ける。
// hostname/bootID/now を注入して lease 判定を決定的にする。
func newTestDaemon(t *testing.T, ha *config.HighAvailability, hostname, bootID string, now func() time.Time) *Daemon {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		PollingInterval:  3600, // テスト中にポーリングが走らないよう十分大きく
		MetricsPort:      0,
		DataDir:          dir,
		HighAvailability: ha,
	}
	lg := logging.New(io.Discard)
	metrics := metricsreg.New()
	pipe := usecase.NewPipeline(cfg, lg, metrics)
	d := New(cfg, pipe, lg, metrics, io.Discard)
	d.hostname = hostname
	d.bootID = bootID
	d.now = now
	return d
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func daemonWith(topics ...config.Topic) *Daemon {
	return &Daemon{Cfg: &config.Config{PollingInterval: 60, Topics: topics}}
}

func inboxTopic(name, dir string, fallback int) config.Topic {
	return config.Topic{
		Name: name,
		Source: config.Source{
			Type:                 config.SourceTypeInbox,
			Directory:            dir,
			FallbackPollInterval: fallback,
		},
	}
}

func TestInboxDirs_inboxとpullが混在する場合_inboxの受信ディレクトリだけ返ること(t *testing.T) {
	// Arrange
	d := daemonWith(
		config.Topic{Name: "orders", Source: config.Source{Type: config.SourceTypeSFTP, Directory: "/out/orders"}},
		inboxTopic("invoices", "/inbox/invoices", 30),
		inboxTopic("receipts", "/inbox/receipts", 10),
	)

	// Act
	dirs := d.inboxDirs()

	// Assert
	if len(dirs) != 2 || dirs[0] != "/inbox/invoices" || dirs[1] != "/inbox/receipts" {
		t.Errorf("inboxDirs = %v, want only the two inbox directories", dirs)
	}
}

func TestInboxDirs_inboxが無い場合_空になること(t *testing.T) {
	// Arrange
	d := daemonWith(config.Topic{Name: "orders", Source: config.Source{Type: config.SourceTypeLocal, Directory: "/out/orders"}})

	// Act & Assert
	if dirs := d.inboxDirs(); len(dirs) != 0 {
		t.Errorf("inboxDirs without inbox topics must be empty, got %v", dirs)
	}
}

func TestMinFallbackInterval_複数のinboxがある場合_最小の間隔を返すこと(t *testing.T) {
	// Arrange
	d := daemonWith(
		inboxTopic("invoices", "/inbox/invoices", 30),
		inboxTopic("receipts", "/inbox/receipts", 10),
	)

	// Act & Assert
	if got := d.minFallbackInterval(); got != 10 {
		t.Errorf("minFallbackInterval = %d, want 10", got)
	}
}

func TestMinFallbackInterval_fallbackが未設定のinboxの場合_polling_intervalを使うこと(t *testing.T) {
	// Arrange (fallback=0 は applyDefaults 前の状態を模す。polling_interval を流用する)
	d := daemonWith(inboxTopic("invoices", "/inbox/invoices", 0))

	// Act & Assert
	if got := d.minFallbackInterval(); got != 60 {
		t.Errorf("minFallbackInterval = %d, want polling_interval 60", got)
	}
}

// runUntilSettled は ctx を即キャンセルして Run を 1 サイクル稼働させ降ろす。
// active 起動 → graceful shutdown の経路を決定的に通すためのヘルパ。
func runUntilSettled(t *testing.T, d *Daemon) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	// active 起動 (lock 取得 + 初回 RunCycle) の機会を与えてから停止する。
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after ctx cancel")
		return nil
	}
}

func TestRun_HighAvailabilityがnilの場合_従来どおりLockを取得して起動すること(t *testing.T) {
	// Arrange (単一インスタンスモード=後方互換。PID lock を使う)
	d := newTestDaemon(t, nil, "host-a", "boot-1", fixedNow(time.Now()))

	// Act
	err := runUntilSettled(t, d)

	// Assert
	if err != nil {
		t.Fatalf("single-instance Run returned error: %v", err)
	}
	// lock ファイルが解放されている (graceful shutdown で Release 済み)。
	if _, _, herr := d.Lock.Holder(); herr == nil {
		t.Errorf("lock file should be released after graceful shutdown")
	}
}

func TestRun_方式Bでlockが無い場合_AcquireLeaseでactiveになること(t *testing.T) {
	// Arrange
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ha := &config.HighAvailability{UniquenessMethod: config.UniquenessMethodLease, LeaseTTL: 30, HeartbeatInterval: 10}
	d := newTestDaemon(t, ha, "host-a", "boot-1", fixedNow(now))

	// Act: active 昇格後 ctx キャンセルで降ろす。lease を観測するため別 LockManager で確認する。
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	held, _ := d.Lock.HoldsLease("host-a", "boot-1", now)
	cancel()
	err := <-done

	// Assert
	if err != nil {
		t.Fatalf("method-B Run returned error: %v", err)
	}
	if !held {
		t.Errorf("host-a should hold the lease after acquiring (active)")
	}
	// graceful shutdown 後は自世代の lease が解放されていること (heldGen → ReleaseLeaseIfOwner。
	// heartbeat goroutine の終了待ちで最終世代が反映され、TTL 待ちにならない)。
	if stillHeld, _ := d.Lock.HoldsLease("host-a", "boot-1", now); stillHeld {
		t.Errorf("lease should be released after graceful shutdown, but host-a still holds it")
	}
}

func TestRun_方式Bで同一ホストの有効leaseがある場合_終了コード3相当のエラーになること(t *testing.T) {
	// Arrange: 同一 host-a/boot-1 の有効 lease を先に書いておく。
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ha := &config.HighAvailability{UniquenessMethod: config.UniquenessMethodLease, LeaseTTL: 30, HeartbeatInterval: 10}
	d := newTestDaemon(t, ha, "host-a", "boot-1", fixedNow(now))
	if _, err := d.Lock.AcquireLease("host-a", "boot-1", 30, now); err != nil {
		t.Fatalf("seed lease failed: %v", err)
	}

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := d.Run(ctx)

	// Assert: ErrAlreadyLocked = cmd 側で終了コード 3。
	if !errors.Is(err, store.ErrAlreadyLocked) {
		t.Fatalf("want ErrAlreadyLocked (exit 3), got %v", err)
	}
	// 二重起動に失敗したプロセスは既存 active の lease を削除してはならない (codex blocker:
	// generation 一致でのみ解放するため、未取得 (heldGen=0) のこのプロセスは解放しない)。
	held, hErr := d.Lock.HoldsLease("host-a", "boot-1", now)
	if hErr != nil {
		t.Fatalf("HoldsLease check failed: %v", hErr)
	}
	if !held {
		t.Fatal("二重起動失敗で既存 active の lease が削除された (削除されてはならない)")
	}
}

func TestRun_方式Bで他ホストの有効leaseがある場合_standby待機に入ること(t *testing.T) {
	// Arrange: 他ホスト host-b の有効 lease を先に書く。host-a は奪取できず standby になる。
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ha := &config.HighAvailability{UniquenessMethod: config.UniquenessMethodLease, LeaseTTL: 30, HeartbeatInterval: 10}
	d := newTestDaemon(t, ha, "host-a", "boot-1", fixedNow(now))
	if _, err := d.Lock.AcquireLease("host-b", "boot-b", 30, now); err != nil {
		t.Fatalf("seed lease failed: %v", err)
	}

	// Act: standby はブロックするので ctx キャンセルで降ろす。standby 中は lease を奪わない。
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	// standby 中は host-b の lease を奪っていないこと。
	stillB, _ := d.Lock.HoldsLease("host-b", "boot-b", now)
	hostAActive, _ := d.Lock.HoldsLease("host-a", "boot-1", now)
	cancel()
	err := <-done

	// Assert
	if err != nil {
		t.Fatalf("standby Run should return nil on ctx cancel, got %v", err)
	}
	if !stillB {
		t.Errorf("standby host-a must NOT take over host-b's valid lease")
	}
	if hostAActive {
		t.Errorf("host-a must not be active while host-b holds a valid lease")
	}
}

func TestHeartbeatLoop_ErrLeaseLostを返した場合_activeを降格すること(t *testing.T) {
	// Arrange: host-a の lease を gen=1 で取得した後、host-b が奪取し generation を進める。
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ha := &config.HighAvailability{UniquenessMethod: config.UniquenessMethodLease, LeaseTTL: 30, HeartbeatInterval: 10}
	d := newTestDaemon(t, ha, "host-a", "boot-1", fixedNow(now))
	gen, err := d.Lock.AcquireLease("host-a", "boot-1", 30, now)
	if err != nil {
		t.Fatalf("seed lease failed: %v", err)
	}
	// stale にして host-b に奪取させる (generation が進み host-a は lease lost になる)。
	stale := now.Add(40 * time.Second)
	if _, err := d.Lock.AcquireLease("host-b", "boot-b", 30, stale); err != nil {
		t.Fatalf("host-b takeover failed: %v", err)
	}

	// Act: heartbeatLoop は次の tick で Heartbeat→ErrLeaseLost を受け stopActive を呼ぶ。
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	demoted := make(chan struct{})
	stopActive := func() {
		select {
		case <-demoted:
		default:
			close(demoted)
		}
	}
	lc := &leaseControl{hostname: "host-a", bootID: "boot-1", ttl: 30, interval: 10 * time.Millisecond, gen: gen}
	go d.heartbeatLoop(ctx, stopActive, lc)

	// Assert
	select {
	case <-demoted:
		// 降格が発火した。
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop did not demote on ErrLeaseLost")
	}
}

func TestRun_方式Aで他ホストのleaseが残っていても強制奪取してactiveになること(t *testing.T) {
	// Arrange: 他ホスト host-b の有効 lease が残留している (旧 active)。方式A は強制奪取する。
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ha := &config.HighAvailability{UniquenessMethod: config.UniquenessMethodExternalCluster, LeaseTTL: 30, HeartbeatInterval: 10}
	d := newTestDaemon(t, ha, "host-a", "boot-1", fixedNow(now))
	if _, err := d.Lock.AcquireLease("host-b", "boot-b", 30, now); err != nil {
		t.Fatalf("seed residual lease failed: %v", err)
	}

	// Act
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	hostAActive, _ := d.Lock.HoldsLease("host-a", "boot-1", now)
	stillB, _ := d.Lock.HoldsLease("host-b", "boot-b", now)
	cancel()
	err := <-done

	// Assert
	if err != nil {
		t.Fatalf("method-A Run returned error: %v", err)
	}
	if !hostAActive {
		t.Errorf("method-A host-a must forcibly take over and become active")
	}
	if stillB {
		t.Errorf("method-A must overwrite host-b's residual lease")
	}
}
