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

// SCP collects files over plain SSH sessions for servers whose SSH daemon
// does not enable the SFTP subsystem. List / Fetch / Remove run POSIX shell
// commands (find + stat / cat / rm) in one session each, so the remote
// account needs a shell and GNU coreutils/findutils (the Linux mainstream;
// stat -c is not BSD-compatible). File names containing a newline are not
// supported by the list parsing. Size and mtime come from stat, so the
// stability check works the same as with the other connectors.
type SCP struct {
	opts Options
	ssh  *ssh.Client
}

// NewSCP returns an SCP connector. The connection is established lazily on
// first use so construction itself never touches the network.
func NewSCP(o Options) *SCP { return &SCP{opts: o} }

// connect dials the SSH connection once, reusing it afterwards.
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

// run executes one command in a fresh SSH session and returns its stdout.
// Stderr is folded into the error so collect_failed logs carry the cause.
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

// shellQuote wraps s in single quotes for safe use in a remote shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// List returns the regular files directly under the source directory using
// find + stat ("size mtime name" per line).
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

// parseSCPList parses "size mtime ./name" stat lines into FileInfo.
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

// Fetch streams the remote file with cat into destDir under a temp name,
// verifies the copied size against a fresh stat and renames to the final
// name (LR-303). The size is read in the same session batch as the cat, so a
// concurrent rewrite is caught as a mismatch (the file is already
// stability-checked before Fetch is called).
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

// Remove deletes the original file after archive save success (delete handling).
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

// Close shuts the SSH connection if one was opened.
func (c *SCP) Close() error {
	if c.ssh == nil {
		return nil
	}
	err := c.ssh.Close()
	c.ssh = nil
	return err
}
