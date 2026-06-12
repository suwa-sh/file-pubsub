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

func TestHostPort(t *testing.T) {
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
		if got := hostPort(c.host, c.port, c.deflt); got != c.want {
			t.Errorf("hostPort(%q, %d, %d) = %q, want %q", c.host, c.port, c.deflt, got, c.want)
		}
	}
}

func TestWriteTempAndRename(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "work")
	got, err := writeTempAndRename("orders.csv", destDir, strings.NewReader("id,qty\n1,2\n"), 11, nil)
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

func TestWriteTempAndRename_SizeMismatch(t *testing.T) {
	destDir := t.TempDir()
	if _, err := writeTempAndRename("f.csv", destDir, strings.NewReader("abc"), 99, nil); err == nil {
		t.Fatal("size mismatch must fail")
	}
	assertNoFiles(t, destDir)
}

func TestWriteTempAndRename_UnknownSizeSkipsCheck(t *testing.T) {
	if _, err := writeTempAndRename("f.csv", t.TempDir(), strings.NewReader("abc"), -1, nil); err != nil {
		t.Fatalf("want = -1 must skip the size check: %v", err)
	}
}

func TestWriteTempAndRename_FinishError(t *testing.T) {
	destDir := t.TempDir()
	finish := func() error { return errors.New("transfer not complete") }
	if _, err := writeTempAndRename("f.csv", destDir, strings.NewReader("abc"), 3, finish); err == nil {
		t.Fatal("finish error must fail the fetch")
	}
	assertNoFiles(t, destDir)
}

// fakeReadCloser counts Close calls so the tests can assert the FTP data
// connection is released on every path.
type fakeReadCloser struct {
	io.Reader
	closeCalls int
	closeErr   error
}

func (f *fakeReadCloser) Close() error {
	f.closeCalls++
	return f.closeErr
}

// TestDownloadAndClose_ClosesOnEveryPath guards the FTP data-connection
// lifecycle: resp must be closed exactly once whether the download succeeds,
// the copy fails mid-stream or the size check rejects the result.
func TestDownloadAndClose_ClosesOnEveryPath(t *testing.T) {
	t.Run("success closes once", func(t *testing.T) {
		resp := &fakeReadCloser{Reader: strings.NewReader("abc")}
		got, err := downloadAndClose("f.csv", t.TempDir(), resp, 3)
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
	t.Run("size mismatch still closes", func(t *testing.T) {
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: strings.NewReader("abc")}
		if _, err := downloadAndClose("f.csv", destDir, resp, 99); err == nil {
			t.Fatal("size mismatch must fail")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (no leak on the error path)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
	t.Run("read error still closes", func(t *testing.T) {
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: io.MultiReader(strings.NewReader("ab"), errReader{})}
		if _, err := downloadAndClose("f.csv", destDir, resp, 3); err == nil {
			t.Fatal("a mid-stream read error must fail the fetch")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (no leak on the error path)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
	t.Run("close error fails the fetch once", func(t *testing.T) {
		destDir := t.TempDir()
		resp := &fakeReadCloser{Reader: strings.NewReader("abc"), closeErr: errors.New("426 transfer aborted")}
		if _, err := downloadAndClose("f.csv", destDir, resp, 3); err == nil {
			t.Fatal("a failing transfer-complete reply must fail the fetch")
		}
		if resp.closeCalls != 1 {
			t.Errorf("closeCalls = %d, want 1 (the deferred close must not double-close)", resp.closeCalls)
		}
		assertNoFiles(t, destDir)
	})
}

// errReader fails every Read, simulating a dropped data connection.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// assertNoFiles checks that no final or temp file leaked into dir.
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

func TestFTPEntriesToFileInfo(t *testing.T) {
	now := time.Now()
	entries := []*ftp.Entry{
		{Name: "orders_1.csv", Type: ftp.EntryTypeFile, Size: 5, Time: now},
		{Name: "subdir", Type: ftp.EntryTypeFolder},
		{Name: "link", Type: ftp.EntryTypeLink},
	}
	files := ftpEntriesToFileInfo(entries)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1 (directories and links skipped)", len(files))
	}
	if files[0].Name != "orders_1.csv" || files[0].Size != 5 || !files[0].ModTime.Equal(now) {
		t.Errorf("file info mismatch: %+v", files[0])
	}
}

func TestSSHClientConfig_Password(t *testing.T) {
	cfg, err := sshClientConfig(Options{Type: "sftp", Username: "producer", Password: "secret"})
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

func TestSSHClientConfig_KeyFile(t *testing.T) {
	keyPath := writeTestKey(t)
	cfg, err := sshClientConfig(Options{Type: "scp", Username: "producer", KeyFile: keyPath})
	if err != nil {
		t.Fatalf("sshClientConfig: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Errorf("auth methods = %d, want 1 (public key)", len(cfg.Auth))
	}
}

func TestSSHClientConfig_KeyFileAndPassword(t *testing.T) {
	keyPath := writeTestKey(t)
	cfg, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: keyPath, Password: "secret"})
	if err != nil {
		t.Fatalf("sshClientConfig: %v", err)
	}
	if len(cfg.Auth) != 2 {
		t.Errorf("auth methods = %d, want 2 (public key + password)", len(cfg.Auth))
	}
}

func TestSSHClientConfig_Errors(t *testing.T) {
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer"}); err == nil {
		t.Error("missing password and key file must fail")
	}
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: "/nonexistent/key"}); err == nil {
		t.Error("unreadable key file must fail")
	}
	garbage := filepath.Join(t.TempDir(), "garbage")
	if err := os.WriteFile(garbage, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sshClientConfig(Options{Type: "sftp", Username: "producer", KeyFile: garbage}); err == nil {
		t.Error("unparsable key file must fail")
	}
}

// writeTestKey generates an ed25519 private key in OpenSSH PEM format.
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

func TestShellQuote(t *testing.T) {
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
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSCPList(t *testing.T) {
	out := "5 1765500000 ./orders_1.csv\n1024 1765500060 ./with space.csv\n"
	files, err := parseSCPList(out)
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

func TestParseSCPList_Empty(t *testing.T) {
	files, err := parseSCPList("")
	if err != nil {
		t.Fatalf("parseSCPList: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %d, want 0", len(files))
	}
}

func TestParseSCPList_Malformed(t *testing.T) {
	for _, out := range []string{"oops\n", "x 1765500000 ./f\n", "5 y ./f\n", "5 1765500000 ./../escape\n"} {
		if _, err := parseSCPList(out); err == nil {
			t.Errorf("malformed line %q must fail", out)
		}
	}
}

// TestRemoteConnectors_NoNetworkChecks covers the checks every remote
// connector performs before touching the network: context cancellation,
// file-name validation and Close on a never-connected client. Real protocol
// I/O is covered by the docker compose E2E environment.
func TestRemoteConnectors_NoNetworkChecks(t *testing.T) {
	opts := Options{Host: "legacy-host01", Directory: "/out/orders", Username: "u", Password: "p"}
	conns := map[string]Connector{
		"ftp":  NewFTP(opts),
		"sftp": NewSFTP(opts),
		"scp":  NewSCP(opts),
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
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
