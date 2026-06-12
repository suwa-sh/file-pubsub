package source

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"
	"golang.org/x/crypto/ssh"
)

func TestHostPort_ポート指定の組み合わせごとに_既定値補完されたアドレスが返ること(t *testing.T) {
	// Arrange
	cases := []struct {
		host        string
		port, deflt int
		want        string
	}{
		{"legacy-host01", 0, 21, "legacy-host01:21"},
		{"legacy-host01", 0, 22, "legacy-host01:22"},
		{"legacy-host01", 2222, 22, "legacy-host01:2222"},
		{"::1", 0, 22, "[::1]:22"},
	}
	for _, c := range cases {
		// Act
		got := hostPort(c.host, c.port, c.deflt)

		// Assert
		if got != c.want {
			t.Errorf("hostPort(%q, %d, %d) = %q, want %q", c.host, c.port, c.deflt, got, c.want)
		}
	}
}

func TestWriteTempAndRename_サイズが一致する場合_最終名で書き出され一時ファイルが残らないこと(t *testing.T) {
	// Arrange
	destDir := filepath.Join(t.TempDir(), "work")

	// Act
	got, err := writeTempAndRename("orders.csv", destDir, strings.NewReader("id,qty\n1,2\n"), 11, nil)

	// Assert
	if err != nil {
		t.Fatalf("writeTempAndRename: %v", err)
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

func TestWriteTempAndRename_サイズが不一致の場合_失敗してファイルが残らないこと(t *testing.T) {
	// Arrange
	destDir := t.TempDir()

	// Act
	_, err := writeTempAndRename("f.csv", destDir, strings.NewReader("abc"), 99, nil)

	// Assert
	if err == nil {
		t.Fatal("size mismatch must fail")
	}
	assertNoFiles(t, destDir)
}

func TestWriteTempAndRename_サイズ不明の場合_サイズ検証がスキップされること(t *testing.T) {
	// Arrange & Act
	_, err := writeTempAndRename("f.csv", t.TempDir(), strings.NewReader("abc"), -1, nil)

	// Assert
	if err != nil {
		t.Fatalf("want = -1 must skip the size check: %v", err)
	}
}

func TestWriteTempAndRename_finishがエラーの場合_失敗してファイルが残らないこと(t *testing.T) {
	// Arrange
	destDir := t.TempDir()
	finish := func() error { return errors.New("transfer not complete") }

	// Act
	_, err := writeTempAndRename("f.csv", destDir, strings.NewReader("abc"), 3, finish)

	// Assert
	if err == nil {
		t.Fatal("finish error must fail the fetch")
	}
	assertNoFiles(t, destDir)
}

// fakeReadCloser は Close 呼び出し回数を数え、FTP データコネクションが
// あらゆる経路で解放されることをテストで検証できるようにする。
type fakeReadCloser struct {
	io.Reader
	closeCalls int
	closeErr   error
}

func (f *fakeReadCloser) Close() error {
	f.closeCalls++
	return f.closeErr
}

// FTP データコネクションのライフサイクルを保証するテスト: ダウンロード成功・
// ストリーム途中のコピー失敗・サイズ検証の拒否のいずれでも resp はちょうど
// 1 回 close されなければならない。
func TestDownloadAndClose_成否いずれの経路でも_respがちょうど1回closeされること(t *testing.T) {
	t.Run("成功した場合_1回closeされること", func(t *testing.T) {
		// Arrange
		resp := &fakeReadCloser{Reader: strings.NewReader("abc")}

		// Act
		got, err := downloadAndClose("f.csv", t.TempDir(), resp, 3)

		// Assert
		if err != nil {
			t.Fatalf("downloadAndClose: %v", err)
		}
		if data, err := os.ReadFile(got); err != nil || string(data) != "abc" {
			t.Fatalf("content = %q, err = %v", data, err)
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1", resp.closeCalls)
		}
	})
	t.Run("サイズ不一致の場合_それでも1回closeされること", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: strings.NewReader("abc")}

		// Act
		_, err := downloadAndClose("f.csv", destDir, resp, 99)

		// Assert
		if err == nil {
			t.Fatal("size mismatch must fail")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (no leak on the error path)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
	t.Run("読み込みエラーの場合_それでも1回closeされること", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: io.MultiReader(strings.NewReader("ab"), errReader{})}

		// Act
		_, err := downloadAndClose("f.csv", destDir, resp, 3)

		// Assert
		if err == nil {
			t.Fatal("a mid-stream read error must fail the fetch")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (no leak on the error path)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
	t.Run("closeがエラーの場合_fetchが失敗し二重closeされないこと", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: strings.NewReader("abc"), closeErr: errors.New("426 transfer aborted")}

		// Act
		_, err := downloadAndClose("f.csv", destDir, resp, 3)

		// Assert
		if err == nil {
			t.Fatal("a failing transfer-complete reply must fail the fetch")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (the deferred close must not double-close)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
}

// errReader はすべての Read を失敗させ、データコネクションの切断を擬似する。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// assertNoFiles は dir に最終ファイルも一時ファイルもリークしていないことを検証する。
func assertNoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed download must leave no file behind, found %v", entries)
	}
}

func TestFTPEntriesToFileInfo_ディレクトリとリンクが混在する場合_通常ファイルだけが返ること(t *testing.T) {
	// Arrange
	now := time.Now()
	entries := []*ftp.Entry{
		{Name: "orders_1.csv", Type: ftp.EntryTypeFile, Size: 5, Time: now},
		{Name: "subdir", Type: ftp.EntryTypeFolder},
		{Name: "link", Type: ftp.EntryTypeLink},
	}

	// Act
	files := ftpEntriesToFileInfo(entries)

	// Assert
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1 (directories and links skipped)", len(files))
	}
	if files[0].Name != "orders_1.csv" || files[0].Size != 5 || !files[0].ModTime.Equal(now) {
		t.Errorf("file info mismatch: %+v", files[0])
	}
}

func TestSSHClientConfig_パスワードのみの場合_password認証1件で構成されること(t *testing.T) {
	// Arrange & Act
	cfg, err := sshClientConfig(Options{Type: "sftp", Username: "producer", Password: "secret"})

	// Assert
	if err != nil {
		t.Fatalf("sshClientConfig: %v", err)
	}
	if cfg.User != "producer" {
		t.Errorf("user = %q", cfg.User)
	}
	if len(cfg.Auth) != 1 {
		t.Errorf("auth methods = %d, want 1 (password)", len(cfg.Auth))
	}
	if cfg.HostKeyCallback == nil {
		t.Error("host key callback must be set (InsecureIgnoreHostKey)")
	}
	if cfg.Timeout <= 0 {
		t.Error("dial timeout must be set")
	}
}

func TestSSHClientConfig_鍵ファイルのみの場合_公開鍵認証1件で構成されること(t *testing.T) {
	// Arrange
	keyPath := writeTestKey(t)

	// Act
	cfg, err := sshClientConfig(Options{Type: "scp", Username: "producer", KeyFile: keyPath})

	// Assert
	if err != nil {
		t.Fatalf("sshClientConfig: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Errorf("auth methods = %d, want 1 (public key)", len(cfg.Auth))
	}
}

func TestSSHClientConfig_鍵ファイルとパスワードの両方がある場合_認証2件で構成されること(t *testing.T) {
	// Arrange
	keyPath := writeTestKey(t)

	// Act
	cfg, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: keyPath, Password: "secret"})

	// Assert
	if err != nil {
		t.Fatalf("sshClientConfig: %v", err)
	}
	if len(cfg.Auth) != 2 {
		t.Errorf("auth methods = %d, want 2 (public key + password)", len(cfg.Auth))
	}
}

func TestSSHClientConfig_認証情報が不正な場合_エラーになること(t *testing.T) {
	// Arrange
	garbage := filepath.Join(t.TempDir(), "garbage")
	if err := os.WriteFile(garbage, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act & Assert
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer"}); err == nil {
		t.Error("missing password and key file must fail")
	}
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: "/nonexistent/key"}); err == nil {
		t.Error("unreadable key file must fail")
	}
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: garbage}); err == nil {
		t.Error("unparsable key file must fail")
	}
}

// writeTestKey は OpenSSH PEM 形式の ed25519 秘密鍵を生成するヘルパー。
func writeTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test key")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShellQuote_特殊文字を含む入力の場合_安全にシングルクォートされること(t *testing.T) {
	// Arrange
	cases := map[string]string{
		"/out/orders":    "'/out/orders'",
		"with space":     "'with space'",
		"o'brien.csv":    `'o'\''brien.csv'`,
		"$HOME;rm -rf *": "'$HOME;rm -rf *'",
		"`whoami`":       "'`whoami`'",
		`back\slash`:     `'back\slash'`,
		"日本語ファイル名.csv":   "'日本語ファイル名.csv'",
	}
	for in, want := range cases {
		// Act
		got := shellQuote(in)

		// Assert
		if got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSCPList_正常なstat行の場合_FileInfoにパースされること(t *testing.T) {
	// Arrange
	out := "5 1765500000 ./orders_1.csv\n1024 1765500060 ./with space.csv\n"

	// Act
	files, err := parseSCPList(out)

	// Assert
	if err != nil {
		t.Fatalf("parseSCPList: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if files[0].Name != "orders_1.csv" || files[0].Size != 5 || !files[0].ModTime.Equal(time.Unix(1765500000, 0)) {
		t.Errorf("file info mismatch: %+v", files[0])
	}
	if files[1].Name != "with space.csv" || files[1].Size != 1024 {
		t.Errorf("space in file name must survive the split: %+v", files[1])
	}
}

func TestParseSCPList_空出力の場合_0件になること(t *testing.T) {
	// Arrange & Act
	files, err := parseSCPList("")

	// Assert
	if err != nil {
		t.Fatalf("parseSCPList: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %d, want 0", len(files))
	}
}

func TestParseSCPList_不正な行の場合_エラーになること(t *testing.T) {
	// Arrange
	malformed := []string{"oops\n", "x 1765500000 ./f\n", "5 y ./f\n", "5 1765500000 ./../escape\n"}

	for _, out := range malformed {
		// Act
		_, err := parseSCPList(out)

		// Assert
		if err == nil {
			t.Errorf("malformed line %q must fail", out)
		}
	}
}

// すべてのリモートコネクタがネットワークに触れる前に行う検査を網羅するテスト:
// context キャンセル、ファイル名検証、未接続クライアントへの Close。
// 実プロトコル I/O は docker compose の E2E 環境でカバーする。
func TestRemoteConnectors_未接続の場合_キャンセルとパストラバーサルが拒否されCloseがエラーにならないこと(t *testing.T) {
	// Arrange
	opts := Options{Host: "legacy-host01", Directory: "/out/orders", Username: "u", Password: "p"}
	conns := map[string]Connector{
		"ftp":  NewFTP(opts),
		"sftp": NewSFTP(opts),
		"scp":  NewSCP(opts),
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// Act & Assert
	for typ, c := range conns {
		if _, err := c.List(cancelled); err == nil {
			t.Errorf("%s: cancelled context must abort List", typ)
		}
		if _, err := c.Fetch(cancelled, "f", t.TempDir()); err == nil {
			t.Errorf("%s: cancelled context must abort Fetch", typ)
		}
		if err := c.Remove(cancelled, "f"); err == nil {
			t.Errorf("%s: cancelled context must abort Remove", typ)
		}
		if _, err := c.Fetch(context.Background(), "../escape", t.TempDir()); err == nil {
			t.Errorf("%s: path traversal must be rejected before connecting", typ)
		}
		if err := c.Remove(context.Background(), "../escape"); err == nil {
			t.Errorf("%s: path traversal must be rejected before connecting", typ)
		}
		if err := c.Close(); err != nil {
			t.Errorf("%s: Close on a never-connected client must be nil, got %v", typ, err)
		}
	}
}
