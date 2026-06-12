package usecase

import (
	"context"
	"os"
	"testing"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestFanoutDeliversAllSubscriptions(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")

	e.p.Fanout(context.Background())

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

func TestFanoutPartialFailureContinues(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")

	e.p.Fanout(context.Background())

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

func TestFanoutIdempotentNoDoubleDelivery(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.seedArchived("orders_1.csv", "payload")
	e.p.Fanout(context.Background())

	// The consumer takes the files away; a re-run must not re-place them.
	for _, sub := range []string{"current", "next"} {
		if err := os.Remove(e.subFile(sub, "orders_1.csv")); err != nil {
			t.Fatal(err)
		}
	}
	e.p.Fanout(context.Background())
	for _, sub := range []string{"current", "next"} {
		if fileExists(t, e.subFile(sub, "orders_1.csv")) {
			t.Fatalf("%s must not be redelivered (already delivered)", sub)
		}
	}
}

func TestFanoutResumeDeliversOnlyPending(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")

	// Interrupted fan-out: delivering with current already delivered.
	m.Status = domain.StatusDelivering
	now := e.clock.Now()
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &now, "")
	if err := e.p.Manifests.Put(m); err != nil {
		t.Fatal(err)
	}

	e.p.Fanout(context.Background())

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
