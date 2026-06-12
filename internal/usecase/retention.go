package usecase

import (
	"context"
	"fmt"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Retention deletes archive files past their retention deadline (SP-006).
// Only the archive file body is removed; the manifest delivery history is
// kept for audit and the status command (CTR-003).
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
		exists, err := p.Archive.Exists(m.Topic, m.MessageID)
		if err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_delete_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions; retried on the next polling cycle", err)})
			continue
		}
		if !exists {
			continue // already deleted (idempotent re-run)
		}
		if err := p.Archive.Delete(m.Topic, m.MessageID); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_delete_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions; retried on the next polling cycle", err)})
			continue
		}
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "retention_deleted"})
	}
}
