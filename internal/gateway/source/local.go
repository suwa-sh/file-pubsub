package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Local はローカルディレクトリからファイルを収集する。
type Local struct {
	dir string
}

// NewLocal はローカルディレクトリ dir に対するコネクタを返す。
func NewLocal(dir string) *Local {
	return &Local{dir: dir}
}

// List はソースディレクトリ直下の通常ファイルを返す。
func (l *Local) List(ctx context.Context) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", l.dir, err)
	}
	var files []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", l.dir, err)
		}
		files = append(files, FileInfo{Name: info.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return files, nil
}

// Fetch はソースファイルを一時名で destDir にコピーし、コピー後のサイズを
// ソースと突き合わせて検証してから最終名にリネームする (LR-303)。
func (l *Local) Fetch(ctx context.Context, name, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	src, err := os.Open(filepath.Join(l.dir, name))
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	defer func() { _ = src.Close() }()
	srcInfo, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	dst := filepath.Join(destDir, name)
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	written, err := io.Copy(f, src)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written != srcInfo.Size() {
		err = fmt.Errorf("size mismatch: copied %d bytes, source has %d", written, srcInfo.Size())
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	return dst, nil
}

// Remove は archive 保存成功後に元ファイルを削除する (delete 扱い)。
func (l *Local) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.dir, name)); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// Close はローカルコネクタでは解放するものがない。
func (l *Local) Close() error { return nil }

// validateName はソースディレクトリ外へ抜けるファイル名を拒否する。
// List は素のファイル名しか返さない。
func validateName(name string) error {
	if name == "" || name != filepath.Base(name) {
		return fmt.Errorf("invalid file name %q", name)
	}
	return nil
}
