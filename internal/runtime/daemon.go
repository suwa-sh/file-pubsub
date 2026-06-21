// Package runtime は常駐デーモンを担う: ロック取得 (stale 復旧つき)、
// 組み込み HTTP 観測サーバー (/healthz, /metrics)、ポーリングスケジューラ、
// グレースフルシャットダウン (SR-006, SR-007, LR-001)。
// 冗長化 (high_availability) 設定時は lease モードで起動し、方式B (lease 自動奪取) /
// 方式A (外部クラスタ委譲) を切り替える (SPEC-015-02, tier-daemon-worker.md)。
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/gateway/watch"
	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

// Daemon は serve サブコマンドの本体。
type Daemon struct {
	Cfg      *config.Config
	Pipeline *usecase.Pipeline
	Log      *logging.Logger
	Lock     *store.LockManager
	Metrics  *metricsreg.Registry
	Stdout   io.Writer

	// テスト用に決定的にするための依存性注入 (本番では nil → 既定実装)。
	now      func() time.Time // 既定 time.Now
	hostname string           // 既定 os.Hostname
	bootID   string           // 既定 readBootID
}

// New はパイプラインの上にデーモンを構築する。stdout が受け取るのは起動
// メッセージのみで、以降はすべて構造化ログに出力される (ui-design.md serve)。
func New(cfg *config.Config, pipe *usecase.Pipeline, log *logging.Logger, metrics *metricsreg.Registry, stdout io.Writer) *Daemon {
	return &Daemon{
		Cfg:      cfg,
		Pipeline: pipe,
		Log:      log,
		Lock:     store.NewLockManager(cfg.DataDir),
		Metrics:  metrics,
		Stdout:   stdout,
	}
}

// SetIdentity は lease に記録する hostname / boot_id を上書きする。冗長構成の受け入れ
// テストが同一プロセス内で別 identity の Daemon を構成し lease 競合を再現するための注入点
// (e2e)。空文字を渡したフィールドは既定 (OS 取得) のまま残す。
func (d *Daemon) SetIdentity(hostname, bootID string) {
	if hostname != "" {
		d.hostname = hostname
	}
	if bootID != "" {
		d.bootID = bootID
	}
}

// SetClock は時刻関数を上書きする。lease の stale 判定・heartbeat を決定的にするための
// 注入点 (e2e)。nil は無視する。
func (d *Daemon) SetClock(now func() time.Time) {
	if now != nil {
		d.now = now
	}
}

// nowFunc は注入された時刻関数 (テスト) または time.Now を返す。
func (d *Daemon) nowFunc() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// identity は lease に記録する hostname / boot_id を解決する。
// 注入値があればそれを使い (テスト)、無ければ OS から取得する。
func (d *Daemon) identity() (hostname, bootID string) {
	hostname = d.hostname
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			h = "unknown-host"
		}
		hostname = h
	}
	bootID = d.bootID
	if bootID == "" {
		bootID = readBootID()
	}
	return hostname, bootID
}

// readBootID は OS の起動世代識別子を返す。Linux は /proc/sys/kernel/random/boot_id を
// 読み、読めない (他 OS 等) 場合は crypto/rand でプロセス起動ごとに採番する (E-011 boot_id)。
func readBootID() string {
	if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失敗は極めて稀。時刻ベースで一意性を確保する。
		return fmt.Sprintf("boot-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// Run はロックを取得し、HTTP サーバーを起動した後、ctx がキャンセルされる
// (停止シグナル) までポーリングを続ける。実行中のサイクルは中断しない:
// キャンセルはサイクルの合間に観測され、その後 HTTP サーバーを停止し
// ロックを解放する (グレースフルシャットダウン。呼び出し側で終了コード 0)。
// store.ErrAlreadyLocked は二重起動を意味する (呼び出し側で終了コード 3)。
//
// high_availability が nil のときは従来どおり単一インスタンス起動 (後方互換)。
// 設定時は lease モードで起動し、方式B では降格後に standby polling へ戻り、
// stale 検知で再昇格する。
func (d *Daemon) Run(ctx context.Context) error {
	if d.Cfg.HighAvailability == nil {
		return d.runSingleInstance(ctx)
	}
	return d.runLease(ctx)
}

// runSingleInstance は high_availability 省略時の従来動作 (PID lock の二重起動防止)。
func (d *Daemon) runSingleInstance(ctx context.Context) error {
	if err := d.Lock.Acquire(os.Getpid(), d.nowFunc()); err != nil {
		return err
	}
	defer func() {
		if err := d.Lock.Release(); err != nil {
			d.Log.Emit(logging.Event{EventType: "shutdown_failed", ErrorDetail: fmt.Sprintf("%v. the leftover lock is recovered as stale on the next start", err)})
		}
	}()

	d.emitStartup(fmt.Sprintf("lock acquired (pid %d)", os.Getpid()))
	if err := d.serveActive(ctx, nil); err != nil {
		return err // HTTP 起動失敗等の実行時エラー (exit 1)。lock は上の defer で解放。
	}
	d.Log.Emit(logging.Event{EventType: "shutdown"})
	return nil
}

// runLease は lease モードの起動分岐 (方式B / 方式A、SPEC-015-02)。
// 方式B は standby↔active を行き来し、方式A は常に active 単発で稼働する。
func (d *Daemon) runLease(ctx context.Context) (err error) {
	hostname, bootID := d.identity()
	ha := d.Cfg.HighAvailability
	ttl := ha.LeaseTTL
	hb := time.Duration(ha.HeartbeatInterval) * time.Second

	// lease_ttl は NFS の属性キャッシュ (actimeo、既定最大 60s) より十分大きく取る必要がある
	// (小さすぎると stale 判定がキャッシュ越しに誤り、誤奪取の恐れ)。下限の注意喚起 (fix G)。
	if ttl <= 60 {
		d.Log.Emit(logging.Event{EventType: "lease_ttl_warning", ErrorDetail: fmt.Sprintf("lease_ttl=%ds is not safely above the NFS attribute cache (actimeo, up to 60s). use a larger lease_ttl to avoid false stale takeover", ttl)})
	}

	// heldGen はこのプロセスが現に保持している lease 世代 (0=未保持)。acquire 成功で設定し、
	// heartbeat 更新で進め、降格で 0 に戻す。
	var heldGen atomic.Int64

	defer func() {
		// graceful shutdown (ctx キャンセル) または起動失敗 (err != nil) で抜けるとき、自分が
		// 現に保持している世代の lease のみ解放する (fix E: 起動失敗時の lease リーク防止)。
		// 正常な降格 (lease 喪失) では heldGen=0 のため解放しない。同一ホスト二重起動の失敗
		// (ErrAlreadyLocked) も heldGen=0 (未取得) のため、既存 active の lease を削除しない。
		// 解放判定を hostname+boot_id だけの HoldsLease ではなく generation 一致で行うことで、
		// 同一ホスト/同一 boot_id でも自分の世代だけを安全に解放する。
		if ctx.Err() == nil && err == nil {
			return // 正常な降格 (方式B の standby 復帰など): lease 所有権は手放し済み
		}
		gen := int(heldGen.Load())
		if gen <= 0 {
			return // 未取得 (二重起動失敗等): 解放しない
		}
		if rErr := d.Lock.ReleaseLeaseIfOwner(hostname, bootID, gen); rErr != nil {
			d.Log.Emit(logging.Event{EventType: "shutdown_failed", ErrorDetail: fmt.Sprintf("%v. the leftover lease is recovered as stale on the next start", rErr)})
		}
	}()

	if ha.UniquenessMethod == config.UniquenessMethodExternalCluster {
		return d.runMethodA(ctx, hostname, bootID, ttl, hb, &heldGen)
	}
	return d.runMethodB(ctx, hostname, bootID, ttl, hb, &heldGen)
}

// runMethodA は方式A (外部クラスタ委譲)。常に active として強制奪取で起動し、
// standby polling は回さない。降格後は再昇格せずデーモンを終える (昇格契機は外部クラスタ)。
func (d *Daemon) runMethodA(ctx context.Context, hostname, bootID string, ttl int, hb time.Duration, heldGen *atomic.Int64) error {
	gen, err := d.Lock.AcquireLeaseForced(hostname, bootID, ttl, d.nowFunc())
	if err != nil {
		// 自分自身の二重起動 (ErrAlreadyLocked) は終了コード 3、その他は実行時エラー。
		// 未取得のため heldGen は 0 のまま (defer は既存 lease を解放しない)。
		return err
	}
	heldGen.Store(int64(gen)) // 取得成功: 保持世代を公開
	d.emitStartup(fmt.Sprintf("lease acquired (forced, host=%s gen=%d)", hostname, gen))
	d.Log.Emit(logging.Event{EventType: "lease_active", ErrorDetail: fmt.Sprintf("promoted to active on %s gen %d (method=external_cluster)", hostname, gen)})

	if err := d.serveActive(ctx, &leaseControl{hostname: hostname, bootID: bootID, ttl: ttl, interval: hb, gen: gen, heldGen: heldGen}); err != nil {
		return err // HTTP 起動失敗等の実行時エラー (exit 1)。lease は runLease の defer で解放。
	}

	if ctx.Err() == nil {
		// ctx キャンセルでない退出 = 降格。方式A は再昇格しない (外部クラスタが契機)。
		d.Log.Emit(logging.Event{EventType: "lease_demoted", ErrorDetail: fmt.Sprintf("demoted on %s (method=external_cluster); scheduler stopped, awaiting external cluster", hostname)})
	} else {
		d.Log.Emit(logging.Event{EventType: "shutdown"})
	}
	return nil
}

// runMethodB は方式B (lease 自動奪取)。standby↔active を行き来する。
// 同一ホストの有効 lease は ErrAlreadyLocked で終了コード 3、他ホストは standby 待機、
// stale は奪取して active 昇格。降格後は standby polling へ戻り再昇格を狙う。
func (d *Daemon) runMethodB(ctx context.Context, hostname, bootID string, ttl int, hb time.Duration, heldGen *atomic.Int64) error {
	for {
		if ctx.Err() != nil {
			d.Log.Emit(logging.Event{EventType: "shutdown"})
			return nil
		}

		gen, err := d.Lock.AcquireLease(hostname, bootID, ttl, d.nowFunc())
		switch {
		case err == nil:
			// active 昇格。serveActive が降格 (lease lost / ttl 超過) で戻るまで稼働する。
			heldGen.Store(int64(gen)) // 取得成功: 保持世代を公開
			d.emitStartup(fmt.Sprintf("lease acquired (host=%s gen=%d)", hostname, gen))
			d.Log.Emit(logging.Event{EventType: "lease_active", ErrorDetail: fmt.Sprintf("promoted to active on %s gen %d (method=lease)", hostname, gen)})
			if err := d.serveActive(ctx, &leaseControl{hostname: hostname, bootID: bootID, ttl: ttl, interval: hb, gen: gen, heldGen: heldGen}); err != nil {
				return err // HTTP 起動失敗等の実行時エラー (exit 1)。lease は runLease の defer で解放。
			}
			if ctx.Err() != nil {
				d.Log.Emit(logging.Event{EventType: "shutdown"})
				return nil
			}
			// 降格して戻った: 保持していないので heldGen を 0 に戻し、standby polling へ移行する。
			heldGen.Store(0)
			d.Log.Emit(logging.Event{EventType: "lease_demoted", ErrorDetail: fmt.Sprintf("demoted on %s; scheduler stopped, returning to standby (method=lease)", hostname)})
		case errors.Is(err, store.ErrAlreadyLocked):
			// 同一ホスト二重起動: standby が意味を持たないため終了コード 3 で終了する。
			return err
		case errors.Is(err, store.ErrLeaseHeld):
			// 他ホストの有効 lease: standby 待機に入り、ttl 失効を監視する。
			d.Log.Emit(logging.Event{EventType: "lease_standby", ErrorDetail: fmt.Sprintf("another host holds the lease; standby on %s, watching for ttl expiry (method=lease)", hostname)})
		default:
			// read 不能等の I/O エラー: 奪取を楽観視せず standby としてリトライする (fail-closed)。
			d.Log.Emit(logging.Event{EventType: "lease_standby", ErrorDetail: fmt.Sprintf("acquire lease failed: %v; standby on %s and retrying (method=lease)", err, hostname)})
		}

		// standby polling: heartbeat_interval 程度の間隔で AcquireLease を再試行する。
		if !d.standbyWait(ctx, hb) {
			d.Log.Emit(logging.Event{EventType: "shutdown"})
			return nil
		}
	}
}

// standbyWait は hb だけ待ち、待てたら true を返す。ctx キャンセルで false を返す。
func (d *Daemon) standbyWait(ctx context.Context, hb time.Duration) bool {
	if hb <= 0 {
		hb = time.Second
	}
	t := time.NewTimer(hb)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// leaseControl は active 稼働中の heartbeat に必要な情報をまとめる。nil の場合は
// 単一インスタンスモードで heartbeat を起動しない。
type leaseControl struct {
	hostname string
	bootID   string
	ttl      int
	interval time.Duration
	gen      int
	// heldGen はこのプロセスが現に保持している lease 世代を公開する (0=未保持)。heartbeat が
	// 更新成功ごとに進め、降格 (lease lost / ttl 超過) で 0 に戻す。runLease の defer はこれを
	// 読み、自分が保持している世代の lease のみ解放する (同一ホスト二重起動で既存 active の
	// lease を誤って消さないため、generation 一致で所有を判定する)。
	heldGen *atomic.Int64
}

// leaseChecker は usecase.LeaseChecker を LockManager の HoldsLease で実装する
// アダプタ。active 稼働中の Pipeline に注入し、各メッセージ境界・永続化点で lease
// 保持を確認させる (spec-decision-011)。単一インスタンス運用では Pipeline.Lease を
// nil のままにして lease 確認をスキップする (後方互換)。
type leaseChecker struct {
	lock     *store.LockManager
	hostname string
	bootID   string
	now      func() time.Time
}

// HoldsLease は自ホスト・自世代が ttl 以内に lease を保持しているかを返す。read I/O
// 失敗は (false, error) を返し、呼び出し側 (usecase) が fail-closed に倒せるようにする。
func (c *leaseChecker) HoldsLease() (bool, error) {
	return c.lock.HoldsLease(c.hostname, c.bootID, c.now())
}

// serveActive は active としての稼働本体: 組込 HTTP サーバ + ポーリングスケジューラ +
// inbox 監視 + (lease モードのみ) heartbeat goroutine を起動し、ctx キャンセル
// (graceful shutdown) または lease 喪失 (lease モードの降格) まで RunCycle ループを回す。
// lc が nil なら単一インスタンスモードで heartbeat / 降格は無い。
// 戻り値は HTTP サーバ起動失敗 (net.Listen 失敗) のときのみ非 nil (実行時エラー、exit 1)。
// graceful shutdown / 降格で正常に抜けた場合は nil を返す (fix E)。
func (d *Daemon) serveActive(ctx context.Context, lc *leaseControl) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", d.Metrics.Handler())
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", d.Cfg.MetricsPort))
	if err != nil {
		d.Log.Emit(logging.Event{EventType: "http_server_failed", ErrorDetail: fmt.Sprintf("start http server on metrics_port %d failed: %v. set metrics_port to a free port", d.Cfg.MetricsPort, err)})
		// 起動失敗は黙って成功扱いにせず実行時エラーとして返す (exit 1)。lease は呼び出し側
		// (runLease の defer) が所有者確認のうえ解放し、フェイルオーバーを TTL 待ちにしない。
		return fmt.Errorf("start http server on metrics_port %d: %w", d.Cfg.MetricsPort, err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.Log.Emit(logging.Event{EventType: "http_server_failed", ErrorDetail: fmt.Sprintf("%v. restart the daemon after checking metrics_port", err)})
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// active 稼働の生存制御 ctx: ctx キャンセル または heartbeat 降格で打ち切る。
	activeCtx, stopActive := context.WithCancel(ctx)
	defer stopActive()

	// heartbeat goroutine の終了待ち。serveActive を抜ける前に heartbeat を止めて完了を待つ
	// ことで、最後の Heartbeat 成功後の heldGen 更新を確実に反映させる (lock 世代の更新と
	// heldGen.Store の間に graceful shutdown が割り込み、解放が TTL 待ちになる窓を塞ぐ)。
	var hbWG sync.WaitGroup
	defer func() {
		stopActive() // 先に heartbeat を止める
		hbWG.Wait()  // 終了 (= 最終 heldGen 更新の完了) を待ってから抜ける
	}()

	// lease モード (active) のみ Pipeline へ LeaseChecker を注入し、メッセージ境界・
	// 永続化前の lease 保持確認を有効にする (spec-decision-011)。降格・停止で active を
	// 抜けるときに nil へ戻し、次サイクル以降に古い lease 確認が残らないようにする。
	// 単一インスタンス (lc == nil) では注入せず、lease 確認をスキップする (後方互換)。
	if lc != nil {
		d.Pipeline.Lease = &leaseChecker{lock: d.Lock, hostname: lc.hostname, bootID: lc.bootID, now: d.nowFunc}
		defer func() { d.Pipeline.Lease = nil }()
	}

	// lease モードのみ heartbeat goroutine を起動する。Heartbeat が ErrLeaseLost を
	// 返すか ttl 超過まで更新失敗が続いたら stopActive で RunCycle ループを抜けさせる。
	if lc != nil {
		hbWG.Add(1)
		go func() {
			defer hbWG.Done()
			d.heartbeatLoop(activeCtx, stopActive, lc)
		}()
	}

	// 実行中のサイクルは停止シグナル後でも完了まで走らせる (SR-007)。
	cycleCtx := context.WithoutCancel(ctx)

	// 主ポーリング・inbox の fsnotify イベント・フォールバックポーリングを単一の triggers へ
	// 集約し、単一コンシューマが RunCycle を直列実行する。直列化により二重検知 (イベント駆動 +
	// フォールバック) でもサイクルが重ならず冪等になる (LR-205)。triggers はバッファ 1 で coalesce。
	triggers := make(chan struct{}, 1)
	fire := func() {
		select {
		case triggers <- struct{}{}:
		default:
		}
	}

	ticker := time.NewTicker(time.Duration(d.Cfg.PollingInterval) * time.Second)
	defer ticker.Stop()
	go tick(activeCtx, ticker.C, fire)

	// push 受信モード: 受信ディレクトリを fsnotify で監視 + フォールバックポーリング (REQ-013)。
	inboxDirs := d.inboxDirs()
	if len(inboxDirs) > 0 {
		// フォールバックポーリングは fsnotify の成否によらず常に起動する。fsnotify 初期化や
		// 監視登録に失敗しても (NFS 等)、設定した fallback_poll_interval で確実に取り込む。
		fb := time.NewTicker(time.Duration(d.minFallbackInterval()) * time.Second)
		defer fb.Stop()
		go tick(activeCtx, fb.C, fire)

		w, err := watch.New(inboxDirs, func(dir string, err error) {
			d.Log.Emit(logging.Event{EventType: "collect_failed", ErrorDetail: fmt.Sprintf("watch inbox %q failed: %v. relying on fallback polling for this directory", dir, err)})
		})
		if err != nil {
			d.Log.Emit(logging.Event{EventType: "collect_failed", ErrorDetail: fmt.Sprintf("init fsnotify watcher failed: %v. relying on fallback polling for inbox topics", err)})
		} else {
			defer func() { _ = w.Close() }()
			go w.Run(activeCtx, func(err error) {
				d.Log.Emit(logging.Event{EventType: "collect_failed", ErrorDetail: fmt.Sprintf("fsnotify watcher error: %v. fallback polling continues", err)})
			})
			go forward(activeCtx, w.Trigger(), fire)
		}
	}

	d.Pipeline.RunCycle(cycleCtx)
	for {
		select {
		case <-activeCtx.Done():
			return nil
		case <-triggers:
			d.Pipeline.RunCycle(cycleCtx)
		}
	}
}

// heartbeatLoop は lc.interval ごとに Heartbeat を呼び renewed_at を更新する (active のみ)。
// ErrLeaseLost (他ノード/他世代が奪取済み) を受けるか、更新失敗が続いて ttl 超過に至ったら
// stopActive を呼んで scheduler を止め、active を降格させる (SPEC-015-03)。generation は
// AcquireLease の戻り値を起点に Heartbeat の戻り値で更新していく。
func (d *Daemon) heartbeatLoop(ctx context.Context, stopActive context.CancelFunc, lc *leaseControl) {
	interval := lc.interval
	if interval <= 0 {
		interval = time.Second
	}
	gen := lc.gen
	lastRenew := d.nowFunc() // 最後に成功した renewed_at (ttl 超過判定の基準)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := d.nowFunc()
			newGen, err := d.Lock.Heartbeat(lc.hostname, lc.bootID, gen, now)
			if err == nil {
				gen = newGen
				lastRenew = now
				if lc.heldGen != nil {
					lc.heldGen.Store(int64(newGen)) // 保持世代を最新へ更新 (解放判定の基準)
				}
				continue
			}
			if errors.Is(err, store.ErrLeaseLost) {
				// 他ノード/他世代が奪取済み: active を継続せず自発降格する。lease を失った
				// ため heldGen を 0 に戻し、defer が他世代の lease を解放しないようにする。
				if lc.heldGen != nil {
					lc.heldGen.Store(0)
				}
				d.Log.Emit(logging.Event{EventType: "lease_lost", ErrorDetail: fmt.Sprintf("heartbeat detected lease lost on %s: %v. stopping scheduler and demoting", lc.hostname, err)})
				stopActive()
				return
			}
			// 一時的な更新失敗 (NFS 断等): ttl 超過まで再試行し、超過したら降格する。
			if now.Sub(lastRenew) > time.Duration(lc.ttl)*time.Second {
				// ttl 超過降格: 他ノードに奪取された可能性が高いため heldGen を 0 に戻す。
				if lc.heldGen != nil {
					lc.heldGen.Store(0)
				}
				d.Log.Emit(logging.Event{EventType: "lease_lost", ErrorDetail: fmt.Sprintf("heartbeat update failed past ttl on %s: %v. check NFS connectivity, NTP sync, and lease_ttl. stopping scheduler and demoting", lc.hostname, err)})
				stopActive()
				return
			}
			d.Log.Emit(logging.Event{EventType: "heartbeat_failed", ErrorDetail: fmt.Sprintf("heartbeat update failed on %s (within ttl, will retry): %v", lc.hostname, err)})
		}
	}
}

// emitStartup は起動時メッセージ (Lock 取得結果・設定要約・メトリクスポート) を stdout に
// 出し、startup イベントを構造化ログに出す (active 昇格時。tier-daemon-worker.md 処理フロー 6)。
func (d *Daemon) emitStartup(lockResult string) {
	subs := 0
	for _, t := range d.Cfg.Topics {
		subs += len(t.Subscriptions)
	}
	inboxDirs := d.inboxDirs()
	_, _ = fmt.Fprintf(d.Stdout, "file-pubsub serve: %s, topics=%d subscriptions=%d inbox=%d, metrics on :%d, polling every %ds\n",
		lockResult, len(d.Cfg.Topics), subs, len(inboxDirs), d.Cfg.MetricsPort, d.Cfg.PollingInterval)
	d.Log.Emit(logging.Event{EventType: "startup"})
}

// inboxDirs は push 受信モード (type=inbox) の受信ディレクトリ一覧を返す。
func (d *Daemon) inboxDirs() []string {
	var dirs []string
	for _, t := range d.Cfg.Topics {
		if t.Source.Type == config.SourceTypeInbox {
			dirs = append(dirs, t.Source.Directory)
		}
	}
	return dirs
}

// minFallbackInterval は inbox トピックのフォールバックポーリング間隔の最小値 (秒) を返す。
// 取りこぼし対策として、最も短い間隔で全 inbox をフォールバックポーリングする。
func (d *Daemon) minFallbackInterval() int {
	min := 0
	for _, t := range d.Cfg.Topics {
		if t.Source.Type != config.SourceTypeInbox {
			continue
		}
		fi := t.Source.FallbackPollInterval
		if fi <= 0 {
			fi = d.Cfg.PollingInterval
		}
		if min == 0 || fi < min {
			min = fi
		}
	}
	if min == 0 {
		min = d.Cfg.PollingInterval
	}
	return min
}

// tick は ctx がキャンセルされるまで c の各 tick で fire を呼ぶ。
func tick(ctx context.Context, c <-chan time.Time, fire func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c:
			fire()
		}
	}
}

// forward は ctx がキャンセルされるまで src の各受信で fire を呼ぶ。
func forward(ctx context.Context, src <-chan struct{}, fire func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-src:
			fire()
		}
	}
}
