package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sshDefaultPort は sftp / scp 共通の SSH 既定ポート (設定 port 0 のとき)。
const sshDefaultPort = 22

// sshTimeout は TCP 接続と SSH ハンドシェイクの上限時間。
const sshTimeout = 30 * time.Second

// sshClientConfig は sftp / scp コネクタが共有する SSH クライアント設定を、
// ソースの認証設定 (E-005) から構築する: 鍵ファイル認証 (鍵が暗号化されている
// 場合は password をパスフレーズとして再利用) および/またはパスワード認証。
//
// セキュリティ上の制約: ホスト鍵検証は ssh.InsecureIgnoreHostKey() で意図的に
// スキップしている。設定スキーマはホスト鍵情報を保持せず (config.go Auth)、
// 対象のレガシーホストが管理された known_hosts を公開していることは稀なため。
// トレードオフ — 経路上の中間者がソースサーバになりすませる — は README の
// セキュリティ注記に記載済み。file-pubsub はソースに近い信頼セグメントで動かすこと。
func sshClientConfig(o Options) (*ssh.ClientConfig, error) {
	var methods []ssh.AuthMethod
	if o.KeyFile != "" {
		pem, err := os.ReadFile(o.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("read key file: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && o.Password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(o.Password))
		}
		if err != nil {
			return nil, fmt.Errorf("parse key file %s: %w", o.KeyFile, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if o.Password != "" {
		methods = append(methods, ssh.Password(o.Password))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth configured for %s: set auth.password or auth.key_file", o.Type)
	}
	return &ssh.ClientConfig{
		User:            o.Username,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 上記の制約コメントを参照
		Timeout:         sshTimeout,
	}, nil
}

// dialSSH は sftp / scp コネクタが共有する SSH クライアント接続を開く。
func dialSSH(o Options) (*ssh.Client, error) {
	cfg, err := sshClientConfig(o)
	if err != nil {
		return nil, err
	}
	addr := hostPort(o.Host, o.Port, sshDefaultPort)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect ssh %s as %q: %w", addr, o.Username, err)
	}
	return client, nil
}

// SFTP は SSH サーバの SFTP サブシステム経由でディレクトリからファイルを収集する。
// サーバが対応していれば ftp (非暗号) や scp (シェルが必要) よりこのコネクタを
// 優先すること。
type SFTP struct {
	opts Options
	ssh  *ssh.Client
	sftp *sftp.Client
}

// NewSFTP は SFTP コネクタを返す。接続は初回利用時に遅延確立されるため、
// 生成自体はネットワークに触れない。
func NewSFTP(o Options) *SFTP { return &SFTP{opts: o} }

// connect は一度だけ接続して SFTP サブシステムを開き、以降は再利用する。
func (c *SFTP) connect() (*sftp.Client, error) {
	if c.sftp != nil {
		return c.sftp, nil
	}
	conn, err := dialSSH(c.opts)
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open sftp subsystem: %w", err)
	}
	c.ssh, c.sftp = conn, client
	return client, nil
}

// List はソースディレクトリ内のファイル (名前・サイズ・mtime) を返す。
func (c *SFTP) List(ctx context.Context) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	entries, err := client.ReadDir(c.opts.Directory)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", c.opts.Directory, err)
	}
	var files []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, FileInfo{Name: e.Name(), Size: e.Size(), ModTime: e.ModTime()})
	}
	return files, nil
}

// Fetch は name を一時名で destDir にダウンロードし、コピー後のサイズをリモートの
// stat と突き合わせて検証してから最終名にリネームする (LR-303)。
func (c *SFTP) Fetch(ctx context.Context, name, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	client, err := c.connect()
	if err != nil {
		return "", err
	}
	remote := path.Join(c.opts.Directory, name)
	src, err := client.Open(remote)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	defer func() { _ = src.Close() }()
	stat, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	local, err := writeTempAndRename(name, destDir, src, stat.Size(), nil)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	return local, nil
}

// Remove は archive 保存成功後に元ファイルを削除する (delete 扱い)。
func (c *SFTP) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	client, err := c.connect()
	if err != nil {
		return err
	}
	if err := client.Remove(path.Join(c.opts.Directory, name)); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// Close は SFTP サブシステムと SSH 接続が開かれていれば閉じる。
func (c *SFTP) Close() error {
	var err error
	if c.sftp != nil {
		err = c.sftp.Close()
		c.sftp = nil
	}
	if c.ssh != nil {
		if closeErr := c.ssh.Close(); err == nil {
			err = closeErr
		}
		c.ssh = nil
	}
	return err
}
