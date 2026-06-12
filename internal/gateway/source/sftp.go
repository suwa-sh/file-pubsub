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

// sshDefaultPort is the SSH default for both sftp and scp (config port 0).
const sshDefaultPort = 22

// sshTimeout bounds the TCP dial and the SSH handshake.
const sshTimeout = 30 * time.Second

// sshClientConfig builds the SSH client configuration shared by the sftp and
// scp connectors from the source auth settings (E-005): key file auth (with
// the password reused as the passphrase when the key is encrypted) and/or
// password auth.
//
// Security constraint: host key verification is intentionally skipped with
// ssh.InsecureIgnoreHostKey(). The configuration schema keeps no host-key
// material (config.go Auth), and the target legacy hosts rarely publish
// managed known_hosts. The trade-off — a man-in-the-middle on the network
// path can impersonate the source server — is documented in the README
// security notes; run file-pubsub next to the source on a trusted segment.
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // see the constraint comment above
		Timeout:         sshTimeout,
	}, nil
}

// dialSSH opens the SSH client connection shared by the sftp and scp connectors.
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

// SFTP collects files from a directory on an SSH server via the SFTP
// subsystem. Prefer this connector over ftp (encrypted) and scp (needs a
// shell) when the server supports it.
type SFTP struct {
	opts Options
	ssh  *ssh.Client
	sftp *sftp.Client
}

// NewSFTP returns an SFTP connector. The connection is established lazily on
// first use so construction itself never touches the network.
func NewSFTP(o Options) *SFTP { return &SFTP{opts: o} }

// connect dials and opens the SFTP subsystem once, reusing it afterwards.
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

// List returns the files (name, size, mtime) in the source directory.
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

// Fetch downloads name into destDir under a temp name, verifies the copied
// size against the remote stat and renames to the final name (LR-303).
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

// Remove deletes the original file after archive save success (delete handling).
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

// Close shuts the SFTP subsystem and the SSH connection if they were opened.
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
