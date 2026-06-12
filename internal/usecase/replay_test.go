package usecase

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func deliverAll(t *testing.T, e *testEnv) {
	t.Helper()
	e.p.Fanout(context.Background())
	for _, sub := range []string{"current", "next"} {
		entries, err := os.ReadDir(e.subDirs[sub])
		if err != nil {
			t.Fatal(err)
		}
		for _, ent := range entries {
			if err := os.Remove(e.subFile(sub, ent.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestReplayMessageIDPlacesOnlyDestination(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	deliverAll(t, e)

	count, err := e.p.Replay(context.Background(), ReplayParams{
		Topic: "orders", MessageID: m.MessageID, Subscription: "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("next must receive the replayed file")
	}
	if fileExists(t, e.subFile("next", "orders_1.csv.tmp")) {
		t.Fatal("no temp file must remain")
	}
	if fileExists(t, e.subFile("current", "orders_1.csv")) {
		t.Fatal("current must not be touched by the replay")
	}

	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ReplayRecords) != 1 {
		t.Fatalf("replay_records = %d, want 1", len(got.ReplayRecords))
	}
	r := got.ReplayRecords[0]
	if r.Result != "success" || len(r.TargetSubscriptions) != 1 || r.TargetSubscriptions[0] != "next" {
		t.Fatalf("unexpected replay record: %+v", r)
	}
}

func TestReplayPeriodSelectsByCollectedAt(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	e.clock.Set(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	e.seedArchived("a.csv", "a")
	e.clock.Set(time.Date(2026, 5, 31, 23, 0, 0, 0, time.UTC))
	e.seedArchived("b.csv", "b")
	e.clock.Set(time.Date(2026, 6, 1, 0, 30, 0, 0, time.UTC))
	e.seedArchived("c.csv", "c")
	deliverAll(t, e)

	count, err := e.p.Replay(context.Background(), ReplayParams{
		Topic:        "orders",
		From:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:           time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Subscription: "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (May messages only)", count)
	}
	if !fileExists(t, e.subFile("next", "a.csv")) || !fileExists(t, e.subFile("next", "b.csv")) {
		t.Fatal("May messages must be replayed")
	}
	if fileExists(t, e.subFile("next", "c.csv")) {
		t.Fatal("June message must not be replayed")
	}
}

func TestReplayRecoversDLQSubscription(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())
	for i := 0; i < 3; i++ {
		e.p.Retry(context.Background())
	}
	got, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if s := subState(t, got, "next"); s.Status != domain.SubscriptionDLQ {
		t.Fatalf("precondition: next = %s, want dlq", s.Status)
	}

	e.setSubscriptionDir("next", t.TempDir())
	count, err := e.p.Replay(context.Background(), ReplayParams{
		Topic: "orders", MessageID: m.MessageID, Subscription: "next",
	})
	if err != nil || count != 1 {
		t.Fatalf("replay: count=%d err=%v", count, err)
	}
	got, err = e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if s := subState(t, got, "next"); s.Status != domain.SubscriptionDelivered {
		t.Fatalf("next = %s, want delivered after replay", s.Status)
	}
}

func TestReplayValidation(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		params ReplayParams
	}{
		{"missing topic", ReplayParams{Subscription: "next", MessageID: "x"}},
		{"unknown topic", ReplayParams{Topic: "nope", Subscription: "next", MessageID: "x"}},
		{"missing subscription", ReplayParams{Topic: "orders", MessageID: "x"}},
		{"unknown subscription", ReplayParams{Topic: "orders", Subscription: "nope", MessageID: "x"}},
		{"both message and period", ReplayParams{Topic: "orders", Subscription: "next", MessageID: "x", From: may1, To: may1}},
		{"neither message nor period", ReplayParams{Topic: "orders", Subscription: "next"}},
		{"from only", ReplayParams{Topic: "orders", Subscription: "next", From: may1}},
		{"to before from", ReplayParams{Topic: "orders", Subscription: "next", From: may1, To: may1.AddDate(0, 0, -1)}},
	}
	for _, c := range cases {
		_, err := e.p.Replay(context.Background(), c.params)
		var usage UsageError
		if !errors.As(err, &usage) {
			t.Fatalf("%s: err = %v, want UsageError", c.name, err)
		}
	}
}

func TestReplayMissingArchiveFails(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	deliverAll(t, e)
	if err := e.p.Archive.Delete("orders", m.MessageID); err != nil {
		t.Fatal(err)
	}
	_, err := e.p.Replay(context.Background(), ReplayParams{
		Topic: "orders", MessageID: m.MessageID, Subscription: "next",
	})
	if err == nil {
		t.Fatal("replay of a retention-deleted archive must fail")
	}
	var usage UsageError
	if errors.As(err, &usage) {
		t.Fatal("a missing archive is a runtime error, not a usage error")
	}
}
