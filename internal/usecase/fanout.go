package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Fanout delivers every archived / delivering message (file name ascending,
// SR-005) to its pending subscriptions with AtomicWrite. Delivered
// subscriptions are skipped via the manifest (idempotent re-entry, SR-003);
// failures are recorded as failed and left to the retry stage.
func (p *Pipeline) Fanout(ctx context.Context) {
	manifests, err := p.Manifests.List()
	if err != nil {
		p.emit(logging.Event{EventType: "fanout_failed", ErrorDetail: fmt.Sprintf("list manifests failed: %v. check the manifest directory permissions; retried on the next polling cycle", err)})
		return
	}
	for _, m := range manifests {
		if ctx.Err() != nil {
			return
		}
		if m.Status != domain.StatusArchived && m.Status != domain.StatusDelivering {
			continue
		}
		t := p.findTopic(m.Topic)
		if t == nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "fanout_skipped", ErrorDetail: "topic is not defined in the config. add the topic back or replay the message manually"})
			continue
		}

		if m.Status == domain.StatusArchived {
			m.Status = domain.StatusDelivering
			if err := p.Manifests.Put(m); err != nil {
				p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "fanout_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
				continue
			}
		}

		p.deliverPending(m, t)
		p.settle(m, t)
		if err := p.Manifests.Put(m); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "fanout_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. undelivered subscriptions are re-resolved from the manifest on the next polling cycle", err)})
		}
	}
}
