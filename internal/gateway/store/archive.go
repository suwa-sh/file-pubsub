package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ArchiveStore manages the collect work area (work/collect/{topic}/{message_id})
// and the topic archive (archive/{topic}/{message_id}). Every message must be
// archived before fan-out starts (SP-001); message_id file names guarantee a
// same-name re-export never overwrites history (SR-002).
type ArchiveStore struct {
	workDir    string
	archiveDir string
}

// NewArchiveStore roots the store at dataDir/work/collect and dataDir/archive.
func NewArchiveStore(dataDir string) *ArchiveStore {
	return &ArchiveStore{
		workDir:    filepath.Join(dataDir, "work", "collect"),
		archiveDir: filepath.Join(dataDir, "archive"),
	}
}

// WorkPath returns work/collect/{topic}/{message_id}.
func (s *ArchiveStore) WorkPath(topic, messageID string) string {
	return filepath.Join(s.workDir, topic, messageID)
}

// ArchivePath returns archive/{topic}/{message_id}.
func (s *ArchiveStore) ArchivePath(topic, messageID string) string {
	return filepath.Join(s.archiveDir, topic, messageID)
}

// PutWork stores collected content into the work area with AtomicWrite
// (temp name {message_id}.tmp, then rename — LR-303).
func (s *ArchiveStore) PutWork(topic, messageID string, r io.Reader) error {
	if err := WriteFileAtomic(s.WorkPath(topic, messageID), r, 0o644); err != nil {
		return fmt.Errorf("put work %s/%s: %w", topic, messageID, err)
	}
	return nil
}

// Promote copies the work file into the archive with AtomicWrite, then removes
// the work file (the archive is the source of truth afterwards). Re-running
// after an interruption overwrites the same archive path idempotently.
func (s *ArchiveStore) Promote(topic, messageID string) error {
	if err := CopyFileAtomic(s.WorkPath(topic, messageID), s.ArchivePath(topic, messageID)); err != nil {
		return fmt.Errorf("promote %s/%s to archive: %w", topic, messageID, err)
	}
	if err := os.Remove(s.WorkPath(topic, messageID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("promote %s/%s: remove work file: %w", topic, messageID, err)
	}
	return nil
}

// Open opens the archived file for reading (fan-out / retry / replay source).
func (s *ArchiveStore) Open(topic, messageID string) (io.ReadCloser, error) {
	f, err := os.Open(s.ArchivePath(topic, messageID))
	if err != nil {
		return nil, fmt.Errorf("open archive %s/%s: %w", topic, messageID, err)
	}
	return f, nil
}

// Exists reports whether the archive file is present under its final name.
func (s *ArchiveStore) Exists(topic, messageID string) (bool, error) {
	_, err := os.Stat(s.ArchivePath(topic, messageID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat archive %s/%s: %w", topic, messageID, err)
}

// ListMessageIDs returns the archived message_ids of a topic in ascending
// order (retention scan input). Temp names are ignored.
func (s *ArchiveStore) ListMessageIDs(topic string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.archiveDir, topic))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list archive %s: %w", topic, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), tempSuffix) {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

// Delete removes one expired archive file (retention deletion, SP-006).
// A missing file is not an error (idempotent re-run).
func (s *ArchiveStore) Delete(topic, messageID string) error {
	if err := os.Remove(s.ArchivePath(topic, messageID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete archive %s/%s: %w", topic, messageID, err)
	}
	return nil
}

// CleanupWorkTempFiles removes leftover *.tmp files in the topic work area
// (idempotent restart after an interrupted download).
func (s *ArchiveStore) CleanupWorkTempFiles(topic string) error {
	return CleanupTempFiles(filepath.Join(s.workDir, topic))
}
