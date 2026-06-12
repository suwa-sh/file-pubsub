package source

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/jlaffaye/ftp"
)

// ftpDefaultPort は FTP 制御コネクションの既定ポート (設定 port 0 のとき)。
const ftpDefaultPort = 21

// ftpTimeout は制御コネクションの接続と応答の上限時間。
const ftpTimeout = 30 * time.Second

// FTP は FTP サーバのディレクトリからファイルを収集する。データコネクションは
// パッシブモード (下層クライアントの唯一のモード) を使うため、NAT や外向き専用
// ファイアウォール越しでも動作する。
//
// セキュリティ上の制約: 平文 FTP は認証情報とファイル内容を暗号化せずに送る。
// 信頼できるネットワークでのみ使用し、サーバが対応していれば sftp を優先する
// こと (README のセキュリティ注記を参照)。
type FTP struct {
	opts Options
	conn *ftp.ServerConn
}

// NewFTP は FTP コネクタを返す。接続は初回利用時に遅延確立されるため、
// 生成自体はネットワークに触れない。
func NewFTP(o Options) *FTP { return &FTP{opts: o} }

// connect は一度だけ接続・ログインし、以降は制御コネクションを再利用する。
func (c *FTP) connect() (*ftp.ServerConn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	addr := hostPort(c.opts.Host, c.opts.Port, ftpDefaultPort)
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(ftpTimeout))
	if err != nil {
		return nil, fmt.Errorf("connect ftp %s: %w", addr, err)
	}
	if err := conn.Login(c.opts.Username, c.opts.Password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("ftp login as %q on %s: %w", c.opts.Username, addr, err)
	}
	c.conn = conn
	return conn, nil
}

// List はソースディレクトリ内の通常ファイルを返す。
func (c *FTP) List(ctx context.Context) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	entries, err := conn.List(c.opts.Directory)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", c.opts.Directory, err)
	}
	return ftpEntriesToFileInfo(entries), nil
}

// ftpEntriesToFileInfo は LIST 応答のうち通常ファイルだけを残す。LIST 応答の
// mtime 精度は分単位の場合があるが、安定判定は連続する観測同士の一致を比較する
// ため問題なく機能する。
func ftpEntriesToFileInfo(entries []*ftp.Entry) []FileInfo {
	var files []FileInfo
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile {
			continue
		}
		files = append(files, FileInfo{Name: e.Name, Size: int64(e.Size), ModTime: e.Time})
	}
	return files
}

// Fetch は RETR で name を一時名のまま destDir にダウンロードし、転送完了応答を
// 確認し、SIZE と突き合わせてサイズを検証して (サーバ非対応時はスキップ)、
// 最終名にリネームする (LR-303)。
func (c *FTP) Fetch(ctx context.Context, name, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	conn, err := c.connect()
	if err != nil {
		return "", err
	}
	remote := path.Join(c.opts.Directory, name)

	want := int64(-1)
	if size, err := conn.FileSize(remote); err == nil {
		want = size
	}
	resp, err := conn.Retr(remote)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	local, err := downloadAndClose(name, destDir, resp, want)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	return local, nil
}

// downloadAndClose は FTP データコネクション resp 上で共通ダウンロードプロトコル
// を実行し、あらゆる経路で resp が確実に閉じられることを保証する: 成功経路では
// writeTempAndRename の finish ステップとして閉じ (転送完了応答を確認し、その
// エラーは fetch を失敗させる)、deferred close が全エラー経路をカバーするため、
// 失敗したダウンロードがデータコネクションをリークすることはない。
// close は高々 1 回しか実行されない。
func downloadAndClose(name, destDir string, resp io.ReadCloser, want int64) (string, error) {
	closed := false
	var closeErr error
	closeResp := func() error {
		if !closed {
			closed = true
			closeErr = resp.Close()
		}
		return closeErr
	}
	defer func() { _ = closeResp() }()
	return writeTempAndRename(name, destDir, resp, want, closeResp)
}

// Remove は archive 保存成功後に元ファイルを削除する (delete 扱い)。
func (c *FTP) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	conn, err := c.connect()
	if err != nil {
		return err
	}
	if err := conn.Delete(path.Join(c.opts.Directory, name)); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// Close は制御コネクションが確立済みであれば切断する。
func (c *FTP) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Quit()
	c.conn = nil
	return err
}
