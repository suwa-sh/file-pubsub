package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// processedFile は processed/{topic}.json (schemas.processed_json) を表す:
// ソースが元ファイルを残す場合 (copy 扱い, SP-004) の再収集を防ぐ追記専用記録。
type processedFile struct {
	Topic   string           `json:"topic"`
	Entries []ProcessedEntry `json:"entries"`
}

// ProcessedEntry は processed_json.entries の要素 1 つ。ソースファイルは
// 名前 + mtime (UnixNano) + サイズで識別するため、同名でも内容が変わった再出力
// (mtime またはサイズが変化) は永久にスキップされず再収集される。
type ProcessedEntry struct {
	SourceFileIdentifier string    `json:"source_file_identifier"`
	ModTimeUnixNano      int64     `json:"mtime_unixnano"`
	Size                 int64     `json:"size"`
	ProcessedAt          time.Time `json:"processed_at"`
}

// matches はこのエントリがちょうどこのソースファイル状態を記録しているかどうかを返す。
func (e ProcessedEntry) matches(name string, modTime time.Time, size int64) bool {
	return e.SourceFileIdentifier == name && e.ModTimeUnixNano == modTime.UnixNano() && e.Size == size
}

// ProcessedStore は processed/{topic}.json の読み書きを行う。
type ProcessedStore struct {
	dir string
}

// NewProcessedStore は dataDir/processed を起点とするストアを返す。
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

// IsProcessed はちょうどこのソースファイル状態 (名前 + mtime + サイズ) が記録済み
// かどうかを返す。
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

// MarkProcessed は処理済み記録を AtomicWrite で追記する。記録が永続化されるまで
// ファイルは未処理のまま (安全側: 再収集候補)。記録済みのファイル状態への mark は
// no-op (冪等)。
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

// Entries は topic のすべての処理済み記録を返す。
func (s *ProcessedStore) Entries(topic string) ([]ProcessedEntry, error) {
	f, err := s.load(topic)
	if err != nil {
		return nil, err
	}
	return f.Entries, nil
}
