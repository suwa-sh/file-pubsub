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

	ok, err := s.IsProcessed("customers", "customers_20260612.csv")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("unrecorded file must be unprocessed")
	}

	if err := s.MarkProcessed("customers", "customers_20260612.csv", at); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	ok, err = s.IsProcessed("customers", "customers_20260612.csv")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("recorded file must be processed")
	}

	// Other identifier in the same topic stays unprocessed.
	ok, _ = s.IsProcessed("customers", "customers_20260613.csv")
	if ok {
		t.Error("different identifier must be unprocessed")
	}
}

func TestProcessedStore_MarkIsIdempotentAndAppends(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Now().UTC()

	if err := s.MarkProcessed("customers", "a.csv", at); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "a.csv", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "b.csv", at); err != nil {
		t.Fatal(err)
	}

	entries, err := s.Entries("customers")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2 (idempotent mark)", len(entries))
	}
}

func TestProcessedStore_FileFollowsSchema(t *testing.T) {
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	if err := s.MarkProcessed("customers", "a.csv", time.Now()); err != nil {
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
			ProcessedAt          string `json:"processed_at"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Topic != "customers" || len(doc.Entries) != 1 || doc.Entries[0].SourceFileIdentifier != "a.csv" || doc.Entries[0].ProcessedAt == "" {
		t.Errorf("processed_json schema mismatch: %+v", doc)
	}
}
