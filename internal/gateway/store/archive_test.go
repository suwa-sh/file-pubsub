package store

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestArchiveStore_PutWorkAndPromote(t *testing.T) {
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	id := "20260612T093001_orders_sales.csv"

	if err := s.PutWork("orders", id, strings.NewReader("payload")); err != nil {
		t.Fatalf("PutWork: %v", err)
	}
	if _, err := os.Stat(s.WorkPath("orders", id)); err != nil {
		t.Fatalf("work file missing: %v", err)
	}

	if err := s.Promote("orders", id); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if _, err := os.Stat(s.WorkPath("orders", id)); !os.IsNotExist(err) {
		t.Error("work file must be removed after promote")
	}
	r, err := s.Open("orders", id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	if string(data) != "payload" {
		t.Errorf("archive content = %q", data)
	}
	ok, err := s.Exists("orders", id)
	if err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}
}

func TestArchiveStore_PromoteIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	id := "m1"
	if err := s.PutWork("orders", id, strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote("orders", id); err != nil {
		t.Fatal(err)
	}
	// Re-run after interruption: same work content again, promote overwrites the same path.
	if err := s.PutWork("orders", id, strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote("orders", id); err != nil {
		t.Fatalf("idempotent promote: %v", err)
	}
}

func TestArchiveStore_ListAndDelete_Retention(t *testing.T) {
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)

	for _, id := range []string{"20260101T000000_orders_a.csv", "20260612T000000_orders_b.csv"} {
		if err := s.PutWork("orders", id, strings.NewReader(id)); err != nil {
			t.Fatal(err)
		}
		if err := s.Promote("orders", id); err != nil {
			t.Fatal(err)
		}
	}
	// Leftover temp file must not appear in the scan.
	if err := os.WriteFile(filepath.Join(dataDir, "archive", "orders", "x.tmp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListMessageIDs("orders")
	if err != nil {
		t.Fatalf("ListMessageIDs: %v", err)
	}
	want := []string{"20260101T000000_orders_a.csv", "20260612T000000_orders_b.csv"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}

	// Retention decision (domain) + deletion (store): only the expired file goes.
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	savedAtOld := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	savedAtNew := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	if !domain.IsExpired(domain.RetentionDeadline(savedAtOld, 90), now) {
		t.Fatal("old file must be expired")
	}
	if domain.IsExpired(domain.RetentionDeadline(savedAtNew, 90), now) {
		t.Fatal("new file must be kept")
	}
	if err := s.Delete("orders", want[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ids, _ = s.ListMessageIDs("orders")
	if !reflect.DeepEqual(ids, []string{want[1]}) {
		t.Errorf("after delete ids = %v, want only %v", ids, want[1])
	}
}

func TestArchiveStore_DeleteMissingIsNoError(t *testing.T) {
	s := NewArchiveStore(t.TempDir())
	if err := s.Delete("orders", "nope"); err != nil {
		t.Errorf("deleting a missing archive must be idempotent: %v", err)
	}
}

func TestArchiveStore_ListMissingTopic(t *testing.T) {
	s := NewArchiveStore(t.TempDir())
	ids, err := s.ListMessageIDs("nope")
	if err != nil || ids != nil {
		t.Errorf("missing topic: got %v, %v", ids, err)
	}
}

func TestArchiveStore_CleanupWorkTempFiles(t *testing.T) {
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	if err := s.PutWork("orders", "keep", strings.NewReader("k")); err != nil {
		t.Fatal(err)
	}
	tmp := s.WorkPath("orders", "broken") + ".tmp"
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupWorkTempFiles("orders"); err != nil {
		t.Fatalf("CleanupWorkTempFiles: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("leftover temp file must be removed")
	}
	if _, err := os.Stat(s.WorkPath("orders", "keep")); err != nil {
		t.Error("final-name work file must be kept")
	}
}
