package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestRetentionDeletesOnlyExpiredArchives(t *testing.T) {
	e := newEnv(t, config.HandlingDelete) // ArchiveRetention = 90 days
	old := e.seedArchived("old.csv", "old")
	e.clock.Advance(48 * time.Hour)
	young := e.seedArchived("young.csv", "young")
	e.p.Fanout(context.Background()) // settle both as delivered

	// 91 days after the first save: only the first archive is expired.
	e.clock.Set(old.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	if ok, _ := e.p.Archive.Exists("orders", old.MessageID); ok {
		t.Fatal("expired archive must be deleted")
	}
	if ok, _ := e.p.Archive.Exists("orders", young.MessageID); !ok {
		t.Fatal("archive within the deadline must be kept")
	}

	// The manifest history survives the archive deletion (CTR-003).
	m, err := e.p.Manifests.Get(old.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageID != old.MessageID {
		t.Fatal("manifest must remain readable")
	}

	// Idempotent re-run: nothing to do, no error.
	e.p.Retention(context.Background())
}

// TestRetentionKeepsUnresolvedExpiredArchive guards the terminal-status gate:
// an expired archive whose message is still failed (or delivering / retrying)
// must be kept, because it is the only payload a later retry, DLQ isolation
// or replay can use.
func TestRetentionKeepsUnresolvedExpiredArchive(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background()) // next fails → message settles as failed

	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("precondition: status = %s, want failed", got.Status)
	}

	e.clock.Set(m.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	if ok, _ := e.p.Archive.Exists("orders", m.MessageID); !ok {
		t.Fatal("an unresolved (failed) message must keep its archive past the deadline")
	}
}

// TestRetentionDeletesExpiredDLQArchive: dlq is terminal because Isolate
// copies the payload into dlq/ first, so the archive body may be deleted.
func TestRetentionDeletesExpiredDLQArchive(t *testing.T) {
	e := newEnv(t, config.HandlingDelete) // RetryMaxCount = 2
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

	e.clock.Set(m.SavedAt.AddDate(0, 0, 91))
	e.p.Retention(context.Background())

	if ok, _ := e.p.Archive.Exists("orders", m.MessageID); ok {
		t.Fatal("an expired dlq message must have its archive deleted")
	}
	if !fileExists(t, e.p.DLQ.FilePath("orders", m.MessageID)) {
		t.Fatal("the dlq payload copy must survive retention")
	}
}
