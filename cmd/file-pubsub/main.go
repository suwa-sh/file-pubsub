// Command file-pubsub は、常駐デーモン (serve) と運用 CLI (status / replay /
// config validate) を提供するシングルバイナリである。終了コードは
// ui-design.md に従う: 0 成功、1 実行時エラー、2 設定・引数エラー、3 二重起動。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/runtime"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

const (
	exitOK        = 0
	exitRuntime   = 1
	exitUsage     = 2
	exitDuplicate = 3
)

const usageText = `usage: file-pubsub <command> [flags]

commands:
  serve            --config <path>
  status           --config <path> [--topic T] [--subscription S] [--status delivered|failed|dlq]
  replay           --config <path> --topic T (--from YYYY-MM-DD --to YYYY-MM-DD | --message-id ID) --subscription S
  config validate  --config <path>`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:], stdout, stderr)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "replay":
		return cmdReplay(args[1:], stdout, stderr)
	case "config":
		if len(args) >= 2 && args[1] == "validate" {
			return cmdConfigValidate(args[2:], stdout, stderr)
		}
		_, _ = fmt.Fprintln(stderr, `unknown config subcommand. use "config validate --config <path>"`)
		return exitUsage
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usageText)
		return exitUsage
	}
}

// loadConfig は --config を解決する。ここでの失敗はすべて終了コード 2 になる。
func loadConfig(path string, stderr io.Writer) (*config.Config, bool) {
	if path == "" {
		_, _ = fmt.Fprintln(stderr, "--config is required. specify the path of the single YAML config file")
		return nil, false
	}
	cfg, err := config.Load(path)
	if err != nil {
		var verrs config.ValidationErrors
		if errors.As(err, &verrs) {
			for _, v := range verrs {
				_, _ = fmt.Fprintln(stderr, v.Error())
			}
		} else {
			_, _ = fmt.Fprintf(stderr, "load config %s failed: %v. check the path and the YAML syntax\n", path, err)
		}
		return nil, false
	}
	return cfg, true
}

func cmdServe(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("serve", stderr)
	cfgPath := fs.String("config", "", "path of the config YAML")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitUsage
	}

	lg := logging.New(stdout)
	metrics := metricsreg.New()
	pipe := usecase.NewPipeline(cfg, lg, metrics)
	daemon := runtime.New(cfg, pipe, lg, metrics, stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx); err != nil {
		if errors.Is(err, store.ErrAlreadyLocked) {
			_, _ = fmt.Fprintf(stderr, "duplicate start: %v. the running daemon is untouched; stop it first if a restart is intended\n", err)
			return exitDuplicate
		}
		_, _ = fmt.Fprintln(stderr, err)
		return exitRuntime
	}
	return exitOK
}

func cmdConfigValidate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config validate", stderr)
	cfgPath := fs.String("config", "", "path of the config YAML")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitUsage
	}
	subs := 0
	for _, t := range cfg.Topics {
		subs += len(t.Subscriptions)
	}
	_, _ = fmt.Fprintf(stdout, "OK: topics=%d subscriptions=%d sources=%d\n", len(cfg.Topics), subs, len(cfg.Topics))
	return exitOK
}
