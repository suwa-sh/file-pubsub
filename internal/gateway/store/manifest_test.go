package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// sampleMessage はテスト用の Message を生成するヘルパー。
func sampleMessage() domain.Message {
	return domain.NewMessage(time.Date(2026, 6, 12, 9, 30, 1, 0, time.UTC), "orders", "sales.csv")
}

func TestManifestStore_全フィールドを書いた場合_PutとGetで往復しても内容が一致すること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)

	m := NewManifest(sampleMessage())
	savedAt := time.Date(2026, 6, 12, 9, 30, 5, 0, time.UTC)
	deadline := domain.RetentionDeadline(savedAt, 90)
	m.Status = domain.StatusArchived
	m.ArchivePath = "archive/orders/" + m.MessageID
	m.SavedAt = &savedAt
	m.RetentionDeadline = &deadline
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &savedAt, "")
	m.SetSubscriptionState("next", domain.SubscriptionFailed, nil, "permission denied (write)")
	m.AppendEvent(DeliveryEvent{At: savedAt, Subscription: "next", EventType: "delivery_failed", Detail: "permission denied"})
	m.AppendReplay(ReplayRecord{ReplayedAt: savedAt, TargetSubscriptions: []string{"next"}, Result: "delivered"})

	// Act
	if err := s.Put(m); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(m.MessageID)

	// Assert
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MessageID != m.MessageID || got.Topic != "orders" || got.OriginalFileName != "sales.csv" {
		t.Errorf("identity fields mismatch: %+v", got)
	}
	if got.Status != domain.StatusArchived || got.ArchivePath != m.ArchivePath {
		t.Errorf("archive fields mismatch: %+v", got)
	}
	if got.RetentionDeadline == nil || !got.RetentionDeadline.Equal(deadline) {
		t.Errorf("retention_deadline mismatch: %v", got.RetentionDeadline)
	}
	states := got.SubscriptionStates()
	if states["current"] != domain.SubscriptionDelivered || states["next"] != domain.SubscriptionFailed {
		t.Errorf("subscription states mismatch: %v", states)
	}
	if len(got.DeliveryEvents) != 1 || got.DeliveryEvents[0].EventType != "delivery_failed" {
		t.Errorf("delivery events mismatch: %+v", got.DeliveryEvents)
	}
	if len(got.ReplayRecords) != 1 || got.ReplayRecords[0].Result != "delivered" {
		t.Errorf("replay records mismatch: %+v", got.ReplayRecords)
	}
}

func TestManifestStore_Putした場合_JSONフィールド名がスキーマに従うこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	m := NewManifest(sampleMessage())
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, nil, "")

	// Act
	if err := s.Put(m); err != nil {
		t.Fatal(err)
	}

	// Assert
	raw, err := os.ReadFile(filepath.Join(dataDir, "manifest", m.MessageID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// schemas.manifest_json: non-nullable フィールドはこの名前で存在しなければならない
	for _, key := range []string{"message_id", "topic", "original_file_name", "collected_at", "status", "subscriptions", "retry_count"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("manifest JSON missing required field %q", key)
		}
	}
	subs, ok := doc["subscriptions"].([]any)
	if !ok || len(subs) != 1 {
		t.Fatalf("subscriptions must be an array: %v", doc["subscriptions"])
	}
	sub := subs[0].(map[string]any)
	if sub["subscription"] != "current" || sub["status"] != "delivered" {
		t.Errorf("subscription element mismatch: %v", sub)
	}
}

func TestManifestStore_Put_subscriptionsがnilの場合_空配列としてシリアライズされること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	m := NewManifest(sampleMessage())
	m.Subscriptions = nil

	// Act
	if err := s.Put(m); err != nil {
		t.Fatal(err)
	}

	// Assert
	raw, _ := os.ReadFile(filepath.Join(dataDir, "manifest", m.MessageID+".json"))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["subscriptions"].([]any); !ok {
		t.Errorf("subscriptions must serialize as an array (nullable: false), got %v", doc["subscriptions"])
	}
}

func TestManifestStore_Get_対象が無い場合_エラーになること(t *testing.T) {
	// Arrange
	s := NewManifestStore(t.TempDir())

	// Act & Assert
	if _, err := s.Get("nope"); err == nil {
		t.Error("missing manifest must return an error")
	}
}

func TestManifestStore_List_複数manifestがある場合_messageID昇順で返ること(t *testing.T) {
	// Arrange
	s := NewManifestStore(t.TempDir())
	for _, id := range []string{"b", "a"} {
		m := NewManifest(domain.Message{MessageID: id, Topic: "orders", OriginalFileName: id, CollectedAt: time.Now()})
		if err := s.Put(m); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	all, err := s.List()

	// Assert
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].MessageID != "a" || all[1].MessageID != "b" {
		t.Errorf("List order/content mismatch: %v", all)
	}
}

func TestManifestStore_List_ディレクトリが空の場合_0件で返ること(t *testing.T) {
	// Arrange
	s := NewManifestStore(t.TempDir())

	// Act
	all, err := s.List()

	// Assert
	if err != nil || len(all) != 0 {
		t.Errorf("empty store: got %v, %v", all, err)
	}
}

func TestSetSubscriptionState_同じsubscriptionを再設定した場合_要素が増えず置き換わること(t *testing.T) {
	// Arrange
	m := NewManifest(sampleMessage())
	m.SetSubscriptionState("next", domain.SubscriptionFailed, nil, "boom")
	at := time.Now()

	// Act
	m.SetSubscriptionState("next", domain.SubscriptionDelivered, &at, "")

	// Assert
	if len(m.Subscriptions) != 1 {
		t.Fatalf("must update in place, got %d entries", len(m.Subscriptions))
	}
	if m.Subscriptions[0].Status != domain.SubscriptionDelivered || m.Subscriptions[0].LastError != "" {
		t.Errorf("state not replaced: %+v", m.Subscriptions[0])
	}
}
