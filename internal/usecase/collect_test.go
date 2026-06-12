package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
)

func TestCollect_新規ファイルを初回観測した場合_持ち越され2回目のサイクルで収集されること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "a,b")

	// Act: 初回観測 (持ち越しのみ)
	e.p.Collect(context.Background())

	// Assert: まだ収集されない
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("first cycle must not collect, got %d manifests", got)
	}
	if !fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("source file must remain after the first cycle")
	}

	// Act: 安定判定間隔の経過後にもう 1 サイクル
	e.clock.Advance(11 * time.Second)
	e.p.Collect(context.Background())

	// Assert: 収集されアーカイブまで到達する
	m := e.singleManifest()
	if m.Status != domain.StatusArchived {
		t.Fatalf("status = %s, want archived", m.Status)
	}
	if m.OriginalFileName != "orders_1.csv" || m.Topic != "orders" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.SavedAt == nil || m.RetentionDeadline == nil {
		t.Fatal("saved_at / retention_deadline must be set")
	}
	if want := m.SavedAt.AddDate(0, 0, 90); !m.RetentionDeadline.Equal(want) {
		t.Fatalf("retention_deadline = %v, want %v", m.RetentionDeadline, want)
	}
	if ok, _ := e.p.Archive.Exists("orders", m.MessageID); !ok {
		t.Fatal("archive file must exist")
	}
	if fileExists(t, e.p.Archive.WorkPath("orders", m.MessageID)) {
		t.Fatal("work file must be removed after promotion")
	}
	// delete ハンドリング: アーカイブ成功後に原本が削除される。
	if fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("original file must be deleted")
	}
}

func TestCollect_ファイルが書き込み途中で変化した場合_安定するまで収集されないこと(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("grow.csv", "1")

	// Act: 初回観測後にサイズが変化した状態でサイクルを回す
	e.p.Collect(context.Background())
	e.clock.Advance(11 * time.Second)
	e.writeSource("grow.csv", "12") // 書き込み途中: サイズが変化
	e.p.Collect(context.Background())

	// Assert: 変化したファイルは収集されない
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("changed file must not be collected, got %d manifests", got)
	}

	// Act: 変化が止まってから安定判定間隔を経過させる
	e.clock.Advance(11 * time.Second)
	e.p.Collect(context.Background())

	// Assert: 安定したファイルは収集される
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("stabilized file must be collected, got %d manifests", got)
	}
}

func TestCollect_除外パターンとtmpファイルの場合_収集されないこと(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("a.tmp", "x")
	e.writeSource("b.skip", "x")

	// Act
	e.collectStable()

	// Assert
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("excluded files must not be collected, got %d manifests", got)
	}
}

func TestCollect_copyハンドリングの場合_原本が残り再収集されないこと(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingCopy)
	e.writeSource("customers.csv", "c")

	// Act
	e.collectStable()

	// Assert: アーカイブ済みで原本が残り、処理済み記録が再収集を防ぐ
	m := e.singleManifest()
	if m.Status != domain.StatusArchived {
		t.Fatalf("status = %s, want archived", m.Status)
	}
	if !fileExists(t, filepath.Join(e.srcDir, "customers.csv")) {
		t.Fatal("original must remain in copy mode")
	}
	info, err := os.Stat(filepath.Join(e.srcDir, "customers.csv"))
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.p.Processed.IsProcessed("orders", "customers.csv", info.ModTime(), info.Size())
	if err != nil || !done {
		t.Fatalf("processed record missing: done=%v err=%v", done, err)
	}

	// Act: 同じファイルのまま追加のサイクルを回す
	e.collectStable()
	e.collectStable()

	// Assert: 再収集されない
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("re-collection must be prevented, got %d manifests", got)
	}
}

// TestCollect_copyハンドリングで同名ファイルが変化した場合_新メッセージとして再収集されること は
// processed キーを守るテスト: mtime またはサイズが変わった同名の再出力は再収集
// されなければならず (旧来の名前のみのキーは永遠にスキップしていた)、
// 変化のないファイル (同名 + 同 mtime + 同サイズ) はスキップされたままになる。
func TestCollect_copyハンドリングで同名ファイルが変化した場合_新メッセージとして再収集されること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingCopy)
	e.writeSource("customers.csv", "v1")
	e.collectStable()
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("manifests = %d, want 1", got)
	}

	// Act: 同名のままサイズを変化させる
	e.writeSource("customers.csv", "v1+more")
	e.collectStable()

	// Assert: 新メッセージとして再収集される
	if got := len(e.manifests()); got != 2 {
		t.Fatalf("size change must be re-collected, got %d manifests", got)
	}

	// Act: サイズは同一のまま mtime を変化させる
	src := filepath.Join(e.srcDir, "customers.csv")
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	bumped := info.ModTime().Add(time.Hour)
	if err := os.Chtimes(src, bumped, bumped); err != nil {
		t.Fatal(err)
	}
	e.collectStable()

	// Assert: 新メッセージとして再収集される
	if got := len(e.manifests()); got != 3 {
		t.Fatalf("mtime change must be re-collected, got %d manifests", got)
	}

	// Act: 変化なし (同名 + 同 mtime + 同サイズ) のままサイクルを回す
	e.collectStable()
	e.collectStable()

	// Assert: スキップされたまま
	if got := len(e.manifests()); got != 3 {
		t.Fatalf("unchanged file must stay skipped, got %d manifests", got)
	}
}

// TestCollect_deleteハンドリングで同名同mtimeのファイルが再出現した場合_新メッセージとして収集されること は
// 旧来の mtime ヒューリスティックへの回帰を防ぐ: 記録済み collected_at より
// 古い mtime の同名ファイルを「削除の残骸」とみなし取得せず削除していたため、
// mtime を保存するプロデューサーの再出力 (cp -p) が黙って失われていた。
// delete ハンドリングはソースに存在するファイルを常に新メッセージとして
// 収集しなければならない (at-least-once)。収集せず削除するパスは存在しない。
func TestCollect_deleteハンドリングで同名同mtimeのファイルが再出現した場合_新メッセージとして収集されること(t *testing.T) {
	// Arrange: 1 回目の収集後、同一内容かつ collected_at より古い mtime で再出現させる
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "a")
	e.collectStable()
	first := e.singleManifest()

	e.writeSource("orders_1.csv", "a")
	old := first.CollectedAt.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(e.srcDir, "orders_1.csv"), old, old); err != nil {
		t.Fatal(err)
	}

	// Act
	e.collectStable()

	// Assert: 新しい message_id で収集・アーカイブされ、原本は削除される
	ms := e.manifests()
	if len(ms) != 2 {
		t.Fatalf("the reappeared file must be collected as a new message, got %d manifests", len(ms))
	}
	var second *store.Manifest
	for _, m := range ms {
		if m.MessageID != first.MessageID {
			second = m
		}
	}
	if second == nil {
		t.Fatal("the second collection must get a new message_id")
	}
	if second.Status != domain.StatusArchived {
		t.Fatalf("second status = %s, want archived", second.Status)
	}
	if ok, _ := e.p.Archive.Exists("orders", second.MessageID); !ok {
		t.Fatal("the re-collected payload must be archived, not just deleted")
	}
	if fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("the original must be deleted after the archive save")
	}
}

func TestCollect_collected状態で中断していた場合_再開時にアーカイブまで昇格されること(t *testing.T) {
	// Arrange: 中断された実行を再現する (work ファイル + collected マニフェスト、アーカイブ未作成)
	e := newEnv(t, config.HandlingDelete)
	msg := domain.NewMessage(e.clock.Now(), "orders", "stuck.csv")
	if err := e.p.Archive.PutWork("orders", msg.MessageID, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := e.p.Manifests.Put(store.NewManifest(msg)); err != nil {
		t.Fatal(err)
	}

	// Act
	e.p.Collect(context.Background())

	// Assert
	m := e.singleManifest()
	if m.Status != domain.StatusArchived {
		t.Fatalf("status = %s, want archived after resume", m.Status)
	}
	if ok, _ := e.p.Archive.Exists("orders", msg.MessageID); !ok {
		t.Fatal("archive file must exist after resume")
	}
}
