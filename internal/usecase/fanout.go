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

		// メッセージ境界 lease 確認: Fan-out 配置・Manifest 記録に入る前に lease 保持を
		// 再確認する。失っていればこのメッセージで停止し以降を処理しない (spec-decision-011)。
		if err := p.ensureLease(); err != nil {
			p.emitLeaseStop(m.MessageID, m.Topic, "fanout")
			return
		}

		if m.Status == domain.StatusArchived {
			// 中間状態 archived → delivering をロック保持下の Update で記録する (lock 外 Put 排除)。
			if err := p.transitionStatus(m.MessageID, domain.StatusArchived, domain.StatusDelivering); err != nil {
				p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "fanout_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. retried on the next polling cycle", err)})
				continue
			}
			m.Status = domain.StatusDelivering // in-memory を同期 (後続の deliverPending / recordDelivery 用)
		}

		p.deliverPending(m, t)
		if _, err := p.recordDelivery(m, t); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "fanout_failed", ErrorDetail: fmt.Sprintf("manifest update failed: %v. undelivered subscriptions are re-resolved from the manifest on the next polling cycle", err)})
		}
	}
}
