package usecase

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestRetry_配信先が回復した場合_アーカイブから再配信されdeliveredになること(t *testing.T) {
	// Arrange: next の失敗後、retry パスの前に配信先を回復させる
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())
	fixed := filepath.Join(t.TempDir(), "next")
	e.setSubscriptionDir("next", fixed)

	// Act
	e.p.Retry(context.Background())

	// Assert
	if !fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("next must be redelivered from the archive")
	}
	m, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", m.Status)
	}
	if m.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0 (successful retry does not count)", m.RetryCount)
	}
}

func TestRetry_失敗が上限を超えた場合_DLQに隔離され自動再配信から除外されること(t *testing.T) {
	// Arrange (RetryMaxCount = 2)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	// Act & Assert: 失敗するリトライ 2 回で上限に達する
	for want := 1; want <= 2; want++ {
		e.p.Retry(context.Background())
		got, err := e.p.Manifests.Get(m.MessageID)
		if err != nil {
			t.Fatal(err)
		}
		if got.RetryCount != want {
			t.Fatalf("retry_count = %d, want %d", got.RetryCount, want)
		}
		if got.Status != domain.StatusFailed {
			t.Fatalf("status = %s, want failed", got.Status)
		}
	}

	// Act: 次のパスで上限を超える
	e.p.Retry(context.Background())

	// Assert: DLQ に隔離される
	if !fileExists(t, e.p.DLQ.FilePath("orders", m.MessageID)) {
		t.Fatal("dlq file must exist")
	}
	meta, err := e.p.DLQ.ReadMeta("orders", m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.FailureCount != 2 || meta.IsolationReason == "" || meta.IsolatedAt.IsZero() {
		t.Fatalf("unexpected dlq meta: %+v", meta)
	}
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDLQ {
		t.Fatalf("status = %s, want dlq", got.Status)
	}
	if s := subState(t, got, "next"); s.Status != domain.SubscriptionDLQ {
		t.Fatalf("next = %s, want dlq", s.Status)
	}
	if s := subState(t, got, "current"); s.Status != domain.SubscriptionDelivered {
		t.Fatalf("current = %s, want delivered (untouched)", s.Status)
	}

	// Act: 配信先が回復しても、隔離済みメッセージは自動処理の対象外
	fixed := filepath.Join(t.TempDir(), "next")
	e.setSubscriptionDir("next", fixed)
	e.p.Retry(context.Background())
	e.p.Fanout(context.Background())

	// Assert
	if fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("a dlq-isolated subscription must not be redelivered automatically")
	}
}

// TestRetry_retrying状態で中断していた場合_再開後にdeliveredまで到達すること は
// クラッシュ復旧を守るテスト: failed → retrying の書き込み直後にクラッシュして
// retrying のまま残ったマニフェストは、次の Retry パスで拾われ、配信先が回復
// していれば delivered まで進まなければならない (永遠に固まってはならない)。
func TestRetry_retrying状態で中断していた場合_再開後にdeliveredまで到達すること(t *testing.T) {
	// Arrange: クラッシュの窓を再現する (status を retrying にしただけで終わる)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	stuck, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	stuck.Status = domain.StatusRetrying
	if err := e.p.Manifests.Put(stuck); err != nil {
		t.Fatal(err)
	}
	e.setSubscriptionDir("next", filepath.Join(t.TempDir(), "next"))

	// Act
	e.p.Retry(context.Background())

	// Assert
	if !fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("the retrying message must be redelivered after restart")
	}
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", got.Status)
	}
}

// TestRetry_retrying状態で上限超過のまま中断していた場合_再開後にDLQへ隔離されること:
// retry 上限を使い切った状態で retrying のまま残ったマニフェストは、次のパスで
// DLQ 隔離まで進まなければならない。
func TestRetry_retrying状態で上限超過のまま中断していた場合_再開後にDLQへ隔離されること(t *testing.T) {
	// Arrange (RetryMaxCount = 2): クラッシュ前に上限を使い切った状態を再現する
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	stuck, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	stuck.Status = domain.StatusRetrying
	stuck.RetryCount = 2 // クラッシュ前に上限を使い切っている
	if err := e.p.Manifests.Put(stuck); err != nil {
		t.Fatal(err)
	}

	// Act
	e.p.Retry(context.Background())

	// Assert
	if !fileExists(t, e.p.DLQ.FilePath("orders", m.MessageID)) {
		t.Fatal("the retrying message must be isolated to the DLQ after restart")
	}
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDLQ {
		t.Fatalf("status = %s, want dlq", got.Status)
	}
}
