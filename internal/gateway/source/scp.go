package source

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SCP は SSH デーモンが SFTP サブシステムを有効にしていないサーバ向けに、
// 素の SSH セッションでファイルを収集する。List / Fetch / Remove はそれぞれ
// 1 セッションずつ POSIX シェルコマンド (find + stat / cat / rm) を実行するため、
// リモートアカウントにはシェルと GNU coreutils/findutils が必要 (Linux 主流。
// stat -c は BSD 非互換)。改行を含むファイル名は一覧のパースが対応していない。
// サイズと mtime は stat 由来なので、安定判定は他のコネクタと同様に機能する。
type SCP struct {
	opts Options
	ssh  *ssh.Client
}

// NewSCP は SCP コネクタを返す。接続は初回利用時に遅延確立されるため、
// 生成自体はネットワークに触れない。
func NewSCP(o Options) *SCP { return &SCP{opts: o} }

// connect は一度だけ SSH 接続を確立し、以降は再利用する。
func (c *SCP) connect() (*ssh.Client, error) {
	if c.ssh != nil {
		return c.ssh, nil
	}
	conn, err := dialSSH(c.opts)
	if err != nil {
		return nil, err
	}
	c.ssh = conn
	return conn, nil
}

// run は新規 SSH セッションでコマンドを 1 つ実行し、stdout を返す。
// stderr はエラーに畳み込み、collect_failed ログが原因を保持できるようにする。
func (c *SCP) run(cmd string) ([]byte, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	sess, err := conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open ssh session: %w", err)
	}
	defer func() { _ = sess.Close() }()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	if err := sess.Run(cmd); err != nil {
		return nil, fmt.Errorf("run %q: %w (stderr: %s)", cmd, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// shellQuote はリモートシェルコマンドで安全に使えるよう s をシングルクォートで囲む。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// List は find + stat (1 行ごとに "size mtime name") を使ってソースディレクトリ
// 直下の通常ファイルを返す。
func (c *SCP) List(ctx context.Context) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("cd %s && find . -maxdepth 1 -type f -exec stat -c '%%s %%Y %%n' {} +", shellQuote(c.opts.Directory))
	out, err := c.run(cmd)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", c.opts.Directory, err)
	}
	files, err := parseSCPList(string(out))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", c.opts.Directory, err)
	}
	return files, nil
}

// parseSCPList は "size mtime ./name" 形式の stat 行を FileInfo にパースする。
func parseSCPList(out string) ([]FileInfo, error) {
	var files []FileInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected stat line %q", line)
		}
		size, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("unexpected stat line %q: %w", line, err)
		}
		mtime, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("unexpected stat line %q: %w", line, err)
		}
		name := strings.TrimPrefix(parts[2], "./")
		if err := validateName(name); err != nil {
			return nil, err
		}
		files = append(files, FileInfo{Name: name, Size: size, ModTime: time.Unix(mtime, 0)})
	}
	return files, nil
}

// Fetch は cat でリモートファイルを一時名のまま destDir にストリームし、コピー後の
// サイズを直前の stat と突き合わせて検証してから最終名にリネームする (LR-303)。
// サイズは cat と同じセッションバッチで読むため、並行する書き換えは不一致として
// 検出される (Fetch が呼ばれる前にファイルは安定判定済み)。
func (c *SCP) Fetch(ctx context.Context, name, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	remote := shellQuote(path.Join(c.opts.Directory, name))

	out, err := c.run("stat -c '%s' " + remote)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	want, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return "", fmt.Errorf("fetch %s: unexpected stat output %q: %w", name, out, err)
	}

	conn, err := c.connect()
	if err != nil {
		return "", err
	}
	sess, err := conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("fetch %s: open ssh session: %w", name, err)
	}
	defer func() { _ = sess.Close() }()
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	if err := sess.Start("cat -- " + remote); err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	finish := func() error {
		if err := sess.Wait(); err != nil {
			return fmt.Errorf("remote cat failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
	local, err := writeTempAndRename(name, destDir, stdout, want, finish)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	return local, nil
}

// Remove は archive 保存成功後に元ファイルを削除する (delete 扱い)。
func (c *SCP) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if _, err := c.run("rm -- " + shellQuote(path.Join(c.opts.Directory, name))); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// Close は SSH 接続が開かれていれば閉じる。
func (c *SCP) Close() error {
	if c.ssh == nil {
		return nil
	}
	err := c.ssh.Close()
	c.ssh = nil
	return err
}
