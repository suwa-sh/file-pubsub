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

func TestCollectStabilityCarryOver(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "a,b")

	// First sighting: carried over, nothing collected yet.
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("first cycle must not collect, got %d manifests", got)
	}
	if !fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("source file must remain after the first cycle")
	}

	// Second cycle after the stability interval: collected and archived.
	e.clock.Advance(11 * time.Second)
	e.p.Collect(context.Background())
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
	// delete handling: original removed after archive success.
	if fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("original file must be deleted")
	}
}

func TestCollectInstabilityResetsObservation(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("grow.csv", "1")

	e.p.Collect(context.Background())
	e.clock.Advance(11 * time.Second)
	e.writeSource("grow.csv", "12") // still being written: size changed
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("changed file must not be collected, got %d manifests", got)
	}

	e.clock.Advance(11 * time.Second)
	e.p.Collect(context.Background())
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("stabilized file must be collected, got %d manifests", got)
	}
}

func TestCollectExcludesPatternsAndTmp(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("a.tmp", "x")
	e.writeSource("b.skip", "x")
	e.collectStable()
	if got := len(e.manifests()); got != 0 {
		t.Fatalf("excluded files must not be collected, got %d manifests", got)
	}
}

func TestCollectCopyModeNoDuplicate(t *testing.T) {
	e := newEnv(t, config.HandlingCopy)
	e.writeSource("customers.csv", "c")
	e.collectStable()

	m := e.singleManifest()
	if m.Status != domain.StatusArchived {
		t.Fatalf("status = %s, want archived", m.Status)
	}
	// copy handling: original stays, processed record prevents re-collection.
	if !fileExists(t, filepath.Join(e.srcDir, "customers.csv")) {
		t.Fatal("original must remain in copy mode")
	}
	done, err := e.p.Processed.IsProcessed("orders", "customers.csv")
	if err != nil || !done {
		t.Fatalf("processed record missing: done=%v err=%v", done, err)
	}

	e.collectStable()
	e.collectStable()
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("re-collection must be prevented, got %d manifests", got)
	}
}

func TestCollectDeleteModeLeftoverOriginalNotRecollected(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "a")
	e.collectStable()
	m := e.singleManifest()

	// Simulate a failed original delete: the file reappears with its mtime
	// before the recorded collected_at.
	e.writeSource("orders_1.csv", "a")
	old := m.CollectedAt.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(e.srcDir, "orders_1.csv"), old, old); err != nil {
		t.Fatal(err)
	}

	e.collectStable()
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("leftover original must not become a new message, got %d manifests", got)
	}
	if fileExists(t, filepath.Join(e.srcDir, "orders_1.csv")) {
		t.Fatal("leftover original must be deleted on retry")
	}
}

func TestCollectResumePromotesCollectedManifest(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	// Interrupted run: work file + collected manifest, no archive yet.
	msg := domain.NewMessage(e.clock.Now(), "orders", "stuck.csv")
	if err := e.p.Archive.PutWork("orders", msg.MessageID, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := e.p.Manifests.Put(store.NewManifest(msg)); err != nil {
		t.Fatal(err)
	}

	e.p.Collect(context.Background())
	m := e.singleManifest()
	if m.Status != domain.StatusArchived {
		t.Fatalf("status = %s, want archived after resume", m.Status)
	}
	if ok, _ := e.p.Archive.Exists("orders", msg.MessageID); !ok {
		t.Fatal("archive file must exist after resume")
	}
}
