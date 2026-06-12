package source

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/jlaffaye/ftp"
)

// ftpDefaultPort is the FTP control connection default (config port 0).
const ftpDefaultPort = 21

// ftpTimeout bounds the control connection dial and replies.
const ftpTimeout = 30 * time.Second

// FTP collects files from an FTP server directory. Data connections use
// passive mode (the only mode of the underlying client), which works through
// NAT and outbound-only firewalls.
//
// Security constraint: plain FTP carries credentials and file contents
// unencrypted on the wire. Use it only on trusted networks; prefer sftp where
// the server allows it (see the README security notes).
type FTP struct {
	opts Options
	conn *ftp.ServerConn
}

// NewFTP returns an FTP connector. The connection is established lazily on
// first use so construction itself never touches the network.
func NewFTP(o Options) *FTP { return &FTP{opts: o} }

// connect dials and logs in once, reusing the control connection afterwards.
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

// List returns the regular files in the source directory.
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

// ftpEntriesToFileInfo keeps the regular files of a LIST reply. Note that the
// mtime precision of a LIST reply may be minutes; the stability check still
// works because it compares successive observations for equality.
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

// Fetch downloads name with RETR into destDir under a temp name, confirms the
// transfer-complete reply, verifies the size against SIZE (skipped when the
// server does not support it) and renames to the final name (LR-303).
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

// downloadAndClose runs the shared download protocol over the FTP data
// connection resp and guarantees resp is closed on every path: the success
// path closes it as the writeTempAndRename finish step (confirming the
// transfer-complete reply, whose error fails the fetch), and the deferred
// close covers every error path so a failed download never leaks the data
// connection. The close runs at most once.
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

// Remove deletes the original file after archive save success (delete handling).
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

// Close quits the control connection if one was established.
func (c *FTP) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Quit()
	c.conn = nil
	return err
}
