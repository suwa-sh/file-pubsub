package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DLQMeta は dlq/{topic}/{message_id}.meta.json (schemas.dlq_meta_json) を表す:
// オペレータが replay するか破棄するかを判断する根拠となる。
type DLQMeta struct {
	MessageID       string    `json:"message_id"`
	Topic           string    `json:"topic"`
	IsolationReason string    `json:"isolation_reason"`
	FailureCount    int       `json:"failure_count"`
	IsolatedAt      time.Time `json:"isolated_at"`
}

const dlqMetaSuffix = ".meta.json"

// DLQStore は dlq/{topic}/{message_id} (+ .meta.json) を管理する: リトライ上限を
// 超えて隔離されたメッセージ (SR-004)。自動削除はしない。
type DLQStore struct {
	dir string
}

// NewDLQStore は dataDir/dlq を起点とするストアを返す。
func NewDLQStore(dataDir string) *DLQStore {
	return &DLQStore{dir: filepath.Join(dataDir, "dlq")}
}

// FilePath は dlq/{topic}/{message_id} を返す。
func (s *DLQStore) FilePath(topic, messageID string) string {
	return filepath.Join(s.dir, topic, messageID)
}

func (s *DLQStore) metaPath(topic, messageID string) string {
	return s.FilePath(topic, messageID) + dlqMetaSuffix
}

// Isolate はアーカイブファイル srcPath を DLQ にコピーし、その meta を AtomicWrite
// で書き出す。再実行は同じパスを上書きする (冪等、二重隔離なし)。
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

// ReadMeta は DLQ メッセージ 1 件の隔離メタデータを読み込む。
func (s *DLQStore) ReadMeta(topic, messageID string) (*DLQMeta, error) {
	var meta DLQMeta
	if err := readJSON(s.metaPath(topic, messageID), &meta); err != nil {
		return nil, fmt.Errorf("read dlq meta %s/%s: %w", topic, messageID, err)
	}
	return &meta, nil
}

// List は topic 内の全 DLQ メッセージの隔離メタデータを message_id 昇順で返す。
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
