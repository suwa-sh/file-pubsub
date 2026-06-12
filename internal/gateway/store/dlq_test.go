package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDLQStore_Isolateした場合_ファイルとmetaが読み戻せること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	src := filepath.Join(dataDir, "archive-file")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewDLQStore(dataDir)
	meta := DLQMeta{
		MessageID:       "20260611T220500_invoices_inv_0042.csv",
		Topic:           "invoices",
		IsolationReason: "permission denied (write)",
		FailureCount:    5,
		IsolatedAt:      time.Date(2026, 6, 11, 22, 31, 10, 0, time.UTC),
	}

	// Act
	err := s.Isolate(src, meta)

	// Assert
	if err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	data, err := os.ReadFile(s.FilePath("invoices", meta.MessageID))
	if err != nil {
		t.Fatalf("isolated file: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("isolated content = %q", data)
	}
	got, err := s.ReadMeta("invoices", meta.MessageID)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if got.IsolationReason != meta.IsolationReason || got.FailureCount != 5 || !got.IsolatedAt.Equal(meta.IsolatedAt) {
		t.Errorf("meta mismatch: %+v", got)
	}
}

func TestDLQStore_Isolate_同じメッセージを再隔離した場合_冪等に上書きされ二重隔離されないこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	src := filepath.Join(dataDir, "archive-file")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewDLQStore(dataDir)
	meta := DLQMeta{MessageID: "m1", Topic: "invoices", IsolationReason: "x", FailureCount: 5, IsolatedAt: time.Now().UTC()}
	if err := s.Isolate(src, meta); err != nil {
		t.Fatal(err)
	}

	// Act
	err := s.Isolate(src, meta)

	// Assert
	if err != nil {
		t.Fatalf("re-isolation must overwrite idempotently: %v", err)
	}
	metas, err := s.List("invoices")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Errorf("List = %d entries, want 1 (no double isolation)", len(metas))
	}
}

func TestDLQStore_List_複数メッセージがある場合_messageID昇順で返ること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	src := filepath.Join(dataDir, "archive-file")
	if err := os.WriteFile(src, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewDLQStore(dataDir)
	for _, id := range []string{"b", "a"} {
		if err := s.Isolate(src, DLQMeta{MessageID: id, Topic: "invoices", IsolationReason: "r", FailureCount: 5, IsolatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	metas, err := s.List("invoices")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 || metas[0].MessageID != "a" || metas[1].MessageID != "b" {
		t.Errorf("List = %+v, want sorted by message_id", metas)
	}
}

func TestDLQStore_List_topicが存在しない場合_空で返ること(t *testing.T) {
	// Arrange
	s := NewDLQStore(t.TempDir())

	// Act
	metas, err := s.List("nope")

	// Assert
	if err != nil || metas != nil {
		t.Errorf("missing topic: got %v, %v", metas, err)
	}
}

func TestDLQStore_Isolate_messageIDとtopicが空の場合_エラーになること(t *testing.T) {
	// Arrange
	s := NewDLQStore(t.TempDir())

	// Act & Assert
	if err := s.Isolate("src", DLQMeta{}); err == nil {
		t.Error("missing message_id/topic must fail")
	}
}
