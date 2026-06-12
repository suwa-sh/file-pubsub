package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// processedFile is processed/{topic}.json (schemas.processed_json): the
// append-only record preventing re-collection when the source keeps original
// files (copy handling, SP-004).
type processedFile struct {
	Topic   string           `json:"topic"`
	Entries []ProcessedEntry `json:"entries"`
}

// ProcessedEntry is one element of processed_json.entries. A source file is
// identified by name + mtime (UnixNano) + size, so a same-name re-output with
// different content (mtime or size changed) is re-collected instead of being
// skipped forever.
type ProcessedEntry struct {
	SourceFileIdentifier string    `json:"source_file_identifier"`
	ModTimeUnixNano      int64     `json:"mtime_unixnano"`
	Size                 int64     `json:"size"`
	ProcessedAt          time.Time `json:"processed_at"`
}

// matches reports whether the entry records exactly this source file state.
func (e ProcessedEntry) matches(name string, modTime time.Time, size int64) bool {
	return e.SourceFileIdentifier == name && e.ModTimeUnixNano == modTime.UnixNano() && e.Size == size
}

// ProcessedStore reads and writes processed/{topic}.json.
type ProcessedStore struct {
	dir string
}

// NewProcessedStore roots the store at dataDir/processed.
func NewProcessedStore(dataDir string) *ProcessedStore {
	return &ProcessedStore{dir: filepath.Join(dataDir, "processed")}
}

func (s *ProcessedStore) path(topic string) string {
	return filepath.Join(s.dir, topic+".json")
}

func (s *ProcessedStore) load(topic string) (*processedFile, error) {
	var f processedFile
	if err := readJSON(s.path(topic), &f); err != nil {
		if os.IsNotExist(err) {
			return &processedFile{Topic: topic, Entries: []ProcessedEntry{}}, nil
		}
		return nil, fmt.Errorf("load processed %s: %w", topic, err)
	}
	return &f, nil
}

// IsProcessed reports whether this exact source file state (name + mtime +
// size) is already recorded.
func (s *ProcessedStore) IsProcessed(topic, name string, modTime time.Time, size int64) (bool, error) {
	f, err := s.load(topic)
	if err != nil {
		return false, err
	}
	for _, e := range f.Entries {
		if e.matches(name, modTime, size) {
			return true, nil
		}
	}
	return false, nil
}

// MarkProcessed appends a processed record with AtomicWrite. Until the record
// is persisted the file stays unprocessed (safe side: re-collection candidate).
// Marking an already recorded file state is a no-op (idempotent).
func (s *ProcessedStore) MarkProcessed(topic, name string, modTime time.Time, size int64, at time.Time) error {
	f, err := s.load(topic)
	if err != nil {
		return err
	}
	for _, e := range f.Entries {
		if e.matches(name, modTime, size) {
			return nil
		}
	}
	f.Entries = append(f.Entries, ProcessedEntry{
		SourceFileIdentifier: name,
		ModTimeUnixNano:      modTime.UnixNano(),
		Size:                 size,
		ProcessedAt:          at,
	})
	if err := WriteJSONAtomic(s.path(topic), f); err != nil {
		return fmt.Errorf("mark processed %s: %w", topic, err)
	}
	return nil
}

// Entries returns all processed records of the topic.
func (s *ProcessedStore) Entries(topic string) ([]ProcessedEntry, error) {
	f, err := s.load(topic)
	if err != nil {
		return nil, err
	}
	return f.Entries, nil
}
