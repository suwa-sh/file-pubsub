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
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// newInboxEnv は t.TempDir 上に push 受信モード(inbox)のトピック "invoices" 1 件
// (完了検知 mode/suffix・サブスクリプション current) のパイプラインを構築する。
func newInboxEnv(t *testing.T, mode, suffix, handling string) *testEnv {
	t.Helper()
	base := t.TempDir()
	srcDir := filepath.Join(base, "inbox")
	dataDir := filepath.Join(base, "data")
	cur := filepath.Join(base, "subs", "current")
	for _, d := range []string{srcDir, dataDir, cur} {
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
			Name: "invoices",
			Source: config.Source{
				Type:                 config.SourceTypeInbox,
				Directory:            srcDir,
				OriginalFileHandling: handling,
				Completion:           config.Completion{Mode: mode, Suffix: suffix},
				StabilityCheck:       config.StabilityCheck{Interval: 10},
			},
			Subscriptions: []config.Subscription{
				{Name: "current", Directory: cur},
			},
		}},
	}
	clock := &fakeClock{t: time.Date(2026, 6, 17, 2, 6, 37, 0, time.UTC)}
	p := NewPipeline(cfg, logging.New(io.Discard), nil)
	p.Now = clock.Now
	return &testEnv{
		t: t, p: p, clock: clock,
		srcDir: srcDir, dataDir: dataDir,
		subDirs: map[string]string{"current": cur},
	}
}

func (e *testEnv) srcExists(name string) bool {
	return fileExists(e.t, filepath.Join(e.srcDir, name))
}

func TestCollect_inboxのstability方式の場合_安定後に収集されること(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionStability, "", config.HandlingDelete)
	e.writeSource("invoices_0042.csv", "id,amount\n1,100\n")

	// Act
	e.collectStable()

	// Assert
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("stabilized inbox file must be collected, got %d manifests", got)
	}
	if e.srcExists("invoices_0042.csv") {
		t.Errorf("delete handling must remove the source file after archive")
	}
}

func TestCollect_inboxのrename方式の場合_一時名は収集せず正式名を安定待ちなしで収集すること(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionRename, ".tmp", config.HandlingDelete)
	e.writeSource("invoices_0045.csv.tmp", "writing...")
	e.writeSource("invoices_0045.csv", "id,amount\n1,100\n")

	// Act (安定待ちなしで 1 サイクルで収集される)
	e.p.Collect(context.Background())

	// Assert
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("rename: final name must be collected in one cycle, got %d manifests", got)
	}
	if e.srcExists("invoices_0045.csv") {
		t.Errorf("rename: collected final file must be removed (delete)")
	}
	if !e.srcExists("invoices_0045.csv.tmp") {
		t.Errorf("rename: temp name must not be collected nor removed")
	}
}

func TestCollect_inboxのrename方式でカスタムsuffixの場合_partを一時名として扱うこと(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionRename, ".part", config.HandlingDelete)
	e.writeSource("invoices.csv.part", "writing...")
	e.writeSource("invoices.csv", "done")

	// Act
	e.p.Collect(context.Background())

	// Assert
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("rename(.part): final name must be collected, got %d manifests", got)
	}
	if !e.srcExists("invoices.csv.part") {
		t.Errorf("rename(.part): .part temp name must not be collected")
	}
}

func TestCollect_inboxのmarker方式の場合_マーカー出現で本体を収集し本体とマーカーを後始末すること(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionMarker, ".done", config.HandlingDelete)
	e.writeSource("invoices_0046.csv", "id,amount\n1,100\n")
	e.writeSource("invoices_0046.csv.done", "")

	// Act
	e.p.Collect(context.Background())

	// Assert
	m := e.singleManifest()
	if strings.Contains(m.MessageID, ".done") {
		t.Errorf("marker itself must not be collected as a message: %s", m.MessageID)
	}
	if e.srcExists("invoices_0046.csv") || e.srcExists("invoices_0046.csv.done") {
		t.Errorf("marker(delete): both the body and the marker must be removed after archive")
	}
}

func TestCollect_inboxのmarker方式でマーカー未着の場合_本体を収集しないこと(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionMarker, ".done", config.HandlingDelete)
	e.writeSource("invoices_0046.csv", "id,amount\n1,100\n")

	// Act
	e.p.Collect(context.Background())

	// Assert
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("marker: body without its marker must not be collected, got %d manifests", got)
	}
	if !e.srcExists("invoices_0046.csv") {
		t.Errorf("marker: uncollected body must remain in the inbox")
	}
}

func TestCollect_inboxのmarker方式でカスタムsuffixの場合_okマーカーで判定すること(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionMarker, ".ok", config.HandlingDelete)
	e.writeSource("invoices_0046.csv", "id,amount\n1,100\n")
	e.writeSource("invoices_0046.csv.ok", "")

	// Act
	e.p.Collect(context.Background())

	// Assert
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("marker(.ok): body must be collected on .ok marker, got %d manifests", got)
	}
	if e.srcExists("invoices_0046.csv") || e.srcExists("invoices_0046.csv.ok") {
		t.Errorf("marker(.ok, delete): both body and .ok marker must be removed")
	}
}

func TestCollect_inboxで同名ファイルを再出力した場合_別メッセージとして収集されること(t *testing.T) {
	// Arrange (rename 方式・delete: 収集→回収後に同名を再 put)
	e := newInboxEnv(t, config.CompletionRename, ".tmp", config.HandlingDelete)
	e.writeSource("invoices_0042.csv", "id,amount\n1,100\n")

	// Act (1 回目)
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("first collect must yield 1 manifest, got %d", got)
	}
	// 別時刻で同名ファイルを再出力
	e.clock.Advance(1 * time.Minute)
	e.writeSource("invoices_0042.csv", "id,amount\n2,200\n")

	// Act (2 回目)
	e.p.Collect(context.Background())

	// Assert (別 message_id の 2 件になり履歴が失われない)
	ms := e.manifests()
	if len(ms) != 2 {
		t.Fatalf("re-emitted same-name file must become a new message, got %d manifests", len(ms))
	}
	if ms[0].MessageID == ms[1].MessageID {
		t.Errorf("re-emit must get a distinct message_id, got duplicate %s", ms[0].MessageID)
	}
}

func TestCollect_inboxで同一収集秒に同名ファイルを再収集した場合_連番付きの別message_idになること(t *testing.T) {
	// Arrange (fakeClock を進めず同一秒で 2 回収集する。rename・delete)
	e := newInboxEnv(t, config.CompletionRename, ".tmp", config.HandlingDelete)
	e.writeSource("invoices_0042.csv", "v1")

	// Act (1 回目: 収集→回収。クロックは据え置きのまま同名を再 put)
	e.p.Collect(context.Background())
	e.writeSource("invoices_0042.csv", "v2-changed")
	e.p.Collect(context.Background())

	// Assert (同一秒でも連番付与で別 message_id・2 件。先行の上書きが起きない)
	ms := e.manifests()
	if len(ms) != 2 {
		t.Fatalf("same-second re-collect must not overwrite history, got %d manifests", len(ms))
	}
	if ms[0].MessageID == ms[1].MessageID {
		t.Errorf("same-second re-collect must get distinct message_id, got duplicate %s", ms[0].MessageID)
	}
}

func TestCollect_inboxで同一収集秒に同名ファイルを3回収集した場合_すべて別message_idになること(t *testing.T) {
	// Arrange (fakeClock を進めず同一秒で 3 回収集する)
	e := newInboxEnv(t, config.CompletionRename, ".tmp", config.HandlingDelete)

	// Act (収集→回収→再 put を同一秒で 3 回)
	for i := 0; i < 3; i++ {
		e.writeSource("invoices_0042.csv", "v")
		e.p.Collect(context.Background())
	}

	// Assert (3 件すべて別 message_id。連番 _2 / _3 で一意化される)
	ids := map[string]bool{}
	for _, m := range e.manifests() {
		ids[m.MessageID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("three same-second re-collects must yield 3 distinct message_id, got %d", len(ids))
	}
}

func TestCollect_inboxのmarkerかつcopyで残存マーカーがある場合_新マーカーまで再putを取り込まないこと(t *testing.T) {
	// Arrange (marker・copy: 本体とマーカーを残置する)
	e := newInboxEnv(t, config.CompletionMarker, ".done", config.HandlingCopy)
	e.writeSource("invoices_0048.csv", "v1")
	e.writeSource("invoices_0048.csv.done", "")

	// Act (1 回目: v1 を収集。本体とマーカーは残置・処理済み記録)
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("first collect must yield 1 manifest, got %d", got)
	}
	// 同名で内容の異なる本体を再 put(新しいマーカーはまだ置かない)
	e.clock.Advance(1 * time.Minute)
	e.writeSource("invoices_0048.csv", "v2-different-content")
	e.p.Collect(context.Background())

	// Assert (残存する処理済みマーカーでは再 put を取り込まない)
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("a processed leftover marker must not re-trigger the re-put body, got %d", got)
	}

	// Act (Producer が新しいマーカーを置く)
	e.clock.Advance(1 * time.Minute)
	e.writeSource("invoices_0048.csv.done", "fresh")
	e.p.Collect(context.Background())

	// Assert (新しい未処理マーカーで再 put 本体が取り込まれる)
	if got := len(e.manifests()); got != 2 {
		t.Fatalf("a fresh marker must trigger collection of the re-put body, got %d", got)
	}
}

func TestCollect_inboxのmarker方式でcopy設定の場合_処理済み本体を再収集しないこと(t *testing.T) {
	// Arrange
	e := newInboxEnv(t, config.CompletionMarker, ".done", config.HandlingCopy)
	e.writeSource("invoices_0048.csv", "id,amount\n1,100\n")
	e.writeSource("invoices_0048.csv.done", "")

	// Act (1 回目で収集、本体とマーカーは残置)
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("marker(copy): first collect must yield 1 manifest, got %d", got)
	}
	if !e.srcExists("invoices_0048.csv") || !e.srcExists("invoices_0048.csv.done") {
		t.Fatalf("marker(copy): body and marker must remain after copy")
	}
	// Act (2 回目: 残ったまま再契機)
	e.p.Collect(context.Background())

	// Assert
	if got := len(e.manifests()); got != 1 {
		t.Errorf("marker(copy): processed body must not be re-collected, got %d manifests", got)
	}
}
