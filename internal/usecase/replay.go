package usecase

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/logging"
)

// UsageError は引数・バリデーションの失敗を表す: CLI はこれを終了コード 2 に
// 対応づけ、ファイルは一切配置されない (LR-401 フィードフォワード)。
type UsageError struct{ msg string }

func (e UsageError) Error() string { return e.msg }

func usageErrorf(format string, args ...any) error {
	return UsageError{msg: fmt.Sprintf(format, args...)}
}

// ReplayParams はリプレイ対象を選択する: トピックに加えて、message_id 1 件
// または collected_at の日付範囲のいずれかと、配置先サブスクリプション 1 件。
type ReplayParams struct {
	Topic        string
	MessageID    string
	From, To     time.Time // 両端を含む日付。MessageID 指定時はゼロ値
	Subscription string
}

// ValidateReplay は不正なパラメータの組み合わせを、配置が始まる前に拒否する
// (LR-401)。
func (p *Pipeline) ValidateReplay(params ReplayParams) error {
	if params.Topic == "" {
		return usageErrorf("--topic is required. specify the topic of the messages to replay")
	}
	t := p.findTopic(params.Topic)
	if t == nil {
		return usageErrorf("topic %q is not defined in the config. check the topic name with config validate or status", params.Topic)
	}
	if params.Subscription == "" {
		return usageErrorf("--subscription is required. replay places files only into the specified destination subscription")
	}
	if findSubscription(t, params.Subscription) == nil {
		return usageErrorf("subscription %q is not defined for topic %q. check the subscription name in the config", params.Subscription, params.Topic)
	}
	hasPeriod := !params.From.IsZero() || !params.To.IsZero()
	if params.MessageID != "" && hasPeriod {
		return usageErrorf("--message-id and --from/--to are mutually exclusive. specify either one message or one period")
	}
	if params.MessageID == "" {
		if params.From.IsZero() || params.To.IsZero() {
			return usageErrorf("specify either --message-id or both --from and --to (YYYY-MM-DD)")
		}
		if params.To.Before(params.From) {
			return usageErrorf("--to %s is before --from %s. specify a valid period", params.To.Format("2006-01-02"), params.From.Format("2006-01-02"))
		}
	}
	return nil
}

// Replay はアーカイブ済みメッセージを配置先サブスクリプションへ AtomicWrite で
// 再配置し、各マニフェストにリプレイ記録を追記する (SP-102)。他の
// サブスクリプションには一切触れない。戻り値は配置したメッセージ数。
//
// Replay はマニフェストを書き換えるため、呼び出し側は事前に data-dir ロック
// (store.LockManager) を保持しなければならない: serve と並行実行すると
// last-writer-wins の競合でマニフェスト更新が失われる。
func (p *Pipeline) Replay(ctx context.Context, params ReplayParams) (int, error) {
	if err := p.ValidateReplay(params); err != nil {
		return 0, err
	}
	t := p.findTopic(params.Topic)
	sub := findSubscription(t, params.Subscription)

	var targets []*store.Manifest
	if params.MessageID != "" {
		m, err := p.Manifests.Get(params.MessageID)
		if err != nil {
			return 0, fmt.Errorf("manifest for message %q was not found: %w. check the message_id with the status command", params.MessageID, err)
		}
		if m.Topic != params.Topic {
			return 0, usageErrorf("message %q belongs to topic %q, not %q. specify the matching topic", params.MessageID, m.Topic, params.Topic)
		}
		targets = []*store.Manifest{m}
	} else {
		all, err := p.Manifests.List()
		if err != nil {
			return 0, fmt.Errorf("list manifests failed: %v. check the manifest directory permissions", err)
		}
		until := params.To.AddDate(0, 0, 1)
		for _, m := range all {
			if m.Topic != params.Topic {
				continue
			}
			if m.CollectedAt.Before(params.From) || !m.CollectedAt.Before(until) {
				continue
			}
			targets = append(targets, m)
		}
	}

	count := 0
	for _, m := range targets {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		exists, err := p.Archive.Exists(m.Topic, m.MessageID)
		if err != nil {
			return count, fmt.Errorf("%v. check the archive directory permissions", err)
		}
		if !exists {
			return count, fmt.Errorf("archive file for message %q is missing (it may have been deleted by retention). the message cannot be replayed", m.MessageID)
		}
		dst := filepath.Join(sub.Directory, m.OriginalFileName)
		if err := store.CopyFileAtomic(p.Archive.ArchivePath(m.Topic, m.MessageID), dst); err != nil {
			return count, fmt.Errorf("replay placement for message %q failed: %v. check the subscription directory path and permissions", m.MessageID, err)
		}
		now := p.now()
		m.SetSubscriptionState(params.Subscription, domain.SubscriptionDelivered, &now, "")
		m.AppendReplay(store.ReplayRecord{ReplayedAt: now, TargetSubscriptions: []string{params.Subscription}, Result: "success"})
		m.AppendEvent(store.DeliveryEvent{At: now, Subscription: params.Subscription, EventType: "replayed"})
		if err := p.Manifests.Put(m); err != nil {
			return count, fmt.Errorf("manifest update for message %q failed: %v. the placed file is valid; re-run replay to record it", m.MessageID, err)
		}
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, Subscription: params.Subscription, EventType: "replayed"})
		count++
	}
	return count, nil
}
