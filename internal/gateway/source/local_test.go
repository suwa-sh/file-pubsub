package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNew_種別を指定した場合_対応するコネクタが返り未知の種別はエラーになること(t *testing.T) {
	// Arrange
	cases := map[string]any{
		"local": (*Local)(nil),
		"ftp":   (*FTP)(nil),
		"sftp":  (*SFTP)(nil),
		"scp":   (*SCP)(nil),
	}
	for typ, want := range cases {
		// Act
		c, err := New(Options{Type: typ, Directory: t.TempDir(), Host: "h", Username: "u", Password: "p"})

		// Assert
		if err != nil || c == nil {
			t.Fatalf("%s connector: %v", typ, err)
		}
		if fmt.Sprintf("%T", c) != fmt.Sprintf("%T", want) {
			t.Errorf("New(%s) = %T, want %T", typ, c, want)
		}
		_ = c.Close()
	}

	// Act & Assert (未知の種別)
	if _, err := New(Options{Type: "bogus"}); err == nil {
		t.Error("unknown type must fail")
	}
}

func TestLocal_List_サブディレクトリが混在する場合_通常ファイルだけが返ること(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders_1.csv"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orders_2.csv"), []byte("12"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	files, err := NewLocal(dir).List(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if len(files) != 2 {
		t.Fatalf("List = %d files, want 2 (directories skipped)", len(files))
	}
	if files[0].Name != "orders_1.csv" || files[0].Size != 5 || files[0].ModTime.IsZero() {
		t.Errorf("file info mismatch: %+v", files[0])
	}
}

func TestLocal_Fetch_ソースファイルがある場合_最終名でコピーされ一時ファイルが残らないこと(t *testing.T) {
	// Arrange
	srcDir := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "work")
	if err := os.WriteFile(filepath.Join(srcDir, "orders.csv"), []byte("id,qty\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := NewLocal(srcDir).Fetch(context.Background(), "orders.csv", destDir)

	// Assert
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got != filepath.Join(destDir, "orders.csv") {
		t.Errorf("path = %q", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "id,qty\n1,2\n" {
		t.Errorf("content = %q", data)
	}
	if _, err := os.Stat(got + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp download name must not remain after rename")
	}
}

func TestLocal_Fetch_ソースファイルが無い場合_エラーになること(t *testing.T) {
	// Arrange & Act
	_, err := NewLocal(t.TempDir()).Fetch(context.Background(), "nope.csv", t.TempDir())

	// Assert
	if err == nil {
		t.Error("missing source file must fail")
	}
}

func TestLocal_Fetch_パストラバーサル名を渡した場合_拒否されること(t *testing.T) {
	// Arrange & Act
	_, err := NewLocal(t.TempDir()).Fetch(context.Background(), "../escape", t.TempDir())

	// Assert
	if err == nil {
		t.Error("path traversal must be rejected")
	}
}

func TestLocal_Remove_対象ファイルがある場合_元ファイルが削除されること(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.csv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	err := NewLocal(dir).Remove(context.Background(), "orders.csv")

	// Assert
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("original file must be deleted")
	}
}

func TestLocal_キャンセル済みcontextの場合_全操作が中断されること(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := NewLocal(t.TempDir())

	// Act & Assert
	if _, err := l.List(ctx); err == nil {
		t.Error("cancelled context must abort List")
	}
	if _, err := l.Fetch(ctx, "f", t.TempDir()); err == nil {
		t.Error("cancelled context must abort Fetch")
	}
	if err := l.Remove(ctx, "f"); err == nil {
		t.Error("cancelled context must abort Remove")
	}
}
