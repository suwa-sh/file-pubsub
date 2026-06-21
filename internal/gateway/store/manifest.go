package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
)

// message_id 更新ロック・世代 CAS の有限回リトライ設定 (spec-decision-011)。
// ロック取得失敗 (他者保持) も世代不一致 (CAS 失敗) も有限回であきらめ fail-closed
// にする。上限超過は配信保留であり、衝突を楽観視した上書きはしない。
const (
	manifestLockSuffix  = ".lock"
	manifestLockRetries = 50
	manifestLockBackoff = 10 * time.Millisecond
	manifestCASRetries  = 10

	// manifestLockStaleAfter は更新ロックを「holder クラッシュで残存した stale」と
	// 判定する経過時間。更新ロックは read-merge-write の間 (ミリ秒オーダー) だけ保持
	// するため、リトライ窓 (50×10ms=500ms) や正常な保持時間より十分大きいこの値を
	// 超えて残るロックは異常終了の置き土産とみなす (fix D / spec-decision-011 の
	// 「次回更新時に stale 相当で吸収」)。
	manifestLockStaleAfter = 60 * time.Second

	// エラーラップ用のメッセージプレフィックス (重複を避ける)。
	errAcquireLockFmt = "acquire manifest lock %s: %w"
	errUpdateFmt      = "update manifest %s: %w"
)

// ErrManifestLocked は message_id 更新ロックをリトライ上限まで取得できなかったこと
// を表す fail-closed のエラー (配信保留)。
var ErrManifestLocked = errors.New("manifest update lock is held by another writer")

// ErrManifestCAS は世代 CAS がリトライ上限まで一致せず rename を確定できなかった
// ことを表す fail-closed のエラー。
var ErrManifestCAS = errors.New("manifest revision changed concurrently")

// ErrSkipManifestUpdate は Update の mutate コールバックが「書き込み不要 (no-op)」を
// 表明するための sentinel。冪等な状態遷移 (既に他 active が先行して遷移済みの場合等)
// で書き込みをスキップする。Update はこれを受け取ると rename せず現在の base を返す。
var ErrSkipManifestUpdate = errors.New("manifest update skipped: no change required")

// Manifest はメッセージ単位の配信記録 (manifest/{message_id}.json)。
// フィールド名は object-storage-schema.yaml schemas.manifest_json に厳密に従う。
// manifest は配信状態の唯一の正本である (CTR-003)。
type Manifest struct {
	MessageID         string                 `json:"message_id"`
	Topic             string                 `json:"topic"`
	OriginalFileName  string                 `json:"original_file_name"`
	CollectedAt       time.Time              `json:"collected_at"`
	Status            domain.MessageStatus   `json:"status"`
	ArchivePath       string                 `json:"archive_path,omitempty"`
	SavedAt           *time.Time             `json:"saved_at,omitempty"`
	RetentionDeadline *time.Time             `json:"retention_deadline,omitempty"`
	Subscriptions     []SubscriptionDelivery `json:"subscriptions"`
	RetryCount        int                    `json:"retry_count"`
	DeliveredAt       *time.Time             `json:"delivered_at,omitempty"`
	ReplayRecords     []ReplayRecord         `json:"replay_records,omitempty"`
	DeliveryEvents    []DeliveryEvent        `json:"delivery_events,omitempty"`
	// Revision は更新世代カウンタ (object-storage-schema manifest_json.revision)。
	// 冗長構成での read-merge-write + 世代 CAS (spec-decision-011) に用い、PutMerged
	// が更新ごとに +1 する。ロック保持下の rename 直前に再読込した revision と read 時
	// の revision を比較し、一致する場合のみ rename を確定する。
	Revision int `json:"revision,omitempty"`
}

// SubscriptionDelivery は manifest_json.subscriptions の要素 1 つ。
type SubscriptionDelivery struct {
	Subscription string                    `json:"subscription"`
	Status       domain.SubscriptionStatus `json:"status"`
	DeliveredAt  *time.Time                `json:"delivered_at,omitempty"`
	LastError    string                    `json:"last_error,omitempty"`
}

// ReplayRecord は manifest_json.replay_records の要素 1 つ (SP-102)。
type ReplayRecord struct {
	ReplayedAt          time.Time `json:"replayed_at"`
	TargetSubscriptions []string  `json:"target_subscriptions"`
	Result              string    `json:"result"`
}

// DeliveryEvent は追記専用の監査ログ manifest_json.delivery_events の要素 1 つ
// (NFR E.7.1.1)。
type DeliveryEvent struct {
	At           time.Time `json:"at"`
	Subscription string    `json:"subscription,omitempty"`
	EventType    string    `json:"event_type"`
	Detail       string    `json:"detail,omitempty"`
}

// NewManifest は collected 状態の初期記録を生成する (Collect UC)。
func NewManifest(msg domain.Message) *Manifest {
	return &Manifest{
		MessageID:        msg.MessageID,
		Topic:            msg.Topic,
		OriginalFileName: msg.OriginalFileName,
		CollectedAt:      msg.CollectedAt,
		Status:           domain.StatusCollected,
		Subscriptions:    []SubscriptionDelivery{},
	}
}

// SetSubscriptionState は subscription 1 件の現在の配信状態を記録し、以前の現在
// 状態を置き換える (配信履歴は DeliveryEvents に残す)。
func (m *Manifest) SetSubscriptionState(name string, status domain.SubscriptionStatus, deliveredAt *time.Time, lastError string) {
	for i := range m.Subscriptions {
		if m.Subscriptions[i].Subscription == name {
			m.Subscriptions[i].Status = status
			m.Subscriptions[i].DeliveredAt = deliveredAt
			m.Subscriptions[i].LastError = lastError
			return
		}
	}
	m.Subscriptions = append(m.Subscriptions, SubscriptionDelivery{
		Subscription: name,
		Status:       status,
		DeliveredAt:  deliveredAt,
		LastError:    lastError,
	})
}

// SubscriptionStates は subscription 名から現在の配信状態へのマップを返す。
// 冪等再配信判定 (SR-003) の入力となる。
func (m *Manifest) SubscriptionStates() map[string]domain.SubscriptionStatus {
	states := make(map[string]domain.SubscriptionStatus, len(m.Subscriptions))
	for _, s := range m.Subscriptions {
		states[s.Subscription] = s.Status
	}
	return states
}

// AppendEvent は監査ログに配信イベントを 1 件追記する。
func (m *Manifest) AppendEvent(e DeliveryEvent) {
	m.DeliveryEvents = append(m.DeliveryEvents, e)
}

// eventKey は監査イベントの同一性キー。追記の重複・取りこぼし判定に用いる。
func eventKey(e DeliveryEvent) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", e.At.UnixNano(), e.Subscription, e.EventType, e.Detail)
}

// AppendMissingEvents は events のうち自身にまだ無いものだけを追記する (追記専用の監査
// ログ、merge precedence と同様に lost update を避ける)。ロック保持下の Update コールバック
// から呼び、ロック保持下で再読込した base に対し「このサイクルで生成された新規イベント」を
// 同一性キーで判定して足す。単純な len prefix 比較と違い、2 active が別イベントを同時追記
// しても、再読込した base に他 active のイベントが含まれていても自分の新規分を取りこぼさない。
func (m *Manifest) AppendMissingEvents(events []DeliveryEvent) {
	if len(events) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(m.DeliveryEvents))
	for _, e := range m.DeliveryEvents {
		existing[eventKey(e)] = struct{}{}
	}
	for _, e := range events {
		k := eventKey(e)
		if _, ok := existing[k]; ok {
			continue
		}
		existing[k] = struct{}{}
		m.DeliveryEvents = append(m.DeliveryEvents, e)
	}
}

// AppendReplay は replay 記録を 1 件追記する (SP-102)。
func (m *Manifest) AppendReplay(r ReplayRecord) {
	m.ReplayRecords = append(m.ReplayRecords, r)
}

// ManifestStore は manifest/{message_id}.json の読み書きを行う。
type ManifestStore struct {
	dir string
	// beforeUpdateRename は Update の rename 直前 (staging 書き込み後・lock token 確認前) に
	// 呼ばれるテスト専用フック。本番では nil。stale 回収で lock を奪われる競合を決定的に
	// 再現するために用いる。
	beforeUpdateRename func()
}

// NewManifestStore は dataDir/manifest を起点とするストアを返す。
func NewManifestStore(dataDir string) *ManifestStore {
	return &ManifestStore{dir: filepath.Join(dataDir, "manifest")}
}

func (s *ManifestStore) path(messageID string) string {
	return filepath.Join(s.dir, messageID+".json")
}

// Get は messageID の manifest を読み込む。
func (s *ManifestStore) Get(messageID string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(s.path(messageID), &m); err != nil {
		return nil, fmt.Errorf("get manifest %s: %w", messageID, err)
	}
	return &m, nil
}

// Exists は messageID の manifest が既に存在するかを返す。message_id の一意採番
// (同一収集秒の衝突回避) に使う (SPEC-007-01)。
func (s *ManifestStore) Exists(messageID string) (bool, error) {
	_, err := os.Stat(s.path(messageID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat manifest %s: %w", messageID, err)
}

// Put は manifest を AtomicWrite で永続化する。
func (s *ManifestStore) Put(m *Manifest) error {
	if m.MessageID == "" {
		return fmt.Errorf("put manifest: message_id is empty")
	}
	if m.Subscriptions == nil {
		m.Subscriptions = []SubscriptionDelivery{}
	}
	if err := WriteJSONAtomic(s.path(m.MessageID), m); err != nil {
		return fmt.Errorf("put manifest %s: %w", m.MessageID, err)
	}
	return nil
}

// isSettled は決着状態 (delivered / dlq) かを返す。決着状態は merge precedence で
// 保持され中間状態 (failed) では上書きされない。
func isSettled(s domain.SubscriptionStatus) bool {
	return s == domain.SubscriptionDelivered || s == domain.SubscriptionDLQ
}

// MergeSubscriptionsFrom は upd の subscription 別配送状態を merge precedence で自身へ
// マージする (決着状態を後退させない)。ロック保持下の Update コールバックから呼び出して
// 配送結果を確定させるための公開エントリ (usecase 層が利用する)。
func (m *Manifest) MergeSubscriptionsFrom(upd *Manifest) {
	mergeSubscriptions(m, upd)
}

// mergeSubscriptions は base (read した既存) に upd (自分の更新) を merge precedence
// でマージする (計算ルール「Manifest マージの merge precedence」)。
//   - 決着状態 (delivered / dlq) は保持・上書き不可。
//   - 既存が決着状態の Subscription は自分の中間状態 (failed 等) で後退させない。
//   - 既存が failed / 未記録なら upd の状態を採用する (failed → delivered の昇格を含む)。
func mergeSubscriptions(base, upd *Manifest) {
	for _, u := range upd.Subscriptions {
		existing, found := findSubscription(base, u.Subscription)
		if found && isSettled(existing.Status) {
			// 既存が決着状態: 上書きしない (lost update 回避)。
			continue
		}
		base.SetSubscriptionState(u.Subscription, u.Status, u.DeliveredAt, u.LastError)
	}
}

// findSubscription は base から name の現在状態を探す。
func findSubscription(base *Manifest, name string) (SubscriptionDelivery, bool) {
	for _, s := range base.Subscriptions {
		if s.Subscription == name {
			return s, true
		}
	}
	return SubscriptionDelivery{}, false
}

// Update は既存 Manifest への更新を message_id 単位の更新ロックで直列化し、ロック保持下で
// read → mutate(コールバック) → 世代 CAS → write を行う統一更新 API (spec-decision-011)。
// 既存 Manifest への配送状態・状態遷移・監査追記はすべてこれを経由し、ロック外の素の Put に
// よる lost update 窓を作らない。新規 message の初回作成のみ Put を使う。
//
// 手順:
//  1. manifest/{message_id}.json.lock を O_CREATE|O_EXCL で取得し直列化する。取得失敗
//     (他者保持) は短いバックオフで有限回リトライ。上限超過後、ロックが stale (holder
//     クラッシュ) なら一度だけ remove して再取得を試みる (fix D)。なお回収できなければ
//     ErrManifestLocked で fail-closed (配信保留、上書きしない)。
//  2. ロック保持下で対象 Manifest を read し revision を観測 → mutate で base を直接変更
//     → 一時名へ書く。mutate が ErrSkipManifestUpdate を返した場合は書き込まず base を返す
//     (冪等な遷移スキップ)。
//  3. rename 直前に Manifest を再読込し revision が観測時と一致する場合のみ rename を確定
//     する。不一致なら一時ファイルを破棄して read からやり直す (有限回)。上限超過は
//     ErrManifestCAS で fail-closed。
//
// 返り値は更新後に永続化された Manifest。
func (s *ManifestStore) Update(messageID string, mutate func(*Manifest) error) (*Manifest, error) {
	if messageID == "" {
		return nil, fmt.Errorf("update manifest: message_id is empty")
	}
	release, token, err := s.acquireLock(messageID)
	if err != nil {
		return nil, err
	}
	defer release()
	dst := s.path(messageID)
	lockPath := dst + manifestLockSuffix
	// staging は token 固有名にして、2 writer が同時に lock を保持し得る病的窓でも
	// 互いの staging を clobber しないようにする。最終的に rename で消えるが、途中異常時の
	// 取り残しに備え defer で後始末する。
	staging := dst + ".merge." + token
	defer func() { _ = os.Remove(staging) }()
	for attempt := 0; attempt < manifestCASRetries; attempt++ {
		// read: 現在の Manifest と観測 revision を得る。
		base, err := s.Get(messageID)
		if err != nil {
			return nil, fmt.Errorf(errUpdateFmt, messageID, err)
		}
		observed := base.Revision

		// mutate: ロック保持下で read した base を直接変更する。
		if err := mutate(base); err != nil {
			if errors.Is(err, ErrSkipManifestUpdate) {
				return base, nil // 書き込み不要 (冪等な no-op)
			}
			return nil, err
		}
		base.Revision = observed + 1
		if base.Subscriptions == nil {
			base.Subscriptions = []SubscriptionDelivery{}
		}

		// write: マージ後の内容を token 固有の staging ファイルへ完全に書き出す。
		if err := WriteJSONAtomic(staging, base); err != nil {
			return nil, fmt.Errorf(errUpdateFmt, messageID, err)
		}

		// rename 直前のガード (相互排他の最終確認):
		//  1. lock 所有確認: lock token が自分のものか。stale 回収で lock を奪われていたら
		//     書き込まず fail-closed (奪取した writer の更新を clobber しない)。
		//  2. 世代 CAS: revision が観測時と一致する場合のみ確定。不一致なら read からやり直す。
		if s.beforeUpdateRename != nil {
			s.beforeUpdateRename() // テスト専用: token 確認前に lock 奪取を差し込む
		}
		held, hErr := lockHeldBy(lockPath, token)
		if hErr != nil || !held {
			return nil, fmt.Errorf(errUpdateFmt, messageID, ErrManifestLocked)
		}
		current, err := s.Get(messageID)
		if err != nil {
			return nil, fmt.Errorf(errUpdateFmt, messageID, err)
		}
		if current.Revision != observed {
			// 世代が変化 (他 active が更新): read からやり直す (staging は次周回で上書き)。
			continue
		}
		if err := os.Rename(staging, dst); err != nil {
			return nil, fmt.Errorf(errUpdateFmt, messageID, err)
		}
		return base, nil
	}
	// 上限超過: 配信保留 (fail-closed)。
	return nil, fmt.Errorf(errUpdateFmt, messageID, ErrManifestCAS)
}

// PutMerged は upd の subscription 別配送状態を merge precedence でマージする Update の
// 特化版 (spec-decision-011)。Update に集約された read-merge-write + 世代 CAS を用いる。
func (s *ManifestStore) PutMerged(upd *Manifest) (*Manifest, error) {
	if upd.MessageID == "" {
		return nil, fmt.Errorf("put merged manifest: message_id is empty")
	}
	return s.Update(upd.MessageID, func(base *Manifest) error {
		mergeSubscriptions(base, upd)
		return nil
	})
}

// acquireLock は message_id 更新ロックを O_CREATE|O_EXCL で取得し、解放クロージャを
// 返す (UC「冪等に処理を再開する」の message_id ロック)。取得失敗 (他者保持で既に存在)
// は有限回リトライする。上限超過後、ロックが stale (holder クラッシュで残存) であれば
// 一度だけ remove して再取得を試みる (fix D)。それでも取得できなければ ErrManifestLocked
// で fail-closed (配信保留、上書きしない)。
func (s *ManifestStore) acquireLock(messageID string) (release func(), token string, err error) {
	lockPath := s.path(messageID) + manifestLockSuffix
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, "", fmt.Errorf(errAcquireLockFmt, messageID, err)
	}
	// owner token: 自分が取得した lock を一意に識別する。release は token 一致時のみ削除し、
	// stale 回収で別 writer に奪われた lock を旧 holder 復帰時に誤って消さないようにする。
	// Update は rename 直前に token 一致を再確認し、token 固有 staging を使うことで、2 writer
	// が同時に lock を保持し得る病的窓 (>stale TTL 保持後の回収) でも書き込みを取りこぼさない。
	token, err = newLockToken()
	if err != nil {
		return nil, "", fmt.Errorf(errAcquireLockFmt, messageID, err)
	}
	release = func() {
		if held, _ := lockHeldBy(lockPath, token); held {
			_ = os.Remove(lockPath)
		}
	}
	for attempt := 0; attempt < manifestLockRetries; attempt++ {
		if err := writeLockExclusive(lockPath, token); err == nil {
			return release, token, nil
		} else if !os.IsExist(err) {
			return nil, "", fmt.Errorf(errAcquireLockFmt, messageID, err)
		}
		time.Sleep(manifestLockBackoff)
	}
	// 上限超過: stale ロック (holder クラッシュの置き土産) なら一度だけ回収して再取得を
	// 試みる。stale 判定は lock ファイルの mtime 経過時間で行う (正常な保持は ms オーダー
	// のため manifestLockStaleAfter を超えるものは異常終了とみなせる)。回収を巡る競合では
	// O_EXCL の勝者のみが取得し、敗者は fail-closed のまま (上書きを楽観視しない)。
	if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > manifestLockStaleAfter {
		_ = os.Remove(lockPath)
		if err := writeLockExclusive(lockPath, token); err == nil {
			return release, token, nil
		}
	}
	// stale でない (= 取得競合中) / 回収に敗北: 衝突を楽観視せず配信保留 (fail-closed)。
	return nil, "", fmt.Errorf(errAcquireLockFmt, messageID, ErrManifestLocked)
}

// lockHeldBy は lockPath の owner token が token と一致する (= 自分が保持している) かを返す。
func lockHeldBy(lockPath, token string) (bool, error) {
	cur, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(cur)) == token, nil
}

// writeLockExclusive は lockPath を O_CREATE|O_EXCL で作成し owner token を書き込む。
// EEXIST はそのまま返す (呼び出し側が取得競合を判定する)。
func writeLockExclusive(lockPath, token string) error {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return err
	}
	return f.Close()
}

// newLockToken は lock の owner token (ランダム 16 バイトの hex) を返す。
func newLockToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate lock token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// List はすべての manifest を message_id 順 (ファイル名昇順, SR-005) で読み込む。
func (s *ManifestStore) List() ([]*Manifest, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list manifests: %w", err)
	}
	var manifests []*Manifest
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		m, err := s.Get(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, m)
	}
	return manifests, nil
}
