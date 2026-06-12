package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// seedStatusFixture は次の状態を作る: メッセージ A は両サブスクリプションに
// 配信済み、B は next が DLQ 隔離 (current は配信済み)、C は next へリプレイ済み。
func seedStatusFixture(t *testing.T, e *testEnv) (a, b, c string) {
	t.Helper()
	ma := e.seedArchived("a.csv", "a")
	e.p.Fanout(context.Background())

	e.clock.Advance(time.Minute)
	mb := e.seedArchived("b.csv", "b")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())
	for i := 0; i < 3; i++ {
		e.p.Retry(context.Background())
	}
	e.setSubscriptionDir("next", t.TempDir())

	e.clock.Advance(time.Minute)
	mc := e.seedArchived("c.csv", "c")
	e.p.Fanout(context.Background())
	if _, err := e.p.Replay(context.Background(), ReplayParams{Topic: "orders", MessageID: mc.MessageID, Subscription: "next"}); err != nil {
		t.Fatal(err)
	}
	return ma.MessageID, mb.MessageID, mc.MessageID
}

func TestStatusRows_各フィルタを指定した場合_該当行のみ返ること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	a, b, c := seedStatusFixture(t, e)

	// Act: フィルタなし
	rows, err := e.p.StatusRows(StatusFilter{})

	// Assert: メッセージ × サブスクリプションの全行が返る
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6 (3 messages x 2 subscriptions)", len(rows))
	}

	// Act: status フィルタ
	failedDLQ, err := e.p.StatusRows(StatusFilter{Status: "dlq"})

	// Assert: dlq の行のみ返る
	if err != nil {
		t.Fatal(err)
	}
	if len(failedDLQ) != 1 || failedDLQ[0].MessageID != b || failedDLQ[0].Subscription != "next" {
		t.Fatalf("dlq filter = %+v", failedDLQ)
	}
	if failedDLQ[0].DeliveredAt != nil {
		t.Fatal("a dlq row must have no delivered_at")
	}

	// Act: subscription フィルタ
	bySub, err := e.p.StatusRows(StatusFilter{Subscription: "current"})

	// Assert: current の行のみ返る
	if err != nil {
		t.Fatal(err)
	}
	if len(bySub) != 3 {
		t.Fatalf("subscription filter = %d rows, want 3", len(bySub))
	}
	for _, r := range bySub {
		if r.Subscription != "current" || r.Status != domain.SubscriptionDelivered {
			t.Fatalf("unexpected row: %+v", r)
		}
	}

	// Act: 未知の topic フィルタ
	byTopic, err := e.p.StatusRows(StatusFilter{Topic: "unknown"})

	// Assert: 何も一致しない
	if err != nil {
		t.Fatal(err)
	}
	if len(byTopic) != 0 {
		t.Fatalf("unknown topic filter must match nothing, got %d", len(byTopic))
	}

	// Assert: replay フラグはメッセージ C のリプレイ先サブスクリプションのみに立つ
	for _, r := range rows {
		wantReplay := r.MessageID == c && r.Subscription == "next"
		if r.Replay != wantReplay {
			t.Fatalf("replay flag of %s/%s = %v, want %v", r.MessageID, r.Subscription, r.Replay, wantReplay)
		}
	}
	_ = a
}

func TestSummarizeStatus_行を集計した場合_topicとsubscriptionごとの件数になること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	seedStatusFixture(t, e)
	rows, err := e.p.StatusRows(StatusFilter{})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	sums := SummarizeStatus(rows)

	// Assert
	if len(sums) != 2 {
		t.Fatalf("summaries = %d, want 2", len(sums))
	}
	cur, next := sums[0], sums[1]
	if cur.Subscription != "current" || cur.Delivered != 3 || cur.Failed != 0 || cur.DLQ != 0 {
		t.Fatalf("current summary = %+v", cur)
	}
	if next.Subscription != "next" || next.Delivered != 2 || next.DLQ != 1 {
		t.Fatalf("next summary = %+v", next)
	}
}

func TestDLQList_DLQメッセージがある場合_隔離メタが返りtopicフィルタが効くこと(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	_, b, _ := seedStatusFixture(t, e)

	// Act: フィルタなし
	metas, err := e.p.DLQList("")

	// Assert: 隔離メタが返る
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].MessageID != b || metas[0].Topic != "orders" {
		t.Fatalf("dlq list = %+v", metas)
	}
	if metas[0].IsolationReason == "" || metas[0].FailureCount != 2 {
		t.Fatalf("dlq meta = %+v", metas[0])
	}

	// Act: 別 topic でフィルタ
	none, err := e.p.DLQList("other")

	// Assert: 他 topic は除外される
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("topic filter must exclude other topics, got %d", len(none))
	}
}
