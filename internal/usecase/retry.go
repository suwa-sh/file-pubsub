package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Retry handles every failed message: within retry_max_count the failed
// subscriptions are redelivered from the archive; beyond it the message is
// isolated to dlq/{topic}/{message_id} (+ .meta.json) and excluded from
// automatic redelivery (SR-004; replay is the only way back).
//
// Messages already in the retrying state are picked up too: a crash right
// after the failed → retrying manifest write would otherwise leave the
// message stuck forever. Re-entry is safe because redelivery skips delivered
// subscriptions via the manifest (SR-003) and DLQStore.Isolate overwrites the
// same dlq paths idempotently.
func (p *Pipeline) Retry(ctx context.Context) {
	manifests, err := p.Manifests.List()
	if err != nil {
		p.emit(logging.Event{EventType: "retry_failed", ErrorDetail: fmt.Sprintf("list manifests failed: %v. check the manifest directory permissions; retried on the next polling cycle", err)})
		return
	}
	for _, m := range manifests {
		if ctx.Err() != nil {
			return
		}
		if m.Status != domain.StatusFailed && m.Status != domain.StatusRetrying {
			continue
		}
		t := p.findTopic(m.Topic)
		if t == nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry_skipped", ErrorDetail: "topic is not defined in the config. add the topic back or replay the message manually"})
			continue
		}

		if m.Status == domain.StatusFailed {
			m.Status = domain.StatusRetrying
			if err := p.Manifests.Put(m); err != nil {
				p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
				continue
			}
		}

		if domain.ShouldIsolate(m.RetryCount, p.Cfg.RetryMaxCount) {
			p.isolateToDLQ(m, t)
			continue
		}

		m.Status = domain.StatusDelivering
		if err := p.Manifests.Put(m); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
			continue
		}
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry"})
		failures := p.deliverPending(m, t)
		if failures > 0 {
			m.RetryCount++
		}
		p.settle(m, t)
		if err := p.Manifests.Put(m); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
		}
	}
}

// isolateToDLQ copies the archive file into the DLQ with its isolation meta,
// marks every failed subscription dlq and finishes the message as dlq.
// DLQStore.Isolate overwrites the same dlq paths, so re-running for a message
// stuck in retrying (crash recovery) never double-isolates.
func (p *Pipeline) isolateToDLQ(m *store.Manifest, t *config.Topic) {
	now := p.now()
	var failedSubs []string
	reason := ""
	for _, s := range m.Subscriptions {
		if s.Status == domain.SubscriptionFailed {
			failedSubs = append(failedSubs, s.Subscription)
			if reason == "" {
				reason = s.LastError
			}
		}
	}
	if reason == "" {
		reason = "delivery failed repeatedly"
	}

	meta := store.DLQMeta{
		MessageID:       m.MessageID,
		Topic:           m.Topic,
		IsolationReason: reason,
		FailureCount:    m.RetryCount,
		IsolatedAt:      now,
	}
	if err := p.DLQ.Isolate(p.Archive.ArchivePath(m.Topic, m.MessageID), meta); err != nil {
		// Manifest keeps failed so the isolation is retried next cycle.
		m.Status = domain.StatusFailed
		_ = p.Manifests.Put(m)
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "dlq_isolation_failed", ErrorDetail: fmt.Sprintf("%v. check the dlq directory permissions and disk space; the isolation is retried on the next polling cycle", err)})
		return
	}

	for _, name := range failedSubs {
		m.SetSubscriptionState(name, domain.SubscriptionDLQ, nil, reason)
		m.AppendEvent(store.DeliveryEvent{At: now, Subscription: name, EventType: "dlq_isolated", Detail: reason})
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, Subscription: name, EventType: "dlq_isolated", ErrorDetail: fmt.Sprintf("retry limit (%d) exceeded: %s. inspect the cause with status --status dlq and redeliver with the replay command after fixing it", p.Cfg.RetryMaxCount, reason)})
	}
	p.settle(m, t)
	if err := p.Manifests.Put(m); err != nil {
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retry_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
	}
}
