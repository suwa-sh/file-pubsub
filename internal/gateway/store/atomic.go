// Package store implements the data-directory file stores defined in
// object-storage-schema.yaml. All writes go through AtomicWrite: a temp name
// in the same directory, fsync, then rename (SR-001, LR-301), so a file under
// its final name is always complete.
package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const tempSuffix = ".tmp"

// WriteFileAtomic writes r to dst+".tmp", fsyncs, then renames to dst.
func WriteFileAtomic(dst string, r io.Reader, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	tmp := dst + tempSuffix
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomic write %s: %w", dst, err)
	}
	return nil
}

// WriteJSONAtomic JSON-encodes v and writes it atomically (manifest / meta files).
func WriteJSONAtomic(dst string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", dst, err)
	}
	return WriteFileAtomic(dst, strings.NewReader(string(data)+"\n"), 0o644)
}

// CopyFileAtomic copies src to dst with AtomicWrite (fan-out / replay / DLQ
// isolation). The source file permissions are preserved.
func CopyFileAtomic(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return WriteFileAtomic(dst, f, info.Mode().Perm())
}

// CleanupTempFiles removes *.tmp files directly under dir, left over from an
// interrupted write. A missing directory is not an error (idempotent restart).
func CleanupTempFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cleanup temp files in %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), tempSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cleanup temp files in %s: %w", dir, err)
		}
	}
	return nil
}

// readJSON decodes the JSON file at path into v.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
