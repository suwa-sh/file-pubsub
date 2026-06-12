package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// Manifest is the per-message delivery record (manifest/{message_id}.json).
// Field names follow object-storage-schema.yaml schemas.manifest_json exactly.
// The manifest is the single source of truth for delivery state (CTR-003).
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

// SubscriptionDelivery is one element of manifest_json.subscriptions.
type SubscriptionDelivery struct {
	Subscription string                    `json:"subscription"`
	Status       domain.SubscriptionStatus `json:"status"`
	DeliveredAt  *time.Time                `json:"delivered_at,omitempty"`
	LastError    string                    `json:"last_error,omitempty"`
}

// ReplayRecord is one element of manifest_json.replay_records (SP-102).
type ReplayRecord struct {
	ReplayedAt          time.Time `json:"replayed_at"`
	TargetSubscriptions []string  `json:"target_subscriptions"`
	Result              string    `json:"result"`
}

// DeliveryEvent is one element of the append-only manifest_json.delivery_events
// audit log (NFR E.7.1.1).
type DeliveryEvent struct {
	At           time.Time `json:"at"`
	Subscription string    `json:"subscription,omitempty"`
	EventType    string    `json:"event_type"`
	Detail       string    `json:"detail,omitempty"`
}

// NewManifest builds the initial collected record (Collect UC).
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

// SetSubscriptionState records the current delivery state of one subscription,
// replacing its previous current state (delivery history goes to DeliveryEvents).
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

// SubscriptionStates maps subscription name to its current delivery status,
// the input of the idempotent-redelivery decision (SR-003).
func (m *Manifest) SubscriptionStates() map[string]domain.SubscriptionStatus {
	states := make(map[string]domain.SubscriptionStatus, len(m.Subscriptions))
	for _, s := range m.Subscriptions {
		states[s.Subscription] = s.Status
	}
	return states
}

// AppendEvent appends one delivery event to the audit log.
func (m *Manifest) AppendEvent(e DeliveryEvent) {
	m.DeliveryEvents = append(m.DeliveryEvents, e)
}

// AppendReplay appends one replay record (SP-102).
func (m *Manifest) AppendReplay(r ReplayRecord) {
	m.ReplayRecords = append(m.ReplayRecords, r)
}

// ManifestStore reads and writes manifest/{message_id}.json.
type ManifestStore struct {
	dir string
}

// NewManifestStore roots the store at dataDir/manifest.
func NewManifestStore(dataDir string) *ManifestStore {
	return &ManifestStore{dir: filepath.Join(dataDir, "manifest")}
}

func (s *ManifestStore) path(messageID string) string {
	return filepath.Join(s.dir, messageID+".json")
}

// Get loads the manifest of messageID.
func (s *ManifestStore) Get(messageID string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(s.path(messageID), &m); err != nil {
		return nil, fmt.Errorf("get manifest %s: %w", messageID, err)
	}
	return &m, nil
}

// Put persists the manifest with AtomicWrite.
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

// List loads every manifest, sorted by message_id (file name ascending, SR-005).
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
