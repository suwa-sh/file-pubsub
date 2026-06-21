package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestManifestStore_PutMerged_既存deliveredを保持したまま別Subscriptionを追記できること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	at := time.Date(2026, 6, 12, 9, 30, 5, 0, time.UTC)
	base.SetSubscriptionState("current", domain.SubscriptionDelivered, &at, "")
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}

	// Act
	upd := NewManifest(sampleMessage())
	upd.SetSubscriptionState("next", domain.SubscriptionDelivered, &at, "")
	got, err := s.PutMerged(upd)

	// Assert
	if err != nil {
		t.Fatalf("PutMerged: %v", err)
	}
	states := got.SubscriptionStates()
	if states["current"] != domain.SubscriptionDelivered {
		t.Errorf("既存 current=delivered が保持されていない: %v", states)
	}
	if states["next"] != domain.SubscriptionDelivered {
		t.Errorf("追記 next=delivered が記録されていない: %v", states)
	}
}

func TestManifestStore_PutMerged_failedを再配信成功でdeliveredへ更新できること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	base.SetSubscriptionState("next", domain.SubscriptionFailed, nil, "permission denied")
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}

	// Act
	upd := NewManifest(sampleMessage())
	at := time.Now()
	upd.SetSubscriptionState("next", domain.SubscriptionDelivered, &at, "")
	got, err := s.PutMerged(upd)

	// Assert
	if err != nil {
		t.Fatalf("PutMerged: %v", err)
	}
	if got.SubscriptionStates()["next"] != domain.SubscriptionDelivered {
		t.Errorf("failed が delivered へ昇格していない: %v", got.SubscriptionStates())
	}
}

func TestManifestStore_PutMerged_deliveredをfailedで上書きしないこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	at := time.Date(2026, 6, 12, 9, 30, 5, 0, time.UTC)
	base.SetSubscriptionState("current", domain.SubscriptionDelivered, &at, "")
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}

	// Act
	upd := NewManifest(sampleMessage())
	upd.SetSubscriptionState("current", domain.SubscriptionFailed, nil, "boom")
	got, err := s.PutMerged(upd)

	// Assert
	if err != nil {
		t.Fatalf("PutMerged: %v", err)
	}
	if got.SubscriptionStates()["current"] != domain.SubscriptionDelivered {
		t.Errorf("決着状態 delivered が failed で後退した: %v", got.SubscriptionStates())
	}
}

func TestManifestStore_PutMerged_dlqをfailedで上書きしないこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	base.SetSubscriptionState("current", domain.SubscriptionDLQ, nil, "exhausted")
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}

	// Act
	upd := NewManifest(sampleMessage())
	upd.SetSubscriptionState("current", domain.SubscriptionFailed, nil, "boom")
	got, err := s.PutMerged(upd)

	// Assert
	if err != nil {
		t.Fatalf("PutMerged: %v", err)
	}
	if got.SubscriptionStates()["current"] != domain.SubscriptionDLQ {
		t.Errorf("決着状態 dlq が failed で後退した: %v", got.SubscriptionStates())
	}
}

func TestManifestStore_PutMerged_2回続けて別Subscriptionを更新した場合_両方の決着状態を取りこぼさないこと(t *testing.T) {
	// Arrange (lost update 回避: 逐次の read-merge-write で両決着が保持されることを検証)
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	if err := s.Put(NewManifest(sampleMessage())); err != nil {
		t.Fatal(err)
	}
	at := time.Now()

	// Act
	a := NewManifest(sampleMessage())
	a.SetSubscriptionState("current", domain.SubscriptionDelivered, &at, "")
	if _, err := s.PutMerged(a); err != nil {
		t.Fatalf("PutMerged a: %v", err)
	}
	b := NewManifest(sampleMessage())
	b.SetSubscriptionState("next", domain.SubscriptionDelivered, &at, "")
	got, err := s.PutMerged(b)

	// Assert
	if err != nil {
		t.Fatalf("PutMerged b: %v", err)
	}
	states := got.SubscriptionStates()
	if states["current"] != domain.SubscriptionDelivered || states["next"] != domain.SubscriptionDelivered {
		t.Errorf("lost update: 両方の delivered が保持されていない: %v", states)
	}
}

func TestManifestStore_PutMerged_revisionが更新ごとに増えること(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	if err := s.Put(NewManifest(sampleMessage())); err != nil {
		t.Fatal(err)
	}

	// Act
	first, err := s.PutMerged(NewManifest(sampleMessage()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PutMerged(NewManifest(sampleMessage()))

	// Assert
	if err != nil {
		t.Fatalf("PutMerged: %v", err)
	}
	if second.Revision != first.Revision+1 {
		t.Errorf("revision が +1 されていない: first=%d second=%d", first.Revision, second.Revision)
	}
}

func TestManifestStore_PutMerged_TTL内の更新ロックが残存している場合_リトライ上限超過でfailclosedになること(t *testing.T) {
	// Arrange (他者がロック保持中を模擬: lock ファイルを先に O_CREATE|O_EXCL で作っておく。
	// mtime は now のため stale TTL 内 = 回収対象外。取得競合中とみなし fail-closed が正)。
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}
	lockPath := s.path(base.MessageID) + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("prepare lock: %v", err)
	}
	_ = f.Close()

	// Act
	_, err = s.PutMerged(NewManifest(sampleMessage()))

	// Assert
	if err == nil {
		t.Fatal("TTL 内のロック残存時は fail-closed のエラーになるべき")
	}
}

func TestManifestStore_PutMerged_残存ロックがstaleTTLを超過している場合_回収して更新できること(t *testing.T) {
	// Arrange (holder クラッシュで残った stale ロックを模擬: lock を作り mtime を
	// stale TTL より十分過去へ backdate する。fix D: 次回更新で吸収されるべき)。
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}
	lockPath := s.path(base.MessageID) + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("prepare lock: %v", err)
	}
	_ = f.Close()
	stale := time.Now().Add(-2 * manifestLockStaleAfter)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("backdate lock: %v", err)
	}

	// Act
	upd := NewManifest(sampleMessage())
	upd.SetSubscriptionState("current", domain.SubscriptionDelivered, nil, "")
	got, err := s.PutMerged(upd)

	// Assert
	if err != nil {
		t.Fatalf("stale ロックは回収して更新できるべき: %v", err)
	}
	if got.SubscriptionStates()["current"] != domain.SubscriptionDelivered {
		t.Errorf("回収後の更新が反映されていない: %v", got.SubscriptionStates())
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Errorf("更新完了後はロックが解放されているべき: stat=%v", statErr)
	}
}

func TestManifestStore_acquireLock_releaseは別writerに奪われたlockを削除しないこと(t *testing.T) {
	// Arrange: A が lock を取得した後、stale 回収等で別 writer B が lock を奪った状況を、
	// lock ファイルの中身を B の token で上書きして模擬する (owner token によるガード検証)。
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	if err := s.Put(NewManifest(sampleMessage())); err != nil {
		t.Fatal(err)
	}
	releaseA, _, err := s.acquireLock(sampleMessage().MessageID)
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	lockPath := s.path(sampleMessage().MessageID) + manifestLockSuffix
	if err := os.WriteFile(lockPath, []byte("other-writer-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act: A の release は token 不一致のため B の lock を消してはならない。
	releaseA()

	// Assert
	cur, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("B の lock が誤って削除された: %v", err)
	}
	if strings.TrimSpace(string(cur)) != "other-writer-token" {
		t.Errorf("lock の所有者が変わっている: %q", string(cur))
	}
}

func TestManifestStore_Update_renameの直前にlockを奪われた場合_clobberせずfailclosedになること(t *testing.T) {
	// Arrange: Update の staging 書き込み後・rename 前に、別 writer が lock を奪取し manifest を
	// 別内容で書き換える状況をフックで模擬する。token 固有 staging + rename 直前の lock token
	// 確認により、lock を失った writer は rename せず fail-closed になり、奪取側の更新を
	// clobber しないこと (codex [major]: read→remove TOCTOU / staging clobber の防御)。
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	base := NewManifest(sampleMessage())
	if err := s.Put(base); err != nil {
		t.Fatal(err)
	}
	lockPath := s.path(base.MessageID) + manifestLockSuffix
	once := false
	s.beforeUpdateRename = func() {
		if once {
			return
		}
		once = true
		// 別 writer が lock を奪取 (token 差し替え) し、決着状態を書き込んだと模擬する。
		if err := os.WriteFile(lockPath, []byte("takeover-token\n"), 0o644); err != nil {
			t.Fatalf("takeover lock: %v", err)
		}
		winner := NewManifest(sampleMessage())
		winner.Revision = 1
		winner.SetSubscriptionState("current", domain.SubscriptionDelivered, nil, "")
		if err := WriteJSONAtomic(s.path(base.MessageID), winner); err != nil {
			t.Fatalf("winner write: %v", err)
		}
	}

	// Act: lock を失った writer が failed を書こうとする。
	upd := NewManifest(sampleMessage())
	upd.SetSubscriptionState("current", domain.SubscriptionFailed, nil, "boom")
	_, err := s.PutMerged(upd)

	// Assert: lock 喪失で fail-closed (rename しない)。奪取側の delivered が残る。
	if err == nil {
		t.Fatal("lock を失った writer は fail-closed になるべき")
	}
	got, getErr := s.Get(base.MessageID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.SubscriptionStates()["current"] != domain.SubscriptionDelivered {
		t.Fatalf("奪取側の delivered が clobber された: %v", got.SubscriptionStates())
	}
}

func TestManifestStore_Update_mutateを適用しrevisionを増やすこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	if err := s.Put(NewManifest(sampleMessage())); err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := s.Update(sampleMessage().MessageID, func(base *Manifest) error {
		base.Status = domain.StatusArchived
		return nil
	})

	// Assert
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Status != domain.StatusArchived {
		t.Errorf("mutate が反映されていない: status=%s", got.Status)
	}
	if got.Revision != 1 {
		t.Errorf("revision は +1 されるべき: got=%d", got.Revision)
	}
	persisted, _ := s.Get(sampleMessage().MessageID)
	if persisted.Status != domain.StatusArchived {
		t.Errorf("永続化されていない: status=%s", persisted.Status)
	}
}

func TestManifestStore_Update_mutateがErrSkipManifestUpdateを返した場合_書き込まずrevision据え置きで返すこと(t *testing.T) {
	// Arrange
	dataDir := t.TempDir()
	s := NewManifestStore(dataDir)
	if err := s.Put(NewManifest(sampleMessage())); err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := s.Update(sampleMessage().MessageID, func(base *Manifest) error {
		return ErrSkipManifestUpdate
	})

	// Assert
	if err != nil {
		t.Fatalf("ErrSkipManifestUpdate は no-op で nil エラーを返すべき: %v", err)
	}
	if got.Revision != 0 {
		t.Errorf("no-op では revision を据え置くべき: got=%d", got.Revision)
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
