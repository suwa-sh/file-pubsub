package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "sub", "orders.csv")

	if err := WriteFileAtomic(dst, strings.NewReader("id,qty\n1,2\n"), 0o644); err != nil {
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

func TestWriteFileAtomic_OverwritesExisting(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "f.txt")
	if err := WriteFileAtomic(dst, strings.NewReader("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(dst, strings.NewReader("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "new" {
		t.Errorf("content = %q, want new", data)
	}
}

func TestWriteJSONAtomic(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "meta.json")
	if err := WriteJSONAtomic(dst, map[string]int{"n": 1}); err != nil {
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

func TestCopyFileAtomic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "dst.bin")
	if err := CopyFileAtomic(src, dst); err != nil {
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

func TestCopyFileAtomic_MissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := CopyFileAtomic(filepath.Join(dir, "missing"), filepath.Join(dir, "dst")); err == nil {
		t.Error("missing source must fail")
	}
}

func TestCleanupTempFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.tmp", "b.csv", "c.csv.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CleanupTempFiles(dir); err != nil {
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

func TestCleanupTempFiles_MissingDir(t *testing.T) {
	if err := CleanupTempFiles(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("missing dir must not be an error: %v", err)
	}
}
