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
	info, err := os.Stat(filepath.Join(e.srcDir, "customers.csv"))
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.p.Processed.IsProcessed("orders", "customers.csv", info.ModTime(), info.Size())
	if err != nil || !done {
		t.Fatalf("processed record missing: done=%v err=%v", done, err)
	}

	e.collectStable()
	e.collectStable()
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("re-collection must be prevented, got %d manifests", got)
	}
}

// TestCollectCopyModeRecollectsChangedFile guards the processed key: a
// same-name re-output whose mtime or size changed must be re-collected (the
// old name-only key skipped it forever), while an unchanged file (same name +
// mtime + size) stays skipped.
func TestCollectCopyModeRecollectsChangedFile(t *testing.T) {
	e := newEnv(t, config.HandlingCopy)
	e.writeSource("customers.csv", "v1")
	e.collectStable()
	if got := len(e.manifests()); got != 1 {
		t.Fatalf("manifests = %d, want 1", got)
	}

	// Size change under the same name: re-collected as a new message.
	e.writeSource("customers.csv", "v1+more")
	e.collectStable()
	if got := len(e.manifests()); got != 2 {
		t.Fatalf("size change must be re-collected, got %d manifests", got)
	}

	// mtime change with identical size: re-collected as a new message.
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
	if got := len(e.manifests()); got != 3 {
		t.Fatalf("mtime change must be re-collected, got %d manifests", got)
	}

	// Unchanged (same name + mtime + size): skipped.
	e.collectStable()
	e.collectStable()
	if got := len(e.manifests()); got != 3 {
		t.Fatalf("unchanged file must stay skipped, got %d manifests", got)
	}
}

// TestCollectDeleteModeSameNameSameMtimeRecollected guards against the old
// mtime heuristic that treated a same-name file whose mtime predates the
// recorded collected_at as a "delete leftover" and removed it without
// fetching: a producer re-output that preserves mtime (cp -p) was silently
// lost. Delete handling must always collect a present source file as a new
// message (at-least-once); there is no delete-without-collect path.
func TestCollectDeleteModeSameNameSameMtimeRecollected(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "a")
	e.collectStable()
	first := e.singleManifest()

	// The same file reappears with identical content and an mtime before the
	// recorded collected_at (producer re-output with preserved mtime).
	e.writeSource("orders_1.csv", "a")
	old := first.CollectedAt.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(e.srcDir, "orders_1.csv"), old, old); err != nil {
		t.Fatal(err)
	}

	e.collectStable()
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
