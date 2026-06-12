// Package source provides the collection connector interface. It is the only
// interface in the system (LP-301, CLP-001): local / ftp / sftp / scp
// connectors are interchangeable behind it and downstream stages never depend
// on the source type.
package source

import (
	"context"
	"fmt"
	"time"
)

// FileInfo is one source file observation (name, size, mtime) used by the
// stability check.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Connector is the common collection connector interface (C-01).
type Connector interface {
	// List returns the files (name, size, mtime) in the source directory.
	List(ctx context.Context) ([]FileInfo, error)
	// Fetch downloads name into destDir under a temp name, verifies the copy,
	// renames it to its final name (LR-303) and returns the local path.
	Fetch(ctx context.Context, name, destDir string) (string, error)
	// Remove deletes the original file. Call only after archive save success
	// is confirmed (delete handling, LR-303).
	Remove(ctx context.Context, name string) error
	Close() error
}

// Options selects and configures a connector implementation.
type Options struct {
	Type      string // local / ftp / sftp / scp
	Host      string
	Port      int
	Directory string
	Username  string
	Password  string
	KeyFile   string
}

// New returns the connector for o.Type. Remote connectors (ftp / sftp / scp)
// are planned for Phase 3.
func New(o Options) (Connector, error) {
	switch o.Type {
	case "local":
		return NewLocal(o.Directory), nil
	case "ftp", "sftp", "scp":
		return nil, fmt.Errorf("source type %q is not implemented yet (Phase 3)", o.Type)
	default:
		return nil, fmt.Errorf("unknown source type %q", o.Type)
	}
}
