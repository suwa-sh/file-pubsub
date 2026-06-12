package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessedStore_MarkAndCheck(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.UTC)
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	ok, err := s.IsProcessed("customers", "customers_20260612.csv", mtime, 42)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("unrecorded file must be unprocessed")
	}

	if err := s.MarkProcessed("customers", "customers_20260612.csv", mtime, 42, at); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	ok, err = s.IsProcessed("customers", "customers_20260612.csv", mtime, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("recorded file must be processed")
	}

	// Other identifier in the same topic stays unprocessed.
	ok, _ = s.IsProcessed("customers", "customers_20260613.csv", mtime, 42)
	if ok {
		t.Error("different name must be unprocessed")
	}
}

// TestProcessedStore_KeyIncludesMtimeAndSize guards the processed key: the
// same file name with a different mtime or size is a different source file
// state and must not be reported as processed.
func TestProcessedStore_KeyIncludesMtimeAndSize(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Now().UTC()
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}

	if ok, _ := s.IsProcessed("customers", "a.csv", mtime.Add(time.Second), 42); ok {
		t.Error("same name with a different mtime must be unprocessed")
	}
	if ok, _ := s.IsProcessed("customers", "a.csv", mtime, 43); ok {
		t.Error("same name with a different size must be unprocessed")
	}
	if ok, _ := s.IsProcessed("customers", "a.csv", mtime, 42); !ok {
		t.Error("identical name + mtime + size must stay processed")
	}
}

func TestProcessedStore_MarkIsIdempotentAndAppends(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Now().UTC()
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "b.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}
	// A changed file state of the same name is a new record, not a duplicate.
	if err := s.MarkProcessed("customers", "a.csv", mtime.Add(time.Minute), 43, at); err != nil {
		t.Fatal(err)
	}

	entries, err := s.Entries("customers")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("entries = %d, want 3 (idempotent mark, changed state appended)", len(entries))
	}
}

func TestProcessedStore_FileFollowsSchema(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 123, time.UTC)
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "processed", "customers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Topic   string `json:"topic"`
		Entries []struct {
			SourceFileIdentifier string `json:"source_file_identifier"`
			ModTimeUnixNano      int64  `json:"mtime_unixnano"`
			Size                 int64  `json:"size"`
			ProcessedAt          string `json:"processed_at"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Topic != "customers" || len(doc.Entries) != 1 {
		t.Fatalf("processed_json schema mismatch: %+v", doc)
	}
	e := doc.Entries[0]
	if e.SourceFileIdentifier != "a.csv" || e.ModTimeUnixNano != mtime.UnixNano() || e.Size != 42 || e.ProcessedAt == "" {
		t.Errorf("processed_json entry mismatch: %+v", e)
	}
}
