package usecase

import (
	"context"
	"os"
	"testing"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestFanout_archivedメッセージがある場合_全サブスクリプションに配信されること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")

	// Act
	e.p.Fanout(context.Background())

	// Assert
	for _, sub := range []string{"current", "next"} {
		path := e.subFile(sub, "orders_1.csv")
		if !fileExists(t, path) {
			t.Fatalf("%s must receive the file", sub)
		}
		if got := readFile(t, path); got != "payload" {
			t.Fatalf("%s content = %q", sub, got)
		}
		if fileExists(t, path+".tmp") {
			t.Fatalf("%s must not keep a temp file", sub)
		}
	}
	m, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", m.Status)
	}
	if m.DeliveredAt == nil {
		t.Fatal("delivered_at must be set")
	}
	for _, sub := range []string{"current", "next"} {
		s := subState(t, m, sub)
		if s.Status != domain.SubscriptionDelivered || s.DeliveredAt == nil {
			t.Fatalf("%s state = %+v, want delivered", sub, s)
		}
	}
}

func TestFanout_一部のサブスクリプションが失敗した場合_他への配信は継続しfailedが記録されること(t *testing.T) {
	// Arrange
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")

	// Act
	e.p.Fanout(context.Background())

	// Assert
	if !fileExists(t, e.subFile("current", "orders_1.csv")) {
		t.Fatal("current must be delivered despite the next failure")
	}
	m, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.StatusFailed {
		t.Fatalf("status = %s, want failed", m.Status)
	}
	if s := subState(t, m, "current"); s.Status != domain.SubscriptionDelivered {
		t.Fatalf("current = %s, want delivered", s.Status)
	}
	s := subState(t, m, "next")
	if s.Status != domain.SubscriptionFailed {
		t.Fatalf("next = %s, want failed", s.Status)
	}
	if s.LastError == "" {
		t.Fatal("failed subscription must record last_error (cause + remedy)")
	}
}

func TestFanout_配信済みメッセージを再実行した場合_再配置されないこと(t *testing.T) {
	// Arrange: 配信後、コンシューマーがファイルを引き取った状態にする
	e := newEnv(t, config.HandlingDelete)
	e.seedArchived("orders_1.csv", "payload")
	e.p.Fanout(context.Background())
	for _, sub := range []string{"current", "next"} {
		if err := os.Remove(e.subFile(sub, "orders_1.csv")); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	e.p.Fanout(context.Background())

	// Assert
	for _, sub := range []string{"current", "next"} {
		if fileExists(t, e.subFile(sub, "orders_1.csv")) {
			t.Fatalf("%s must not be redelivered (already delivered)", sub)
		}
	}
}

func TestFanout_delivering状態で中断していた場合_未配信のサブスクリプションのみ配信されること(t *testing.T) {
	// Arrange: 中断されたファンアウトを再現する (delivering で current は配信済み)
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	m.Status = domain.StatusDelivering
	now := e.clock.Now()
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &now, "")
	if err := e.p.Manifests.Put(m); err != nil {
		t.Fatal(err)
	}

	// Act
	e.p.Fanout(context.Background())

	// Assert
	if fileExists(t, e.subFile("current", "orders_1.csv")) {
		t.Fatal("current was already delivered and must not be re-placed")
	}
	if !fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("next must be delivered on resume")
	}
	m, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.StatusDelivered {
		t.Fatalf("status = %s, want delivered", m.Status)
	}
}
