package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Fanout は archived / delivering の全メッセージを (ファイル名昇順、SR-005)
// 未配信のサブスクリプションへ AtomicWrite で配信する。配信済みの
// サブスクリプションはマニフェストに基づきスキップされ (冪等な再入、SR-003)、
// 失敗は failed として記録して retry ステージに委ねる。
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
