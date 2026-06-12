package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// Manifest はメッセージ単位の配信記録 (manifest/{message_id}.json)。
// フィールド名は object-storage-schema.yaml schemas.manifest_json に厳密に従う。
// manifest は配信状態の唯一の正本である (CTR-003)。
type Manifest struct {
	MessageID         string                 `json:"message_id"`
	Topic             string                 `json:"topic"`
	OriginalFileName  string                 `json:"original_file_name"`
	CollectedAt       time.Time              `json:"collected_at"`
	Status            domain.MessageStatus   `json:"status"`
	ArchivePath       string                 `json:"archive_path,omitempty"`
	SavedAt           *time.Time             `json:"saved_at,omitempty"`
	RetentionDeadline *time.Time             `json:"retention_deadline,omitempty"`
	Subscriptions     []SubscriptionDelivery `json:"subscriptions"`
	RetryCount        int                    `json:"retry_count"`
	DeliveredAt       *time.Time             `json:"delivered_at,omitempty"`
	ReplayRecords     []ReplayRecord         `json:"replay_records,omitempty"`
	DeliveryEvents    []DeliveryEvent        `json:"delivery_events,omitempty"`
}

// SubscriptionDelivery は manifest_json.subscriptions の要素 1 つ。
type SubscriptionDelivery struct {
	Subscription string                    `json:"subscription"`
	Status       domain.SubscriptionStatus `json:"status"`
	DeliveredAt  *time.Time                `json:"delivered_at,omitempty"`
	LastError    string                    `json:"last_error,omitempty"`
}

// ReplayRecord は manifest_json.replay_records の要素 1 つ (SP-102)。
type ReplayRecord struct {
	ReplayedAt          time.Time `json:"replayed_at"`
	TargetSubscriptions []string  `json:"target_subscriptions"`
	Result              string    `json:"result"`
}

// DeliveryEvent は追記専用の監査ログ manifest_json.delivery_events の要素 1 つ
// (NFR E.7.1.1)。
type DeliveryEvent struct {
	At           time.Time `json:"at"`
	Subscription string    `json:"subscription,omitempty"`
	EventType    string    `json:"event_type"`
	Detail       string    `json:"detail,omitempty"`
}

// NewManifest は collected 状態の初期記録を生成する (Collect UC)。
func NewManifest(msg domain.Message) *Manifest {
	return &Manifest{
		MessageID:        msg.MessageID,
		Topic:            msg.Topic,
		OriginalFileName: msg.OriginalFileName,
		CollectedAt:      msg.CollectedAt,
		Status:           domain.StatusCollected,
		Subscriptions:    []SubscriptionDelivery{},
	}
}

// SetSubscriptionState は subscription 1 件の現在の配信状態を記録し、以前の現在
// 状態を置き換える (配信履歴は DeliveryEvents に残す)。
func (m *Manifest) SetSubscriptionState(name string, status domain.SubscriptionStatus, deliveredAt *time.Time, lastError string) {
	for i := range m.Subscriptions {
		if m.Subscriptions[i].Subscription == name {
			m.Subscriptions[i].Status = status
			m.Subscriptions[i].DeliveredAt = deliveredAt
			m.Subscriptions[i].LastError = lastError
			return
		}
	}
	m.Subscriptions = append(m.Subscriptions, SubscriptionDelivery{
		Subscription: name,
		Status:       status,
		DeliveredAt:  deliveredAt,
		LastError:    lastError,
	})
}

// SubscriptionStates は subscription 名から現在の配信状態へのマップを返す。
// 冪等再配信判定 (SR-003) の入力となる。
func (m *Manifest) SubscriptionStates() map[string]domain.SubscriptionStatus {
	states := make(map[string]domain.SubscriptionStatus, len(m.Subscriptions))
	for _, s := range m.Subscriptions {
		states[s.Subscription] = s.Status
	}
	return states
}

// AppendEvent は監査ログに配信イベントを 1 件追記する。
func (m *Manifest) AppendEvent(e DeliveryEvent) {
	m.DeliveryEvents = append(m.DeliveryEvents, e)
}

// AppendReplay は replay 記録を 1 件追記する (SP-102)。
func (m *Manifest) AppendReplay(r ReplayRecord) {
	m.ReplayRecords = append(m.ReplayRecords, r)
}

// ManifestStore は manifest/{message_id}.json の読み書きを行う。
type ManifestStore struct {
	dir string
}

// NewManifestStore は dataDir/manifest を起点とするストアを返す。
func NewManifestStore(dataDir string) *ManifestStore {
	return &ManifestStore{dir: filepath.Join(dataDir, "manifest")}
}

func (s *ManifestStore) path(messageID string) string {
	return filepath.Join(s.dir, messageID+".json")
}

// Get は messageID の manifest を読み込む。
func (s *ManifestStore) Get(messageID string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(s.path(messageID), &m); err != nil {
		return nil, fmt.Errorf("get manifest %s: %w", messageID, err)
	}
	return &m, nil
}

// Put は manifest を AtomicWrite で永続化する。
func (s *ManifestStore) Put(m *Manifest) error {
	if m.MessageID == "" {
		return fmt.Errorf("put manifest: message_id is empty")
	}
	if m.Subscriptions == nil {
		m.Subscriptions = []SubscriptionDelivery{}
	}
	if err := WriteJSONAtomic(s.path(m.MessageID), m); err != nil {
		return fmt.Errorf("put manifest %s: %w", m.MessageID, err)
	}
	return nil
}

// List はすべての manifest を message_id 順 (ファイル名昇順, SR-005) で読み込む。
func (s *ManifestStore) List() ([]*Manifest, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list manifests: %w", err)
	}
	var manifests []*Manifest
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		m, err := s.Get(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, m)
	}
	return manifests, nil
}
