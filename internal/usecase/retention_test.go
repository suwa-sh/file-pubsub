package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestRetention_期限切れと期限内のアーカイブが混在する場合_期限切れのみ削除されること(t *testing.T) {
	// Arrange (ArchiveRetention = 90 日)
	e := newEnv(t, config.HandlingDelete)
	old := e.seedArchived("old.csv", "old")
	e.clock.Advance(48 * time.Hour)
	young := e.seedArchived("young.csv", "young")
	e.p.Fanout(context.Background()) // 両方を delivered として決着させる

	// Act: 最初の保存から 91 日後 (期限切れは最初のアーカイブのみ)
	e.clock.Set(old.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	// Assert
	if ok, _ := e.p.Archive.Exists("orders", old.MessageID); ok {
		t.Fatal("expired archive must be deleted")
	}
	if ok, _ := e.p.Archive.Exists("orders", young.MessageID); !ok {
		t.Fatal("archive within the deadline must be kept")
	}
	// マニフェストの履歴はアーカイブ削除後も残る (CTR-003)。
	m, err := e.p.Manifests.Get(old.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageID != old.MessageID {
		t.Fatal("manifest must remain readable")
	}

	// Act & Assert: 冪等な再実行 (何もすることがなく、エラーも出ない)
	e.p.Retention(context.Background())
}

// TestRetention_未決着のメッセージが期限切れの場合_アーカイブが保持されること は
// 終端ステータスゲートを守るテスト: failed (あるいは delivering / retrying) の
// まま期限切れになったアーカイブは保持しなければならない。後続の retry・
// DLQ 隔離・replay が使える唯一のペイロードだからである。
func TestRetention_未決着のメッセージが期限切れの場合_アーカイブが保持されること(t *testing.T) {
	// Arrange: next の失敗でメッセージを failed として決着させる
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("precondition: status = %s, want failed", got.Status)
	}

	// Act
	e.clock.Set(m.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	// Assert
	if ok, _ := e.p.Archive.Exists("orders", m.MessageID); !ok {
		t.Fatal("an unresolved (failed) message must keep its archive past the deadline")
	}
}

// TestRetention_dlqメッセージが期限切れの場合_アーカイブが削除されDLQコピーは残ること:
// dlq が終端なのは Isolate が先にペイロードを dlq/ へコピーしているためで、
// アーカイブ本体は削除してよい。
func TestRetention_dlqメッセージが期限切れの場合_アーカイブが削除されDLQコピーは残ること(t *testing.T) {
	// Arrange: retry 上限超過で DLQ 隔離まで追い込む (RetryMaxCount = 2)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())
	for i := 0; i < 3; i++ {
		e.p.Retry(context.Background())
	}
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDLQ {
		t.Fatalf("precondition: status = %s, want dlq", got.Status)
	}
	if !fileExists(t, e.p.DLQ.FilePath("orders", m.MessageID)) {
		t.Fatal("precondition: the dlq copy of the payload must exist")
	}

	// Act
	e.clock.Set(m.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	// Assert
	if ok, _ := e.p.Archive.Exists("orders", m.MessageID); ok {
		t.Fatal("an expired dlq message must have its archive deleted")
	}
	if !fileExists(t, e.p.DLQ.FilePath("orders", m.MessageID)) {
		t.Fatal("the dlq payload copy must survive retention")
	}
}
