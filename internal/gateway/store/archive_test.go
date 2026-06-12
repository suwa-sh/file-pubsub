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

func TestArchiveStore_PutWorkしてPromoteした場合_アーカイブに昇格しワークファイルが消えること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	id := "20260612T093001_orders_sales.csv"

	// Act (PutWork)
	if err := s.PutWork("orders", id, strings.NewReader("payload")); err != nil {
		t.Fatalf("PutWork: %v", err)
	}

	// Assert (PutWork)
	if _, err := os.Stat(s.WorkPath("orders", id)); err != nil {
		t.Fatalf("work file missing: %v", err)
	}

	// Act (Promote)
	if err := s.Promote("orders", id); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert (Promote)
	if _, err := os.Stat(s.WorkPath("orders", id)); !os.IsNotExist(err) {
		t.Error("work file must be removed after promote")
	}
	r, err := s.Open("orders", id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()
	data, _ := io.ReadAll(r)
	if string(data) != "payload" {
		t.Errorf("archive content = %q", data)
	}
	ok, err := s.Exists("orders", id)
	if err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}
}

func TestArchiveStore_Promote_中断後に再実行した場合_冪等に成功すること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	id := "m1"
	if err := s.PutWork("orders", id, strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote("orders", id); err != nil {
		t.Fatal(err)
	}

	// Act: 中断後の再実行 — 同じワーク内容を再投入し、promote が同じパスを上書きする
	if err := s.PutWork("orders", id, strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	err := s.Promote("orders", id)

	// Assert
	if err != nil {
		t.Fatalf("idempotent promote: %v", err)
	}
}

func TestArchiveStore_retention対象を削除した場合_期限切れだけが消えて昇順一覧が維持されること(t *testing.T) {
	// Arrange
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
	// 残留した一時ファイルはスキャンに現れてはならない
	if err := os.WriteFile(filepath.Join(dataDir, "archive", "orders", "x.tmp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Act (List)
	ids, err := s.ListMessageIDs("orders")

	// Assert (List)
	if err != nil {
		t.Fatalf("ListMessageIDs: %v", err)
	}
	want := []string{"20260101T000000_orders_a.csv", "20260612T000000_orders_b.csv"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}

	// Arrange (retention 判定 (domain) + 削除 (store): 期限切れのファイルだけが消える)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	savedAtOld := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	savedAtNew := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	if !domain.IsExpired(domain.RetentionDeadline(savedAtOld, 90), now) {
		t.Fatal("old file must be expired")
	}
	if domain.IsExpired(domain.RetentionDeadline(savedAtNew, 90), now) {
		t.Fatal("new file must be kept")
	}

	// Act (Delete)
	if err := s.Delete("orders", want[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Assert (Delete)
	ids, _ = s.ListMessageIDs("orders")
	if !reflect.DeepEqual(ids, []string{want[1]}) {
		t.Errorf("after delete ids = %v, want only %v", ids, want[1])
	}
}

func TestArchiveStore_Delete_対象ファイルが無い場合_エラーにならないこと(t *testing.T) {
	// Arrange
	s := NewArchiveStore(t.TempDir())

	// Act & Assert
	if err := s.Delete("orders", "nope"); err != nil {
		t.Errorf("deleting a missing archive must be idempotent: %v", err)
	}
}

func TestArchiveStore_ListMessageIDs_topicが存在しない場合_空で返ること(t *testing.T) {
	// Arrange
	s := NewArchiveStore(t.TempDir())

	// Act
	ids, err := s.ListMessageIDs("nope")

	// Assert
	if err != nil || ids != nil {
		t.Errorf("missing topic: got %v, %v", ids, err)
	}
}

func TestArchiveStore_CleanupWorkTempFiles_tmpが残っている場合_tmpだけ削除され最終名は残ること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewArchiveStore(dataDir)
	if err := s.PutWork("orders", "keep", strings.NewReader("k")); err != nil {
		t.Fatal(err)
	}
	tmp := s.WorkPath("orders", "broken") + ".tmp"
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	err := s.CleanupWorkTempFiles("orders")

	// Assert
	if err != nil {
		t.Fatalf("CleanupWorkTempFiles: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("leftover temp file must be removed")
	}
	if _, err := os.Stat(s.WorkPath("orders", "keep")); err != nil {
		t.Error("final-name work file must be kept")
	}
}
