package source

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// hostPort は設定されたホストと明示ポートを結合する。port が 0 のときは
// プロトコル既定値 (ftp 21, sftp/scp 22) にフォールバックする (config.go Source)。
func hostPort(host string, port, defaultPort int) string {
	if port == 0 {
		port = defaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// writeTempAndRename は全コネクタ共通のダウンロードプロトコル (LR-303) で src を
// destDir/name に書き出す: 一時名で書き込み、finish を実行し (例: FTP の転送完了
// 応答の確認)、コピー後のサイズを want と突き合わせ (-1 はソースサイズ不明として
// 検証をスキップ)、最終名にリネームする。失敗時は一時ファイルを削除するため、
// 中断されたダウンロードが下流に流れることはない。
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
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}
