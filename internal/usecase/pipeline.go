// Package usecase は、domain のルールとファイルストアの上に、ポーリング
// サイクルの各ステージ (collect → fanout → retry → retention) と共通の運用
// 操作 (replay / status 照会) を実装する。CLI とデーモンはともにこの層を
// 経由する (CLP-101 / CLR-101)。
package usecase

import (
	"errors"
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

// LeaseChecker はメッセージ境界・永続化前の lease 保持確認を抽象化する
// (spec-decision-011)。active な serve は各永続化点の前にこれを呼び、lease を
// 失っていれば「処理中のその1メッセージ」で停止して降格する。返り値は
// (保持しているか, I/O エラー)。read I/O が失敗した場合は保持を楽観視せず
// (false, error) を返し、呼び出し側が安全側 (fail-closed) に倒せるようにする。
type LeaseChecker interface {
	HoldsLease() (bool, error)
}

// errLeaseLost は lease を失った (または保持を確認できなかった) ためメッセージ
// 処理を中断することを表す sentinel エラー。サイクルのステージはこれを検知して
// 以降のメッセージを処理せずに抜ける (高々 1 メッセージの被害限定)。
var errLeaseLost = errors.New("lease not held: stopping at message boundary")

// Pipeline は各ストア・ロガー・メトリクスレジストリを束ねる。
// Log と Metrics は nil でもよい (例: 読み取り専用の CLI 利用)。Now と
// NewConnector の既定値はそれぞれ time.Now と source.New。
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

	// Lease はメッセージ境界・永続化前の lease 保持確認 (active のみ)。nil の場合は
	// 常に保持しているとみなし lease 確認をスキップする (単一インスタンス運用の
	// 後方互換)。daemon が active 稼働時のみ設定し、降格・停止時に nil へ戻す。
	Lease LeaseChecker

	// observations はトピック別の安定判定観測値をポーリングサイクル間で
	// 持ち越す (メモリ上のみ: 再起動時はサイクルが 1 回余計に必要になるだけ)。
	observations map[string]map[string]domain.Observation
}

// NewPipeline は cfg.DataDir をルートとするストア群を持つパイプラインを生成する。
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

// ensureLease は各メッセージの永続化点に入る前の lease 保持確認を行う
// (メッセージ境界 lease 確認、spec-decision-011)。Lease が nil なら単一インスタンス
// 運用とみなし常に保持している扱いで nil を返す (後方互換、確認スキップ)。lease を
// 失っているか、確認 I/O が失敗した場合は errLeaseLost を返し (fail-closed)、呼び出し
// 側はそのメッセージで停止して以降のメッセージを処理しない。
func (p *Pipeline) ensureLease() error {
	if p.Lease == nil {
		return nil // 単一インスタンス: lease 確認をスキップ (後方互換)
	}
	held, err := p.Lease.HoldsLease()
	if err != nil {
		// 保持を確認できない: 楽観視せず安全側に倒す (fail-closed)。
		return fmt.Errorf("%w: lease check failed: %v", errLeaseLost, err)
	}
	if !held {
		return errLeaseLost
	}
	return nil
}

// emitLeaseStop は lease 喪失でメッセージ境界停止したことを構造化ログに出す。
// stage は停止した永続化点 (collect / fanout / retry) を示す。
func (p *Pipeline) emitLeaseStop(messageID, topic, stage string) {
	p.emit(logging.Event{
		MessageID:   messageID,
		Topic:       topic,
		EventType:   "lease_lost",
		ErrorDetail: fmt.Sprintf("lease not held before %s persistence point. stopping at this message to limit duplication to at most one message; demoting to standby", stage),
	})
}

// recordDelivery は配信結果 (delivered などのサブスクリプション別配送状態) を、ロック保持下の
// 統一 Update API で永続化する (read-merge-write + 世代 CAS、spec-decision-011)。マージ・監査
// 追記・RetryCount 反映・Status 確定 (settle) をすべて 1 つのロック区間で行うことで、ロック外の
// 素の Put による lost update 窓 (§7 指摘 M-1 / codex blocker) を塞ぐ。これにより 2 active 同時
// 更新でも決着状態 (delivered / dlq) を取りこぼさない (merge precedence)。返り値は確定後の最新
// Manifest。Manifest 更新も永続化点であり、呼び出し側は事前に ensureLease で lease 保持を確認する。
func (p *Pipeline) recordDelivery(m *store.Manifest, t *config.Topic) (*store.Manifest, error) {
	return p.Manifests.Update(m.MessageID, func(base *store.Manifest) error {
		// 配送状態を merge precedence でマージ (決着状態を後退させない)。
		base.MergeSubscriptionsFrom(m)
		// 監査イベントは追記専用。同一性キーで未反映分のみ足す。2 active が別イベントを
		// 同時追記し base に他 active のイベントが先に入っていても、自分の新規イベントを
		// 取りこぼさない (len prefix 比較だと競合時に drop し得るため identity merge にする)。
		base.AppendMissingEvents(m.DeliveryEvents)
		// RetryCount は後退させない。
		if m.RetryCount > base.RetryCount {
			base.RetryCount = m.RetryCount
		}
		// マージ後の最新サブスクリプション状態から Status を導出して確定させる。
		p.settle(base, t)
		return nil
	})
}

// transitionStatus はロック保持下で from → to のメッセージ状態遷移を冪等に行う
// (中間状態 delivering / retrying などの永続化を統一 Update 経由にして lock 外 Put を排除)。
// base が既に from でない (他 active が先行して遷移済み、またはクラッシュ再開で進行済み) 場合は
// 書き込まず no-op とする。遷移自体は domain.ValidateTransition で正当性を担保する。
func (p *Pipeline) transitionStatus(messageID string, from, to domain.MessageStatus) error {
	_, err := p.Manifests.Update(messageID, func(base *store.Manifest) error {
		if base.Status != from {
			return store.ErrSkipManifestUpdate // 既に進行済み: 冪等 no-op
		}
		if err := domain.ValidateTransition(base.Status, to); err != nil {
			return err
		}
		base.Status = to
		return nil
	})
	return err
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

// archiveRelPath はマニフェストの archive_path 値を返す (object-storage-schema)。
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

// settle は配信パス後のサブスクリプション別状態からメッセージのステータスを
// 導出する。設定された全サブスクリプションが delivered → delivered、
// failed が 1 つでもあれば → failed、それ以外で隔離済みが残っていれば dlq。
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

// deliverPending は m の未配信サブスクリプションすべてにアーカイブファイルを
// AtomicWrite で配置し、サブスクリプション別の delivered / failed をマニフェスト
// (メモリ上。永続化は呼び出し側) に記録して失敗数を返す。
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
