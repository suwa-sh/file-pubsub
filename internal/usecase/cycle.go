package usecase

import (
	"context"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// RunCycle executes one polling cycle in the fixed order collect → fanout →
// retry → retention (LR-001), then refreshes the dlq / backlog gauges.
// Stage errors are logged inside each stage and never abort the cycle.
func (p *Pipeline) RunCycle(ctx context.Context) {
	p.Collect(ctx)
	p.Fanout(ctx)
	p.Retry(ctx)
	p.Retention(ctx)
	p.RefreshGauges()
}

// RefreshGauges recomputes the per-topic dlq_count and backlog_count gauges
// from the manifests and the DLQ directory.
func (p *Pipeline) RefreshGauges() {
	if p.Metrics == nil {
		return
	}
	manifests, err := p.Manifests.List()
	if err != nil {
		return
	}
	backlog := map[string]int{}
	for _, m := range manifests {
		switch m.Status {
		case domain.StatusDelivered, domain.StatusDLQ:
		default:
			backlog[m.Topic]++
		}
	}
	for i := range p.Cfg.Topics {
		name := p.Cfg.Topics[i].Name
		p.Metrics.SetBacklogCount(name, backlog[name])
		if metas, err := p.DLQ.List(name); err == nil {
			p.Metrics.SetDLQCount(name, len(metas))
		}
	}
}
