package source

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// hostPort joins the configured host with the explicit port, falling back to
// the protocol default (ftp 21, sftp/scp 22) when port is 0 (config.go Source).
func hostPort(host string, port, defaultPort int) string {
	if port == 0 {
		port = defaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// writeTempAndRename streams src into destDir/name with the shared download
// protocol of every connector (LR-303): write under a temp name, run finish
// (e.g. confirm the FTP transfer-complete reply), verify the copied size
// against want (-1 skips the check when the source size is unknown), then
// rename to the final name. On any failure the temp file is removed so an
// interrupted download never flows downstream.
func writeTempAndRename(name, destDir string, src io.Reader, want int64, finish func() error) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(destDir, name)
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	written, err := io.Copy(f, src)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && finish != nil {
		err = finish()
	}
	if err == nil && want >= 0 && written != want {
		err = fmt.Errorf("size mismatch: copied %d bytes, source has %d", written, want)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}
