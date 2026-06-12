// Package runtime hosts the resident daemon: lock acquisition (with stale
// recovery), the embedded HTTP observability server (/healthz, /metrics),
// the polling scheduler and graceful shutdown (SR-006, SR-007, LR-001).
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

// Daemon is the serve subcommand body.
type Daemon struct {
	Cfg      *config.Config
	Pipeline *usecase.Pipeline
	Log      *logging.Logger
	Lock     *store.LockManager
	Metrics  *metricsreg.Registry
	Stdout   io.Writer
}

// New builds a daemon over the pipeline. stdout receives only the startup
// message; everything after goes to the structured log (ui-design.md serve).
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

// Run acquires the lock, starts the HTTP server, then polls until ctx is
// cancelled (the stop signal). A running cycle is never interrupted: the
// cancellation is observed between cycles, then the HTTP server stops and the
// lock is released (graceful shutdown, exit code 0 at the caller).
// store.ErrAlreadyLocked means a duplicate start (exit code 3 at the caller).
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.Lock.Acquire(os.Getpid(), time.Now()); err != nil {
		return err
	}
	defer func() {
		if err := d.Lock.Release(); err != nil {
			d.Log.Emit(logging.Event{EventType: "shutdown_failed", ErrorDetail: fmt.Sprintf("%v. the leftover lock is recovered as stale on the next start", err)})
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", d.Metrics.Handler())
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", d.Cfg.MetricsPort))
	if err != nil {
		return fmt.Errorf("start http server on metrics_port %d failed: %w. set metrics_port to a free port", d.Cfg.MetricsPort, err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.Log.Emit(logging.Event{EventType: "http_server_failed", ErrorDetail: fmt.Sprintf("%v. restart the daemon after checking metrics_port", err)})
		}
	}()

	subs := 0
	for _, t := range d.Cfg.Topics {
		subs += len(t.Subscriptions)
	}
	_, _ = fmt.Fprintf(d.Stdout, "file-pubsub serve: lock acquired (pid %d), topics=%d subscriptions=%d, metrics on :%d, polling every %ds\n",
		os.Getpid(), len(d.Cfg.Topics), subs, d.Cfg.MetricsPort, d.Cfg.PollingInterval)
	d.Log.Emit(logging.Event{EventType: "startup"})

	// A running cycle is completed even after the stop signal (SR-007).
	cycleCtx := context.WithoutCancel(ctx)
	ticker := time.NewTicker(time.Duration(d.Cfg.PollingInterval) * time.Second)
	defer ticker.Stop()
	d.Pipeline.RunCycle(cycleCtx)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			d.Pipeline.RunCycle(cycleCtx)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	d.Log.Emit(logging.Event{EventType: "shutdown"})
	return nil
}
