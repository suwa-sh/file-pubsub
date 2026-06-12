package usecase

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }
func (c *fakeClock) Set(t time.Time)         { c.t = t }

type testEnv struct {
	t       *testing.T
	p       *Pipeline
	clock   *fakeClock
	srcDir  string
	dataDir string
	subDirs map[string]string // サブスクリプション名 -> ディレクトリ
}

// newEnv は t.TempDir 上にトピック "orders" 1 件 (local ソース、
// サブスクリプション current / next、安定判定間隔 10 秒) のパイプラインを構築する。
func newEnv(t *testing.T, handling string) *testEnv {
	t.Helper()
	base := t.TempDir()
	srcDir := filepath.Join(base, "src")
	dataDir := filepath.Join(base, "data")
	cur := filepath.Join(base, "subs", "current")
	next := filepath.Join(base, "subs", "next")
	for _, d := range []string{srcDir, dataDir, cur, next} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		PollingInterval:  60,
		ArchiveRetention: 90,
		RetryMaxCount:    2,
		MetricsPort:      9090,
		DataDir:          dataDir,
		Topics: []config.Topic{{
			Name: "orders",
			Source: config.Source{
				Type:                 config.SourceTypeLocal,
				Directory:            srcDir,
				OriginalFileHandling: handling,
				StabilityCheck:       config.StabilityCheck{Interval: 10},
				ExcludePatterns:      []string{"*.skip"},
			},
			Subscriptions: []config.Subscription{
				{Name: "current", Directory: cur},
				{Name: "next", Directory: next},
			},
		}},
	}
	clock := &fakeClock{t: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)}
	p := NewPipeline(cfg, logging.New(io.Discard), nil)
	p.Now = clock.Now
	return &testEnv{
		t: t, p: p, clock: clock,
		srcDir: srcDir, dataDir: dataDir,
		subDirs: map[string]string{"current": cur, "next": next},
	}
}

func (e *testEnv) writeSource(name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.srcDir, name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// collectStable は安定判定間隔を挟んで収集パスを 2 回実行し、書き込んだ直後の
// ソースファイルが安定と観測されて収集されるようにする。
func (e *testEnv) collectStable() {
	e.t.Helper()
	e.p.Collect(context.Background())
	e.clock.Advance(11 * time.Second)
	e.p.Collect(context.Background())
}

func (e *testEnv) manifests() []*store.Manifest {
	e.t.Helper()
	ms, err := e.p.Manifests.List()
	if err != nil {
		e.t.Fatal(err)
	}
	return ms
}

func (e *testEnv) singleManifest() *store.Manifest {
	e.t.Helper()
	ms := e.manifests()
	if len(ms) != 1 {
		e.t.Fatalf("expected 1 manifest, got %d", len(ms))
	}
	return ms[0]
}

// seedArchived は collect ステージを経由せず、archived 状態のメッセージ
// (アーカイブファイル + マニフェスト) を直接植え付ける。
func (e *testEnv) seedArchived(name, content string) *store.Manifest {
	e.t.Helper()
	msg := domain.NewMessage(e.clock.Now(), "orders", name)
	if err := store.WriteFileAtomic(e.p.Archive.ArchivePath("orders", msg.MessageID), strings.NewReader(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	m := store.NewManifest(msg)
	if err := e.p.Manifests.Put(m); err != nil {
		e.t.Fatal(err)
	}
	if err := e.p.finalizeArchive(m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// breakSubscription はサブスクリプションディレクトリを通常ファイルの配下に
// 向け、そこへの配置がすべて失敗するようにする (ファイルパスへの MkdirAll)。
func (e *testEnv) breakSubscription(name string) {
	e.t.Helper()
	blocker := filepath.Join(e.t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.setSubscriptionDir(name, filepath.Join(blocker, "sub"))
}

func (e *testEnv) setSubscriptionDir(name, dir string) {
	e.t.Helper()
	t := e.p.findTopic("orders")
	sub := findSubscription(t, name)
	if sub == nil {
		e.t.Fatalf("subscription %s not found", name)
	}
	sub.Directory = dir
	e.subDirs[name] = dir
}

func (e *testEnv) subFile(sub, name string) string {
	return filepath.Join(e.subDirs[sub], name)
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatal(err)
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func subState(t *testing.T, m *store.Manifest, name string) store.SubscriptionDelivery {
	t.Helper()
	for _, s := range m.Subscriptions {
		if s.Subscription == name {
			return s
		}
	}
	t.Fatalf("subscription %s has no record in manifest %s", name, m.MessageID)
	return store.SubscriptionDelivery{}
}
