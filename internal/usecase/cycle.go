package usecase

import (
	"context"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// RunCycle はポーリングサイクルを 1 回、collect → fanout → retry → retention の
// 固定順 (LR-001) で実行し、最後に dlq / backlog ゲージを更新する。
// 各ステージのエラーはステージ内部でログに記録され、サイクルを中断しない。
func (p *Pipeline) RunCycle(ctx context.Context) {
	p.Collect(ctx)
	p.Fanout(ctx)
	p.Retry(ctx)
	p.Retention(ctx)
	p.RefreshGauges()
}

// RefreshGauges はトピック別の dlq_count / backlog_count ゲージを
// マニフェストと DLQ ディレクトリから再計算する。
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
