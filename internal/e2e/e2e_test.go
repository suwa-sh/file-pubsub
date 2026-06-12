// Package e2e exercises the whole pipeline and the resident daemon over real
// files: golden path (collect → archive → fan-out → manifest), idempotent
// re-run, graceful shutdown and duplicate-start prevention.
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

func TestGoldenPathCycle(t *testing.T) {
	f := newFixture(t, 60, 10, 9090)
	clock := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	pipe := usecase.NewPipeline(f.cfg, logging.New(io.Discard), nil)
	pipe.Now = func() time.Time { return clock }

	if err := os.WriteFile(filepath.Join(f.srcDir, "orders_1.csv"), []byte("a,b,c"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pipe.RunCycle(ctx) // first sighting: stability carry-over
	clock = clock.Add(11 * time.Second)
	pipe.RunCycle(ctx) // stable: collect → archive → fan-out

	// Both subscriptions received the file under the original name.
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

	// The archive remains and the manifest is delivered.
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
	// The original was collected and deleted (delete handling).
	if exists(filepath.Join(f.srcDir, "orders_1.csv")) {
		t.Fatal("original file must be deleted from the source")
	}

	// Re-run: no double delivery, no re-collection.
	for _, dir := range []string{f.curDir, f.nextDir} {
		if err := os.Remove(filepath.Join(dir, "orders_1.csv")); err != nil {
			t.Fatal(err)
		}
	}
	clock = clock.Add(time.Minute)
	pipe.RunCycle(ctx)
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

// freePort reserves an ephemeral TCP port for the daemon test.
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

func TestDaemonGracefulShutdown(t *testing.T) {
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

	// A second daemon on the same data dir is a duplicate start.
	second := runtime.New(f.cfg, pipe, lg, metrics, io.Discard)
	if err := second.Run(context.Background()); !errors.Is(err, store.ErrAlreadyLocked) {
		t.Fatalf("second daemon: err = %v, want ErrAlreadyLocked", err)
	}

	// Drop a file and wait for the daemon to deliver it everywhere.
	if err := os.WriteFile(filepath.Join(f.srcDir, "live.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "delivery to both subscriptions", func() bool {
		return exists(filepath.Join(f.curDir, "live.csv")) && exists(filepath.Join(f.nextDir, "live.csv"))
	})

	// The metrics endpoint exposes the per-topic counters while running.
	waitFor(t, 10*time.Second, "processed_total metric", func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return err == nil && strings.Contains(string(body), `file_pubsub_processed_total{topic="orders"} 1`)
	})

	// Stop signal → graceful shutdown: clean exit, lock released, HTTP down.
	cancel()
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

	// The manifest recorded the delivery before the daemon stopped.
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
