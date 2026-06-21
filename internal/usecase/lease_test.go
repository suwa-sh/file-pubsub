package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
)

// fakeLeaseChecker は HoldsLease の戻り値を制御できるテスト用 LeaseChecker。
// calls で呼び出し回数を数え、held=false / err 設定で lease 喪失・I/O 失敗を模擬する。
type fakeLeaseChecker struct {
	held  bool
	err   error
	calls int
	// loseAfter > 0 の場合、loseAfter 回目以降の呼び出しで held=false を返す
	// (メッセージ処理の途中で lease を失う状況の模擬)。
	loseAfter int
}

func (c *fakeLeaseChecker) HoldsLease() (bool, error) {
	c.calls++
	if c.err != nil {
		return false, c.err
	}
	if c.loseAfter > 0 && c.calls >= c.loseAfter {
		return false, nil
	}
	return c.held, nil
}

func TestFanout_LeaseCheckerがnilの場合_lease確認をスキップして全メッセージを配信すること(t *testing.T) {
	// Arrange (後方互換: 単一インスタンス運用で lease 確認なし)
	e := newEnv(t, config.HandlingDelete)
	m1 := e.seedArchived("orders_1.csv", "a")
	m2 := e.seedArchived("orders_2.csv", "b")
	e.p.Lease = nil

	// Act
	e.p.Fanout(context.Background())

	// Assert
	for _, m := range []string{m1.MessageID, m2.MessageID} {
		got, err := e.p.Manifests.Get(m)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != domain.StatusDelivered {
			t.Fatalf("message %s status = %s, want delivered", m, got.Status)
		}
	}
}

func TestFanout_leaseを保持している場合_全永続化点を通過して配信が完了すること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	checker := &fakeLeaseChecker{held: true}
	e.p.Lease = checker

	// Act
	e.p.Fanout(context.Background())

	// Assert
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", got.Status)
	}
	if checker.calls == 0 {
		t.Fatal("lease 確認が一度も呼ばれていない")
	}
}

func TestFanout_永続化点の前でleaseを失っている場合_処理を停止し以降のメッセージを配信しないこと(t *testing.T) {
	// Arrange (lease を保持していない: 1 件目の永続化点に入る前に停止する)
	e := newEnv(t, config.HandlingDelete)
	m1 := e.seedArchived("orders_1.csv", "a")
	m2 := e.seedArchived("orders_2.csv", "b")
	e.p.Lease = &fakeLeaseChecker{held: false}

	// Act
	e.p.Fanout(context.Background())

	// Assert
	for _, m := range []string{m1.MessageID, m2.MessageID} {
		got, err := e.p.Manifests.Get(m)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == domain.StatusDelivered {
			t.Fatalf("message %s は lease 喪失中に配信されてはならない (status=%s)", m, got.Status)
		}
	}
	if fileExists(t, e.subFile("current", "orders_1.csv")) {
		t.Fatal("lease 喪失中はファイルを配置してはならない")
	}
}

func TestFanout_lease確認のIOが失敗した場合_failclosedで配信を進めないこと(t *testing.T) {
	// Arrange (lease 確認 I/O 失敗: 保持を楽観視せず安全側に倒す)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.p.Lease = &fakeLeaseChecker{err: errors.New("read lock failed")}

	// Act
	e.p.Fanout(context.Background())

	// Assert
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == domain.StatusDelivered {
		t.Fatalf("lease 確認失敗時は fail-closed で配信しない (status=%s)", got.Status)
	}
	if fileExists(t, e.subFile("current", "orders_1.csv")) {
		t.Fatal("lease 確認失敗時はファイルを配置してはならない")
	}
}

func TestCollect_永続化点の前でleaseを失っている場合_収集を停止すること(t *testing.T) {
	// Arrange (lease を保持していない: collectFile の入口で停止する)
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "payload")
	e.p.Lease = &fakeLeaseChecker{held: false}

	// Act (安定判定を満たすよう 2 サイクル実行する)
	e.collectStable()

	// Assert
	if len(e.manifests()) != 0 {
		t.Fatal("lease 喪失中は収集してはならない (manifest が作られない)")
	}
}

func TestCollect_leaseを保持している場合_収集が完了すること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	e.writeSource("orders_1.csv", "payload")
	e.p.Lease = &fakeLeaseChecker{held: true}

	// Act
	e.collectStable()

	// Assert
	if len(e.manifests()) != 1 {
		t.Fatalf("lease 保持中は収集されるべき: manifests=%d", len(e.manifests()))
	}
}

func TestFanout_PutMerged経由の更新で既存の決着状態を取りこぼさないこと(t *testing.T) {
	// Arrange: current=delivered が記録済みの delivering メッセージを再開配信する
	// (merge precedence: 既存 delivered が後退せず next が追記される)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	m.Status = domain.StatusDelivering
	now := e.clock.Now()
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &now, "")
	if err := e.p.Manifests.Put(m); err != nil {
		t.Fatal(err)
	}
	e.p.Lease = &fakeLeaseChecker{held: true}

	// Act
	e.p.Fanout(context.Background())

	// Assert
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	states := got.SubscriptionStates()
	if states["current"] != domain.SubscriptionDelivered {
		t.Fatalf("既存 current=delivered が保持されていない: %v", states)
	}
	if states["next"] != domain.SubscriptionDelivered {
		t.Fatalf("next=delivered が追記されていない: %v", states)
	}
	if got.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", got.Status)
	}
	if got.Revision == 0 {
		t.Fatal("PutMerged 経由なら revision が増えているはず")
	}
}

func TestRecordDelivery_複数activeが同一メッセージの別subscriptionを同時記録した場合_決着状態を取りこぼさないこと(t *testing.T) {
	// Arrange: archived メッセージ 1 件に対し、N 個の「別 active」が各自の subscription を
	// delivered として同時に記録する状況を模擬する。旧実装は recordDelivery が PutMerged
	// (ロック保持下) の後にロック外の素の Put(merged) を行っており、この最後の Put が
	// last-writer-wins で競合して他 active の delivered を取りこぼし得た (§7 指摘 M-1 /
	// codex blocker)。統一 Update API への一本化でこの窓が塞がれていることを検証する。
	const n = 8
	e := newEnv(t, config.HandlingDelete)
	seed := e.seedArchived("orders_1.csv", "payload")
	now := e.clock.Now()
	subs := make([]config.Subscription, n)
	for i := 0; i < n; i++ {
		subs[i] = config.Subscription{Name: fmt.Sprintf("sub_%d", i), Directory: e.dataDir}
	}
	topic := &config.Topic{Name: "orders", Subscriptions: subs}

	// Act: 各 active は自分の subscription のみを持つ stale なスナップショットで記録する。
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			upd := store.NewManifest(domain.NewMessage(now, "orders", "orders_1.csv"))
			d := now.Add(time.Duration(i) * time.Millisecond) // active ごとに一意な At にする
			name := fmt.Sprintf("sub_%d", i)
			upd.SetSubscriptionState(name, domain.SubscriptionDelivered, &d, "")
			// 各 active が自分の監査イベントを追記する (競合時のイベント取りこぼし検証用)。
			upd.AppendEvent(store.DeliveryEvent{At: d, Subscription: name, EventType: "delivered"})
			_, errs[i] = e.p.recordDelivery(upd, topic)
		}(i)
	}
	wg.Wait()

	// Assert: 全 active の記録が成功し、N 個すべての subscription が delivered で残ること。
	for i, err := range errs {
		if err != nil {
			t.Fatalf("active %d の recordDelivery が失敗: %v", i, err)
		}
	}
	got, err := e.p.Manifests.Get(seed.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	states := got.SubscriptionStates()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("sub_%d", i)
		if states[name] != domain.SubscriptionDelivered {
			t.Fatalf("subscription %s の決着状態が取りこぼされた: states=%v", name, states)
		}
	}
	if got.Status != domain.StatusDelivered {
		t.Fatalf("全 subscription delivered なら status=delivered のはず: got=%s", got.Status)
	}
	// 監査イベントも取りこぼさないこと (identity merge。len 差分追記だと競合時に drop し得た)。
	deliveredEvents := 0
	for _, ev := range got.DeliveryEvents {
		if ev.EventType == "delivered" {
			deliveredEvents++
		}
	}
	if deliveredEvents != n {
		t.Fatalf("全 active の監査イベントが残るべき: delivered events=%d, want %d", deliveredEvents, n)
	}
}

func TestResumeArchiving_leaseを失っている場合_中断アーカイブ昇格を再開せず停止すること(t *testing.T) {
	// Arrange: collected のまま中断したメッセージ (archive 実体はあるが manifest 更新が
	// 失われた状態) を植える。lease を失っていれば resumeArchiving は finalize せず停止する
	// (fix B: 中断アーカイブ昇格も永続化点)。
	e := newEnv(t, config.HandlingDelete)
	msg := domain.NewMessage(e.clock.Now(), "orders", "orders_1.csv")
	if err := store.WriteFileAtomic(e.p.Archive.ArchivePath("orders", msg.MessageID), strings.NewReader("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := store.NewManifest(msg) // status=collected
	if err := e.p.Manifests.Put(m); err != nil {
		t.Fatal(err)
	}
	e.p.Lease = &fakeLeaseChecker{held: false}

	// Act
	e.p.Collect(context.Background()) // 先頭で resumeArchiving が走る

	// Assert: lease 喪失中は archived へ昇格させない (collected のまま)。
	got, err := e.p.Manifests.Get(msg.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusCollected {
		t.Fatalf("lease 喪失中は昇格させてはならない: status=%s", got.Status)
	}
}

func TestFanout_leaseを途中で失った場合_2件目のメッセージを配信しないこと(t *testing.T) {
	// Arrange: 1 件目は lease 保持で配信、2 件目に入る前に lease を失う。
	// loseAfter は HoldsLease の呼び出し回数で制御する (1 件目=1 回目で held)。
	e := newEnv(t, config.HandlingDelete)
	m1 := e.seedArchived("orders_1.csv", "a")
	m2 := e.seedArchived("orders_2.csv", "b")
	e.p.Lease = &fakeLeaseChecker{held: true, loseAfter: 2}

	// Act
	e.p.Fanout(context.Background())

	// Assert (1 件目は配信、2 件目は lease 喪失で未配信)
	got1, err := e.p.Manifests.Get(m1.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got1.Status != domain.StatusDelivered {
		t.Fatalf("1 件目は lease 保持中なので配信されるべき: status=%s", got1.Status)
	}
	got2, err := e.p.Manifests.Get(m2.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Status == domain.StatusDelivered {
		t.Fatalf("2 件目は lease 喪失で配信されてはならない: status=%s", got2.Status)
	}
}
