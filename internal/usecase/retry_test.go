package usecase

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
)

func TestRetryRecoversFailedDelivery(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	// The destination recovers before the retry pass.
	fixed := filepath.Join(t.TempDir(), "next")
	e.setSubscriptionDir("next", fixed)

	e.p.Retry(context.Background())

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

func TestRetryCountsFailuresAndIsolatesToDLQ(t *testing.T) {
	e := newEnv(t, config.HandlingDelete) // RetryMaxCount = 2
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	// Two failing retries reach the limit.
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

	// The next pass exceeds the limit: isolate to DLQ.
	e.p.Retry(context.Background())

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

	// Isolated messages are excluded from further automatic processing.
	fixed := filepath.Join(t.TempDir(), "next")
	e.setSubscriptionDir("next", fixed)
	e.p.Retry(context.Background())
	e.p.Fanout(context.Background())
	if fileExists(t, e.subFile("next", "orders_1.csv")) {
		t.Fatal("a dlq-isolated subscription must not be redelivered automatically")
	}
}

// TestRetryResumesRetryingToDelivered guards crash recovery: a manifest left
// in retrying (crash right after the failed → retrying write) must be picked
// up by the next Retry pass and driven to delivered once the destination
// works again, instead of being stuck forever.
func TestRetryResumesRetryingToDelivered(t *testing.T) {
	e := newEnv(t, config.HandlingDelete)
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	// Simulate the crash window: status written as retrying, nothing after.
	stuck, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	stuck.Status = domain.StatusRetrying
	if err := e.p.Manifests.Put(stuck); err != nil {
		t.Fatal(err)
	}

	e.setSubscriptionDir("next", filepath.Join(t.TempDir(), "next"))
	e.p.Retry(context.Background())

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

// TestRetryResumesRetryingToDLQ: a manifest left in retrying with the retry
// limit already exhausted must proceed to DLQ isolation on the next pass.
func TestRetryResumesRetryingToDLQ(t *testing.T) {
	e := newEnv(t, config.HandlingDelete) // RetryMaxCount = 2
	m := e.seedArchived("orders_1.csv", "payload")
	e.breakSubscription("next")
	e.p.Fanout(context.Background())

	stuck, err := e.p.Manifests.Get(m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	stuck.Status = domain.StatusRetrying
	stuck.RetryCount = 2 // limit exhausted before the crash
	if err := e.p.Manifests.Put(stuck); err != nil {
		t.Fatal(err)
	}

	e.p.Retry(context.Background())

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
