package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DLQMeta is dlq/{topic}/{message_id}.meta.json (schemas.dlq_meta_json): the
// basis for the operator's decision to replay or discard.
type DLQMeta struct {
	MessageID       string    `json:"message_id"`
	Topic           string    `json:"topic"`
	IsolationReason string    `json:"isolation_reason"`
	FailureCount    int       `json:"failure_count"`
	IsolatedAt      time.Time `json:"isolated_at"`
}

const dlqMetaSuffix = ".meta.json"

// DLQStore manages dlq/{topic}/{message_id} (+ .meta.json): messages isolated
// after exceeding the retry limit (SR-004). No automatic deletion.
type DLQStore struct {
	dir string
}

// NewDLQStore roots the store at dataDir/dlq.
func NewDLQStore(dataDir string) *DLQStore {
	return &DLQStore{dir: filepath.Join(dataDir, "dlq")}
}

// FilePath returns dlq/{topic}/{message_id}.
func (s *DLQStore) FilePath(topic, messageID string) string {
	return filepath.Join(s.dir, topic, messageID)
}

func (s *DLQStore) metaPath(topic, messageID string) string {
	return s.FilePath(topic, messageID) + dlqMetaSuffix
}

// Isolate copies the archive file srcPath into the DLQ and writes its meta
// with AtomicWrite. Re-running overwrites the same paths (idempotent, no
// double isolation).
func (s *DLQStore) Isolate(srcPath string, meta DLQMeta) error {
	if meta.MessageID == "" || meta.Topic == "" {
		return fmt.Errorf("isolate to dlq: message_id and topic are required")
	}
	if err := CopyFileAtomic(srcPath, s.FilePath(meta.Topic, meta.MessageID)); err != nil {
		return fmt.Errorf("isolate %s/%s to dlq: %w", meta.Topic, meta.MessageID, err)
	}
	if err := WriteJSONAtomic(s.metaPath(meta.Topic, meta.MessageID), meta); err != nil {
		return fmt.Errorf("isolate %s/%s to dlq: write meta: %w", meta.Topic, meta.MessageID, err)
	}
	return nil
}

// ReadMeta loads the isolation metadata of one DLQ message.
func (s *DLQStore) ReadMeta(topic, messageID string) (*DLQMeta, error) {
	var meta DLQMeta
	if err := readJSON(s.metaPath(topic, messageID), &meta); err != nil {
		return nil, fmt.Errorf("read dlq meta %s/%s: %w", topic, messageID, err)
	}
	return &meta, nil
}

// List returns the isolation metadata of every DLQ message in a topic, sorted
// by message_id ascending.
func (s *DLQStore) List(topic string) ([]DLQMeta, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, topic))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list dlq %s: %w", topic, err)
	}
	var metas []DLQMeta
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, dlqMetaSuffix) {
			continue
		}
		meta, err := s.ReadMeta(topic, strings.TrimSuffix(name, dlqMetaSuffix))
		if err != nil {
			return nil, err
		}
		metas = append(metas, *meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].MessageID < metas[j].MessageID })
	return metas, nil
}
