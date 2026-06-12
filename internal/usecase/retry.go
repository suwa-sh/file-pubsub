package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Retry は failed の全メッセージを処理する: retry_max_count 以内なら失敗した
// サブスクリプションへアーカイブから再配信し、超過したらメッセージを
// dlq/{topic}/{message_id} (+ .meta.json) へ隔離して自動再配信から除外する
// (SR-004。復帰手段は replay のみ)。
//
// retrying 状態のメッセージも拾う: failed → retrying のマニフェスト書き込み
// 直後にクラッシュすると、放置すればメッセージが永遠に固まるため。再入が
// 安全なのは、再配信がマニフェストに基づき配信済みサブスクリプションを
// スキップし (SR-003)、DLQStore.Isolate が同じ dlq パスを冪等に上書きするから。
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

// isolateToDLQ はアーカイブファイルを隔離メタとともに DLQ へコピーし、失敗した
// 全サブスクリプションを dlq にマークしてメッセージを dlq として決着させる。
// DLQStore.Isolate は同じ dlq パスを上書きするため、retrying で固まった
// メッセージへの再実行 (クラッシュ復旧) で二重隔離は起きない。
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
		// マニフェストを failed のまま保ち、次サイクルで隔離を再試行する。
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
