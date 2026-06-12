package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessedStore_MarkProcessedした場合_記録済みになり未記録の名前は未処理のままなこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Date(2026, 6, 12, 9, 30, 1, 0, time.UTC)
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act & Assert (未記録)
	ok, err := s.IsProcessed("customers", "customers_20260612.csv", mtime, 42)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("unrecorded file must be unprocessed")
	}

	// Act (Mark)
	if err := s.MarkProcessed("customers", "customers_20260612.csv", mtime, 42, at); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	// Assert (記録後)
	ok, err = s.IsProcessed("customers", "customers_20260612.csv", mtime, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("recorded file must be processed")
	}
	// 同じ topic でも別の識別子は未処理のまま
	ok, _ = s.IsProcessed("customers", "customers_20260613.csv", mtime, 42)
	if ok {
		t.Error("different name must be unprocessed")
	}
}

// 処理済みキーを保証するテスト: 同名でも mtime またはサイズが異なるものは
// 別のソースファイル状態であり、処理済みと報告してはならない。
func TestIsProcessed_同名でmtimeかサイズが異なる場合_未処理と判定されること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Now().UTC()
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}

	// Act & Assert
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

func TestMarkProcessed_同じ状態を再markした場合_冪等で変化した状態だけ追記されること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	at := time.Now().UTC()
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	// Act
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkProcessed("customers", "b.csv", mtime, 42, at); err != nil {
		t.Fatal(err)
	}
	// 同名でも状態が変化したファイルは重複ではなく新規記録
	if err := s.MarkProcessed("customers", "a.csv", mtime.Add(time.Minute), 43, at); err != nil {
		t.Fatal(err)
	}

	// Assert
	entries, err := s.Entries("customers")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("entries = %d, want 3 (idempotent mark, changed state appended)", len(entries))
	}
}

func TestProcessedStore_MarkProcessedした場合_ファイルがスキーマに従うこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewProcessedStore(dataDir)
	mtime := time.Date(2026, 6, 12, 9, 0, 0, 123, time.UTC)

	// Act
	if err := s.MarkProcessed("customers", "a.csv", mtime, 42, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Assert
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
