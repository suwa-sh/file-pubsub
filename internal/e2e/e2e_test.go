// Package e2e はパイプライン全体と常駐デーモンを実ファイルで動かして検証する:
// ゴールデンパス (collect → archive → fan-out → manifest)、冪等な再実行、
// グレースフルシャットダウン、二重起動防止。
package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

type fixture struct {
	cfg     *config.Config
	srcDir  string
	dataDir string
	curDir  string
	nextDir string
}

func newFixture(t *testing.T, pollingInterval, stabilityInterval, metricsPort int) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
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
		PollingInterval:  pollingInterval,
		ArchiveRetention: 90,
		RetryMaxCount:    3,
		MetricsPort:      metricsPort,
		DataDir:          f.dataDir,
		Topics: []config.Topic{{
			Name: "orders",
			Source: config.Source{
				Type:                 config.SourceTypeLocal,
				Directory:            f.srcDir,
				OriginalFileHandling: config.HandlingDelete,
				StabilityCheck:       config.StabilityCheck{Interval: stabilityInterval},
			},
			Subscriptions: []config.Subscription{
				{Name: "current", Directory: f.curDir},
				{Name: "next", Directory: f.nextDir},
			},
		}},
	}
	return f
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRunCycle_ファイルを投入した場合_収集からファンアウトまで完走し再実行で重複しないこと(t *testing.T) {
	// Arrange: パイプラインとソースファイルを用意する
	f := newFixture(t, 60, 10, 9090)
	clock := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return clock }

	if err := os.WriteFile(filepath.Join(f.srcDir, "orders_1.csv"), []byte("a,b,c"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act: 初回観測 (安定判定の持ち越し) → 安定後のサイクルで収集〜ファンアウト
	ctx := context.Background()
	pipe.RunCycle(ctx)
	clock = clock.Add(11 * time.Second)
	pipe.RunCycle(ctx)

	// Assert: 両サブスクリプションが元のファイル名でファイルを受け取る
	for _, dir := range []string{f.curDir, f.nextDir} {
		p := filepath.Join(dir, "orders_1.csv")
		if !exists(p) {
			t.Fatalf("%s must contain the delivered file", dir)
		}
		b, err := os.ReadFile(p)
		if err != nil || string(b) != "a,b,c" {
			t.Fatalf("delivered content = %q, err=%v", b, err)
		}
		if exists(p + ".tmp") {
			t.Fatal("no temp file may remain in a subscription directory")
		}
	}

	// Assert: アーカイブが残りマニフェストは delivered、原本は削除済み (delete ハンドリング)
	manifests, err := store.NewManifestStore(f.dataDir).List()
	if err != nil || len(manifests) != 1 {
		t.Fatalf("manifests = %d, err=%v", len(manifests), err)
	}
	m := manifests[0]
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", m.Status)
	}
	if ok, _ := store.NewArchiveStore(f.dataDir).Exists("orders", m.MessageID); !ok {
		t.Fatal("archive file must remain after delivery")
	}
	if exists(filepath.Join(f.srcDir, "orders_1.csv")) {
		t.Fatal("original file must be deleted from the source")
	}

	// Act: 再実行 (コンシューマーが引き取った状態でもう 1 サイクル)
	for _, dir := range []string{f.curDir, f.nextDir} {
		if err := os.Remove(filepath.Join(dir, "orders_1.csv")); err != nil {
			t.Fatal(err)
		}
	}
	clock = clock.Add(time.Minute)
	pipe.RunCycle(ctx)

	// Assert: 二重配信も再収集も起きない
	for _, dir := range []string{f.curDir, f.nextDir} {
		if exists(filepath.Join(dir, "orders_1.csv")) {
			t.Fatal("a delivered message must not be delivered twice")
		}
	}
	manifests, err = store.NewManifestStore(f.dataDir).List()
	if err != nil || len(manifests) != 1 {
		t.Fatalf("re-run created extra manifests: %d, err=%v", len(manifests), err)
	}
}

// freePort はデーモンテスト用にエフェメラル TCP ポートを確保する。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDaemonRun_停止シグナルを受けた場合_グレースフルに停止しロックが解放されること(t *testing.T) {
	// Arrange: デーモンを起動して healthz が応答するまで待つ
	port := freePort(t)
	f := newFixture(t, 1, 1, port)

	logFile, err := os.Create(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logFile.Close() }()
	lg := logging.New(logFile)
	metrics := metricsreg.New()
	pipe := usecase.NewPipeline(f.cfg, lg, metrics)
	daemon := runtime.New(f.cfg, pipe, lg, metrics, logFile)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()

	healthz := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	waitFor(t, 10*time.Second, "healthz 200", func() bool {
		resp, err := http.Get(healthz)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	// Act & Assert: 同じ data dir で 2 つ目のデーモンを起動すると二重起動になる
	second := runtime.New(f.cfg, pipe, lg, metrics, io.Discard)
	if err := second.Run(context.Background()); !errors.Is(err, store.ErrAlreadyLocked) {
		t.Fatalf("second daemon: err = %v, want ErrAlreadyLocked", err)
	}

	// Act: ファイルを投入してデーモンの配信を待つ
	if err := os.WriteFile(filepath.Join(f.srcDir, "live.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "delivery to both subscriptions", func() bool {
		return exists(filepath.Join(f.curDir, "live.csv")) && exists(filepath.Join(f.nextDir, "live.csv"))
	})

	// Assert: 稼働中の metrics エンドポイントがトピック別カウンタを公開している
	waitFor(t, 10*time.Second, "processed_total metric", func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return err == nil && strings.Contains(string(body), `file_pubsub_processed_total{topic="orders"} 1`)
	})

	// Act: 停止シグナル → グレースフルシャットダウン
	cancel()

	// Assert: クリーンに終了し、ロックが解放され、HTTP が停止している
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon.Run = %v, want nil (graceful shutdown)", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after the stop signal")
	}
	if exists(filepath.Join(f.dataDir, "lock")) {
		t.Fatal("the lock file must be released on graceful shutdown")
	}
	if _, err := http.Get(healthz); err == nil {
		t.Fatal("healthz must be unreachable after shutdown")
	}

	// Assert: デーモン停止前にマニフェストへ配信が記録され、原本は削除済み
	manifests, err := store.NewManifestStore(f.dataDir).List()
	if err != nil || len(manifests) != 1 {
		t.Fatalf("manifests = %d, err=%v", len(manifests), err)
	}
	if manifests[0].Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", manifests[0].Status)
	}
	if exists(filepath.Join(f.srcDir, "live.csv")) {
		t.Fatal("original file must be deleted from the source")
	}
}

// newPushFixture は push 受信モード(inbox)のトピック "invoices"(サブスクリプション current)を
// 構築する。srcDir を受信ディレクトリとして流用する。
func newPushFixture(t *testing.T, mode, suffix, handling string, polling, metricsPort int) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
		srcDir:  filepath.Join(base, "inbox"),
		dataDir: filepath.Join(base, "data"),
		curDir:  filepath.Join(base, "subs", "current"),
	}
	for _, d := range []string{f.srcDir, f.dataDir, f.curDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg = &config.Config{
		PollingInterval:  polling,
		ArchiveRetention: 90,
		RetryMaxCount:    3,
		MetricsPort:      metricsPort,
		DataDir:          f.dataDir,
		Topics: []config.Topic{{
			Name: "invoices",
			Source: config.Source{
				Type:                 config.SourceTypeInbox,
				Directory:            f.srcDir,
				OriginalFileHandling: handling,
				Completion:           config.Completion{Mode: mode, Suffix: suffix},
				StabilityCheck:       config.StabilityCheck{Interval: 10},
				FallbackPollInterval: polling,
			},
			Subscriptions: []config.Subscription{
				{Name: "current", Directory: f.curDir},
			},
		}},
	}
	return f
}

func TestRunCycle_push受信モードのrename方式の場合_一時名は配信せず正式名を即配信すること(t *testing.T) {
	// Arrange
	f := newPushFixture(t, config.CompletionRename, ".tmp", config.HandlingDelete, 60, 9091)
	clock := time.Date(2026, 6, 17, 2, 6, 37, 0, time.UTC)
	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return clock }
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0045.csv.tmp"), []byte("writing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0045.csv"), []byte("id,amount\n1,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act (rename は安定待ちなしで 1 サイクルで収集〜配信される)
	pipe.RunCycle(context.Background())

	// Assert
	if !exists(filepath.Join(f.curDir, "invoices_0045.csv")) {
		t.Fatal("the final name must be delivered to the subscription")
	}
	if exists(filepath.Join(f.curDir, "invoices_0045.csv.tmp")) {
		t.Fatal("the temp name must never be delivered")
	}
	if exists(filepath.Join(f.srcDir, "invoices_0045.csv")) {
		t.Fatal("the collected final file must be removed from the inbox (delete)")
	}
	if !exists(filepath.Join(f.srcDir, "invoices_0045.csv.tmp")) {
		t.Fatal("the temp file must remain (not collected)")
	}
}

func TestRunCycle_push受信モードのmarker方式の場合_本体を配信しマーカーは配信せず両方回収すること(t *testing.T) {
	// Arrange
	f := newPushFixture(t, config.CompletionMarker, ".done", config.HandlingDelete, 60, 9092)
	clock := time.Date(2026, 6, 17, 2, 6, 37, 0, time.UTC)
	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return clock }
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0046.csv"), []byte("id,amount\n1,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0046.csv.done"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	pipe.RunCycle(context.Background())

	// Assert
	if !exists(filepath.Join(f.curDir, "invoices_0046.csv")) {
		t.Fatal("the body must be delivered on marker arrival")
	}
	if exists(filepath.Join(f.curDir, "invoices_0046.csv.done")) {
		t.Fatal("the marker itself must never be delivered")
	}
	if exists(filepath.Join(f.srcDir, "invoices_0046.csv")) || exists(filepath.Join(f.srcDir, "invoices_0046.csv.done")) {
		t.Fatal("both the body and the marker must be removed from the inbox (delete)")
	}
	manifests, err := store.NewManifestStore(f.dataDir).List()
	if err != nil || len(manifests) != 1 {
		t.Fatalf("marker must yield exactly one message (not the marker), got %d err=%v", len(manifests), err)
	}
}

func TestRunCycle_push受信モードのmarkerかつcopyの場合_二重契機でも重複配信しないこと(t *testing.T) {
	// Arrange
	f := newPushFixture(t, config.CompletionMarker, ".done", config.HandlingCopy, 60, 9093)
	clock := time.Date(2026, 6, 17, 2, 6, 37, 0, time.UTC)
	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return clock }
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0048.csv"), []byte("id,amount\n1,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0048.csv.done"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act (1 回目: 収集・配信。copy なので本体とマーカーは残置)
	pipe.RunCycle(context.Background())
	if !exists(filepath.Join(f.srcDir, "invoices_0048.csv")) || !exists(filepath.Join(f.srcDir, "invoices_0048.csv.done")) {
		t.Fatal("copy handling must keep the body and the marker in the inbox")
	}
	// コンシューマーが引き取った状態を模す
	if err := os.Remove(filepath.Join(f.curDir, "invoices_0048.csv")); err != nil {
		t.Fatal(err)
	}

	// Act (2 回目: 残ったまま再契機)
	clock = clock.Add(time.Minute)
	pipe.RunCycle(context.Background())

	// Assert
	if exists(filepath.Join(f.curDir, "invoices_0048.csv")) {
		t.Fatal("a processed file must not be re-collected nor re-delivered")
	}
	manifests, err := store.NewManifestStore(f.dataDir).List()
	if err != nil || len(manifests) != 1 {
		t.Fatalf("re-trigger created extra manifests: %d err=%v", len(manifests), err)
	}
}

func TestDaemonRun_push受信モードでファイル投入時_イベント駆動でポーリングを待たず配信されること(t *testing.T) {
	// Arrange (polling/fallback を 60s と長くし、5s 以内の配信は fsnotify 由来であることを保証する)
	port := freePort(t)
	f := newPushFixture(t, config.CompletionRename, ".tmp", config.HandlingDelete, 60, port)
	lg := logging.New(io.Discard)
	metrics := metricsreg.New()
	pipe := usecase.NewPipeline(f.cfg, lg, metrics)
	daemon := runtime.New(f.cfg, pipe, lg, metrics, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	waitFor(t, 10*time.Second, "healthz 200", func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	// Act (受信ディレクトリへ正式名で put)
	if err := os.WriteFile(filepath.Join(f.srcDir, "invoices_0050.csv"), []byte("id,amount\n1,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Assert (polling 60s を待たず数秒で配信される = イベント駆動)
	waitFor(t, 5*time.Second, "event-driven delivery before the polling interval", func() bool {
		return exists(filepath.Join(f.curDir, "invoices_0050.csv"))
	})

	// Cleanup
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon.Run = %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after the stop signal")
	}
}
