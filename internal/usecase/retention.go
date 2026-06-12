package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Retention は保持期限を過ぎたアーカイブファイルを削除する (SP-006)。
// 対象は配信が決着した (delivered / dlq) メッセージのみ: failed / delivering /
// retrying のアーカイブを削除すると、後続の retry や DLQ 隔離が必要とする
// 唯一のペイロードが失われる。dlq が安全なのは、メッセージが dlq として
// 決着する前に DLQStore.Isolate がペイロードを dlq/{topic}/{message_id} へ
// コピー (移動ではない) しているため。削除するのはアーカイブファイル本体のみで、
// マニフェストの配信履歴は監査と status コマンドのために保持する (CTR-003)。
func (p *Pipeline) Retention(ctx context.Context) {
	manifests, err := p.Manifests.List()
	if err != nil {
		p.emit(logging.Event{EventType: "retention_failed", ErrorDetail: fmt.Sprintf("list manifests failed: %v. check the manifest directory permissions; retried on the next polling cycle", err)})
		return
	}
	now := p.now()
	for _, m := range manifests {
		if ctx.Err() != nil {
			return
		}
		if m.RetentionDeadline == nil || !domain.IsExpired(*m.RetentionDeadline, now) {
			continue
		}
		if m.Status != domain.StatusDelivered && m.Status != domain.StatusDLQ {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_skipped", ErrorDetail: fmt.Sprintf("retention skipped (unresolved): status %q is not terminal (delivered / dlq). the archive payload is kept until the delivery settles; resolve the failure or replay the message", m.Status)})
			continue
		}
		exists, err := p.Archive.Exists(m.Topic, m.MessageID)
		if err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_delete_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions; retried on the next polling cycle", err)})
			continue
		}
		if !exists {
			continue // 削除済み (冪等な再実行)
		}
		if err := p.Archive.Delete(m.Topic, m.MessageID); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_delete_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions; retried on the next polling cycle", err)})
			continue
		}
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_deleted"})
	}
}
