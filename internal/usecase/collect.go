package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/source"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Collect runs one collection pass over every topic: resume interrupted
// archive promotions, then list → stability/exclusion checks → fetch →
// message_id assignment → work→archive promotion → manifest (collected →
// archived) → original file handling. A failing topic never stops the others.
func (p *Pipeline) Collect(ctx context.Context) {
	p.resumeArchiving()
	for i := range p.Cfg.Topics {
		t := &p.Cfg.Topics[i]
		if err := p.collectTopic(ctx, t); err != nil {
			p.emit(logging.Event{
				Topic:       t.Name,
				EventType:   "collect_failed",
				ErrorDetail: fmt.Sprintf("%v. check the source connection settings and credentials; the topic is retried on the next polling cycle", err),
			})
		}
	}
}

// resumeArchiving promotes messages left in the collected state by an
// interruption: work file present → promote again (idempotent overwrite);
// archive already present → only the manifest update was lost, redo it.
func (p *Pipeline) resumeArchiving() {
	manifests, err := p.Manifests.List()
	if err != nil {
		p.emit(logging.Event{EventType: "archive_failed", ErrorDetail: fmt.Sprintf("list manifests for archive resume failed: %v. check the manifest directory permissions; retried on the next polling cycle", err)})
		return
	}
	for _, m := range manifests {
		if m.Status != domain.StatusCollected {
			continue
		}
		if _, err := os.Stat(p.Archive.WorkPath(m.Topic, m.MessageID)); err == nil {
			if err := p.Archive.Promote(m.Topic, m.MessageID); err != nil {
				p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions and disk space; retried on the next polling cycle", err)})
				continue
			}
		} else if ok, _ := p.Archive.Exists(m.Topic, m.MessageID); !ok {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: "neither the work file nor the archive file exists for a collected message. the source file is re-collected as a new message on a later cycle"})
			continue
		}
		if err := p.finalizeArchive(m); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. retried on the next polling cycle", err)})
		}
	}
}

func (p *Pipeline) collectTopic(ctx context.Context, t *config.Topic) error {
	_ = p.Archive.CleanupWorkTempFiles(t.Name)
	fetchDir := filepath.Join(p.Cfg.DataDir, "work", "fetch", t.Name)
	_ = store.CleanupTempFiles(fetchDir)

	conn, err := p.newConnector(source.Options{
		Type:      t.Source.Type,
		Host:      t.Source.Host,
		Port:      t.Source.Port,
		Directory: t.Source.Directory,
		Username:  t.Source.Auth.Username,
		Password:  t.Source.Auth.Password,
		KeyFile:   t.Source.Auth.KeyFile,
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	files, err := conn.List(ctx)
	if err != nil {
		return err
	}

	obs := p.topicObservations(t.Name)
	interval := time.Duration(t.Source.StabilityCheck.Interval) * time.Second
	present := map[string]bool{}

	for _, f := range files {
		present[f.Name] = true
		if domain.IsExcluded(f.Name, t.Source.ExcludePatterns) {
			continue
		}
		if t.Source.OriginalFileHandling == config.HandlingCopy {
			done, err := p.Processed.IsProcessed(t.Name, f.Name, f.ModTime, f.Size)
			if err != nil {
				p.emit(logging.Event{Topic: t.Name, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("%v. the file %q stays a re-collection candidate", err, f.Name)})
				continue
			}
			if done {
				continue
			}
		}
		// Delete handling: every file present in the source is collected as a
		// new message, even when its name and mtime match an earlier message
		// (e.g. a leftover original whose delete failed, or a producer
		// re-output with cp -p). At-least-once: a duplicate collection is
		// bounded to one extra message because the original is deleted right
		// after the archive save succeeds.

		curr := domain.Observation{Name: f.Name, Size: f.Size, ModTime: f.ModTime, ObservedAt: p.now()}
		prev, seen := obs[f.Name]
		if !seen || prev.Size != curr.Size || !prev.ModTime.Equal(curr.ModTime) {
			obs[f.Name] = curr // first sighting or still being written: carry over
			continue
		}
		if !domain.IsStable(prev, curr, interval) {
			continue
		}
		if err := p.collectFile(ctx, t, conn, fetchDir, f); err != nil {
			p.emit(logging.Event{Topic: t.Name, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("collect %q: %v. retried on the next polling cycle", f.Name, err)})
			continue
		}
		delete(obs, f.Name)
	}
	for name := range obs {
		if !present[name] {
			delete(obs, name)
		}
	}
	if p.Metrics != nil {
		p.Metrics.SetLastCollected(t.Name, p.now())
	}
	return nil
}

func (p *Pipeline) collectFile(ctx context.Context, t *config.Topic, conn source.Connector, fetchDir string, f source.FileInfo) error {
	name := f.Name
	msg := domain.NewMessage(p.now(), t.Name, name)

	local, err := conn.Fetch(ctx, name, fetchDir)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	fetched, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("open fetched file: %w", err)
	}
	err = p.Archive.PutWork(t.Name, msg.MessageID, fetched)
	_ = fetched.Close()
	_ = os.Remove(local)
	if err != nil {
		return err
	}

	m := store.NewManifest(msg)
	if err := p.Manifests.Put(m); err != nil {
		return err
	}
	p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "collected"})

	if err := p.Archive.Promote(t.Name, msg.MessageID); err != nil {
		// The message stays collected; resumeArchiving retries next cycle.
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions and disk space; retried on the next polling cycle", err)})
		return nil
	}
	if err := p.finalizeArchive(m); err != nil {
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. retried on the next polling cycle", err)})
		return nil
	}

	// Original handling only after the archive save succeeded (LR-303).
	switch t.Source.OriginalFileHandling {
	case config.HandlingCopy:
		if err := p.Processed.MarkProcessed(t.Name, name, f.ModTime, f.Size, p.now()); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("%v. the file stays a re-collection candidate until the processed record is persisted", err)})
		}
	default:
		if err := conn.Remove(ctx, name); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "original_delete_failed", ErrorDetail: fmt.Sprintf("%v. check the source directory permissions; the delete is retried on the next polling cycle", err)})
		}
	}
	if p.Metrics != nil {
		p.Metrics.IncProcessed(t.Name)
	}
	return nil
}

// finalizeArchive moves the manifest to archived with the archive path, the
// save time and the retention deadline, then removes nothing: the archive is
// the source of truth from here on (SP-001).
func (p *Pipeline) finalizeArchive(m *store.Manifest) error {
	if err := domain.ValidateTransition(m.Status, domain.StatusArchived); err != nil {
		return err
	}
	saved := p.now()
	deadline := domain.RetentionDeadline(saved, p.Cfg.ArchiveRetention)
	m.Status = domain.StatusArchived
	m.ArchivePath = archiveRelPath(m.Topic, m.MessageID)
	m.SavedAt = &saved
	m.RetentionDeadline = &deadline
	m.AppendEvent(store.DeliveryEvent{At: saved, EventType: "archived"})
	if err := p.Manifests.Put(m); err != nil {
		return err
	}
	p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archived"})
	return nil
}
