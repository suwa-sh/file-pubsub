package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic_未作成のサブディレクトリに書いた場合_最終名で書き出され一時ファイルが残らないこと(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	dst := filepath.Join(dir, "sub", "orders.csv")

	// Act
	err := WriteFileAtomic(dst, strings.NewReader("id,qty\n1,2\n"), 0o644)

	// Assert
	if err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "id,qty\n1,2\n" {
		t.Errorf("content = %q", data)
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file must not remain after rename")
	}
}

func TestWriteFileAtomic_既存ファイルがある場合_上書きされること(t *testing.T) {
	// Arrange
	dst := filepath.Join(t.TempDir(), "f.txt")
	if err := WriteFileAtomic(dst, strings.NewReader("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := WriteFileAtomic(dst, strings.NewReader("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Assert
	data, _ := os.ReadFile(dst)
	if string(data) != "new" {
		t.Errorf("content = %q, want new", data)
	}
}

func TestWriteJSONAtomic_値を書いた場合_JSONとして読み戻せること(t *testing.T) {
	// Arrange
	dst := filepath.Join(t.TempDir(), "meta.json")

	// Act
	err := WriteJSONAtomic(dst, map[string]int{"n": 1})

	// Assert
	if err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}
	var got map[string]int
	if err := readJSON(dst, &got); err != nil {
		t.Fatalf("readJSON: %v", err)
	}
	if got["n"] != 1 {
		t.Errorf("got %v", got)
	}
}

func TestCopyFileAtomic_ソースファイルがある場合_内容がコピーされること(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "dst.bin")

	// Act
	err := CopyFileAtomic(src, dst)

	// Assert
	if err != nil {
		t.Fatalf("CopyFileAtomic: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("content = %q", data)
	}
}

func TestCopyFileAtomic_ソースファイルが無い場合_エラーになること(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act & Assert
	if err := CopyFileAtomic(filepath.Join(dir, "missing"), filepath.Join(dir, "dst")); err == nil {
		t.Error("missing source must fail")
	}
}

func TestCleanupTempFiles_tmpと通常ファイルが混在する場合_tmpだけ削除されること(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	for _, name := range []string{"a.tmp", "b.csv", "c.csv.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	err := CleanupTempFiles(dir)

	// Assert
	if err != nil {
		t.Fatalf("CleanupTempFiles: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	var rest []string
	for _, e := range entries {
		rest = append(rest, e.Name())
	}
	if len(rest) != 1 || rest[0] != "b.csv" {
		t.Errorf("remaining = %v, want only b.csv", rest)
	}
}

func TestCleanupTempFiles_ディレクトリが無い場合_エラーにならないこと(t *testing.T) {
	// Arrange & Act & Assert
	if err := CleanupTempFiles(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("missing dir must not be an error: %v", err)
	}
}
