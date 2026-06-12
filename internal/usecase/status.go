package usecase

import (
	"fmt"
	"sort"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
)

// StatusFilter は配信状態の行を絞り込む (空フィールドは全件に一致)。
type StatusFilter struct {
	Topic        string
	Subscription string
	Status       string // delivered / failed / dlq
}

// StatusRow は status テーブルの 1 行 (ui-design.md のカラム契約)。
type StatusRow struct {
	MessageID    string
	Topic        string
	Subscription string
	Status       domain.SubscriptionStatus
	Retry        int
	DeliveredAt  *time.Time
	Replay       bool
}

// StatusRows はマニフェスト (唯一の正本、CTR-003) を走査し、
// メッセージ × サブスクリプションごとに 1 行を message_id 昇順で返す。
func (p *Pipeline) StatusRows(f StatusFilter) ([]StatusRow, error) {
	manifests, err := p.Manifests.List()
	if err != nil {
		return nil, fmt.Errorf("read manifests failed: %v. check the manifest directory under the data dir", err)
	}
	var rows []StatusRow
	for _, m := range manifests {
		if f.Topic != "" && m.Topic != f.Topic {
			continue
		}
		replayed := map[string]bool{}
		for _, r := range m.ReplayRecords {
			for _, s := range r.TargetSubscriptions {
				replayed[s] = true
			}
		}
		for _, s := range m.Subscriptions {
			if f.Subscription != "" && s.Subscription != f.Subscription {
				continue
			}
			if f.Status != "" && string(s.Status) != f.Status {
				continue
			}
			rows = append(rows, StatusRow{
				MessageID:    m.MessageID,
				Topic:        m.Topic,
				Subscription: s.Subscription,
				Status:       s.Status,
				Retry:        m.RetryCount,
				DeliveredAt:  s.DeliveredAt,
				Replay:       replayed[s.Subscription],
			})
		}
	}
	return rows, nil
}

// StatusSummary はトピック / サブスクリプション別の件数ビュー (LP-401)。
type StatusSummary struct {
	Topic        string
	Subscription string
	Delivered    int
	Failed       int
	DLQ          int
}

// SummarizeStatus は行をトピック / サブスクリプション別に集計し、
// トピック → サブスクリプションの順でソートして返す。
func SummarizeStatus(rows []StatusRow) []StatusSummary {
	type key struct{ topic, sub string }
	counts := map[key]*StatusSummary{}
	for _, r := range rows {
		k := key{r.Topic, r.Subscription}
		s := counts[k]
		if s == nil {
			s = &StatusSummary{Topic: r.Topic, Subscription: r.Subscription}
			counts[k] = s
		}
		switch r.Status {
		case domain.SubscriptionDelivered:
			s.Delivered++
		case domain.SubscriptionFailed:
			s.Failed++
		case domain.SubscriptionDLQ:
			s.DLQ++
		}
	}
	out := make([]StatusSummary, 0, len(counts))
	for _, s := range counts {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Topic != out[j].Topic {
			return out[i].Topic < out[j].Topic
		}
		return out[i].Subscription < out[j].Subscription
	})
	return out
}

// DLQList は全 DLQ メッセージ (トピック指定も可) の隔離メタデータを、
// トピック → message_id の順で返す。
func (p *Pipeline) DLQList(topicFilter string) ([]store.DLQMeta, error) {
	var metas []store.DLQMeta
	for i := range p.Cfg.Topics {
		t := &p.Cfg.Topics[i]
		if topicFilter != "" && t.Name != topicFilter {
			continue
		}
		ms, err := p.DLQ.List(t.Name)
		if err != nil {
			return nil, fmt.Errorf("read dlq failed: %v. check the dlq directory under the data dir", err)
		}
		metas = append(metas, ms...)
	}
	return metas, nil
}
