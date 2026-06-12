package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
)

func TestRetentionDeletesOnlyExpiredArchives(t *testing.T) {
	e := newEnv(t, config.HandlingDelete) // ArchiveRetention = 90 days
	old := e.seedArchived("old.csv", "old")
	e.clock.Advance(48 * time.Hour)
	young := e.seedArchived("young.csv", "young")

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
