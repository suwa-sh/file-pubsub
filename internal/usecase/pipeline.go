// Package usecase implements the polling-cycle stages (collect → fanout →
// retry → retention) and the shared operations (replay / status query) on top
// of the domain rules and the file stores. The CLI and the daemon both go
// through this layer (CLP-101 / CLR-101).
package usecase

import (
	"fmt"
	"path"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/metricsreg"
	"github.com/suwa-sh/file-pubsub/internal/gateway/source"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// Pipeline wires the stores, the logger and the metrics registry together.
// Log and Metrics may be nil (e.g. read-only CLI usage); Now and NewConnector
// default to time.Now and source.New.
type Pipeline struct {
	Cfg          *config.Config
	Manifests    *store.ManifestStore
	Archive      *store.ArchiveStore
	DLQ          *store.DLQStore
	Processed    *store.ProcessedStore
	Log          *logging.Logger
	Metrics      *metricsreg.Registry
	Now          func() time.Time
	NewConnector func(source.Options) (source.Connector, error)

	// observations carries the per-topic stability observations between
	// polling cycles (in-memory: a restart just needs one extra cycle).
	observations map[string]map[string]domain.Observation
}

// NewPipeline builds a pipeline whose stores are rooted at cfg.DataDir.
func NewPipeline(cfg *config.Config, log *logging.Logger, metrics *metricsreg.Registry) *Pipeline {
	return &Pipeline{
		Cfg:       cfg,
		Manifests: store.NewManifestStore(cfg.DataDir),
		Archive:   store.NewArchiveStore(cfg.DataDir),
		DLQ:       store.NewDLQStore(cfg.DataDir),
		Processed: store.NewProcessedStore(cfg.DataDir),
		Log:       log,
		Metrics:   metrics,
	}
}

func (p *Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pipeline) newConnector(o source.Options) (source.Connector, error) {
	if p.NewConnector != nil {
		return p.NewConnector(o)
	}
	return source.New(o)
}

func (p *Pipeline) emit(e logging.Event) {
	if p.Log != nil {
		p.Log.Emit(e)
	}
}

func (p *Pipeline) findTopic(name string) *config.Topic {
	for i := range p.Cfg.Topics {
		if p.Cfg.Topics[i].Name == name {
			return &p.Cfg.Topics[i]
		}
	}
	return nil
}

func findSubscription(t *config.Topic, name string) *config.Subscription {
	for i := range t.Subscriptions {
		if t.Subscriptions[i].Name == name {
			return &t.Subscriptions[i]
		}
	}
	return nil
}

func subscriptionNames(t *config.Topic) []string {
	names := make([]string, len(t.Subscriptions))
	for i, s := range t.Subscriptions {
		names[i] = s.Name
	}
	return names
}

// archiveRelPath is the manifest archive_path value (object-storage-schema).
func archiveRelPath(topic, messageID string) string {
	return path.Join("archive", topic, messageID)
}

func (p *Pipeline) topicObservations(topic string) map[string]domain.Observation {
	if p.observations == nil {
		p.observations = map[string]map[string]domain.Observation{}
	}
	if p.observations[topic] == nil {
		p.observations[topic] = map[string]domain.Observation{}
	}
	return p.observations[topic]
}

// settle derives the message status from the per-subscription states after a
// delivery pass. All configured subscriptions delivered → delivered; any
// failed → failed; otherwise dlq when an isolated subscription remains.
func (p *Pipeline) settle(m *store.Manifest, t *config.Topic) {
	states := m.SubscriptionStates()
	allDelivered := len(t.Subscriptions) > 0
	anyFailed := false
	anyDLQ := false
	for _, name := range subscriptionNames(t) {
		switch states[name] {
		case domain.SubscriptionDelivered:
		case domain.SubscriptionFailed:
			anyFailed = true
			allDelivered = false
		case domain.SubscriptionDLQ:
			anyDLQ = true
			allDelivered = false
		default:
			allDelivered = false
		}
	}
	switch {
	case allDelivered:
		m.Status = domain.StatusDelivered
		if m.DeliveredAt == nil {
			now := p.now()
			m.DeliveredAt = &now
		}
	case anyFailed:
		m.Status = domain.StatusFailed
	case anyDLQ:
		m.Status = domain.StatusDLQ
	}
}

// deliverPending places the archive file into every pending subscription of m
// with AtomicWrite, records delivered / failed per subscription in the
// manifest (in memory; the caller persists) and returns the failure count.
func (p *Pipeline) deliverPending(m *store.Manifest, t *config.Topic) int {
	failures := 0
	for _, name := range domain.PendingSubscriptions(m.SubscriptionStates(), subscriptionNames(t)) {
		sub := findSubscription(t, name)
		if sub == nil {
			continue
		}
		dst := path.Join(sub.Directory, m.OriginalFileName)
		err := store.CopyFileAtomic(p.Archive.ArchivePath(m.Topic, m.MessageID), dst)
		now := p.now()
		if err == nil {
			m.SetSubscriptionState(name, domain.SubscriptionDelivered, &now, "")
			m.AppendEvent(store.DeliveryEvent{At: now, Subscription: name, EventType: "delivered"})
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, Subscription: name, EventType: "delivered"})
			continue
		}
		failures++
		detail := fmt.Sprintf("write to subscription directory failed: %v. check the directory path, permissions and disk space; the delivery is retried up to retry_max_count and then isolated to the DLQ", err)
		m.SetSubscriptionState(name, domain.SubscriptionFailed, nil, detail)
		m.AppendEvent(store.DeliveryEvent{At: now, Subscription: name, EventType: "delivery_failed", Detail: detail})
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, Subscription: name, EventType: "delivery_failed", ErrorDetail: detail})
		if p.Metrics != nil {
			p.Metrics.IncDeliveryFailure(m.Topic)
		}
	}
	return failures
}
