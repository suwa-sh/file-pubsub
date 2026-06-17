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

// Collect は全トピックに対して収集パスを 1 回実行する: 中断されたアーカイブ
// 昇格の再開後、一覧取得 → 安定判定・除外判定 → 取得 → message_id 採番 →
// work→archive 昇格 → マニフェスト (collected → archived) → 原本ファイル処理
// の順に進む。あるトピックの失敗が他のトピックを止めることはない。
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

// resumeArchiving は中断によって collected 状態のまま残ったメッセージを昇格させる:
// work ファイルが残っていれば再昇格 (冪等な上書き)、archive が既に存在すれば
// マニフェスト更新だけが失われたとみなしてやり直す。
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

	// inbox の rename / marker は完了検知方式そのものが書き込み完了を保証するため即収集する。
	// それ以外 (pull 型 / inbox の stability) は安定判定で完了を待つ。
	if isImmediateInbox(t.Source) {
		p.collectByCompletion(ctx, t, conn, fetchDir, files)
	} else {
		p.collectByStability(ctx, t, conn, fetchDir, files)
	}
	if p.Metrics != nil {
		p.Metrics.SetLastCollected(t.Name, p.now())
	}
	return nil
}

// isImmediateInbox は安定判定をスキップして即収集する inbox 方式 (rename / marker) かを返す。
func isImmediateInbox(s config.Source) bool {
	return s.Type == config.SourceTypeInbox &&
		(s.Completion.Mode == config.CompletionRename || s.Completion.Mode == config.CompletionMarker)
}

// collectByStability は pull 型および inbox の stability 方式の収集を行う。一覧取得 →
// 除外・処理済み判定 → 安定判定 → 収集を、観測値をサイクル間で持ち越しながら進める。
func (p *Pipeline) collectByStability(ctx context.Context, t *config.Topic, conn source.Connector, fetchDir string, files []source.FileInfo) {
	obs := p.topicObservations(t.Name)
	interval := time.Duration(t.Source.StabilityCheck.Interval) * time.Second
	present := map[string]bool{}
	for _, f := range files {
		present[f.Name] = true
		if domain.IsExcluded(f.Name, t.Source.ExcludePatterns) {
			continue
		}
		if p.skipProcessed(t, f) {
			continue
		}
		// delete ハンドリング: ソースに存在するファイルは、名前と mtime が過去の
		// メッセージと一致しても (例: 削除に失敗した原本の残留、cp -p による
		// プロデューサーの再出力)、すべて新しいメッセージとして収集する。
		// at-least-once: アーカイブ保存成功の直後に原本を削除するため、
		// 重複収集は高々 1 メッセージに抑えられる。
		curr := domain.Observation{Name: f.Name, Size: f.Size, ModTime: f.ModTime, ObservedAt: p.now()}
		prev, seen := obs[f.Name]
		if !seen || prev.Size != curr.Size || !prev.ModTime.Equal(curr.ModTime) {
			obs[f.Name] = curr // 初回観測または書き込み途中: 次サイクルへ持ち越す
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
}

// collectByCompletion は inbox の rename / marker 方式の収集を行う。完了検知方式で書き込み
// 完了が確定済みのファイルを安定判定なしで即収集し、marker は収集後にマーカーを後始末する (LR-305)。
func (p *Pipeline) collectByCompletion(ctx context.Context, t *config.Topic, conn source.Connector, fetchDir string, files []source.FileInfo) {
	completion := t.Source.Completion
	var markerReady map[string]bool
	var byName map[string]source.FileInfo
	if completion.Mode == config.CompletionMarker {
		markerReady, byName = markerReadiness(files, completion.Suffix)
	}
	for _, f := range files {
		// rename の一時名・marker のマーカー自身/未レディ本体は収集対象外 (SPEC-014-03)。
		if skipByCompletion(completion, f.Name, markerReady) {
			continue
		}
		if domain.IsExcluded(f.Name, t.Source.ExcludePatterns) {
			continue
		}
		if p.skipProcessed(t, f) {
			continue
		}
		// marker 方式はマーカーを本体の付随ファイルとして渡し、collectFile が Archive 保存成功後に
		// 本体と同じタイミングで後始末する (LR-305)。
		companions, skip := p.markerCompanions(t, completion, f, byName)
		if skip {
			continue
		}
		if err := p.collectFile(ctx, t, conn, fetchDir, f, companions...); err != nil {
			p.emit(logging.Event{Topic: t.Name, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("collect %q: %v. retried on the next polling cycle", f.Name, err)})
			continue
		}
	}
}

// markerCompanions は marker 方式での付随ファイル (マーカー) を返す。copy で残置され既に
// 処理済みのマーカーは新たな完了契機にしないため skip=true を返す (SPEC-014-02)。
func (p *Pipeline) markerCompanions(t *config.Topic, completion config.Completion, f source.FileInfo, byName map[string]source.FileInfo) (companions []source.FileInfo, skip bool) {
	if completion.Mode != config.CompletionMarker {
		return nil, false
	}
	mf, ok := byName[domain.MarkerOf(f.Name, completion.Suffix)]
	if !ok {
		return nil, false
	}
	if t.Source.OriginalFileHandling == config.HandlingCopy && p.markerProcessed(t, mf) {
		return nil, true // 同名再 put の残存処理済みマーカーは新しい未処理マーカーを待つ
	}
	return []source.FileInfo{mf}, false
}

// skipProcessed は copy 設定時に、処理済み管理に記録済み (または照合エラー) のファイルを
// この収集サイクルでスキップすべきかを返す。delete 設定では常に false。
func (p *Pipeline) skipProcessed(t *config.Topic, f source.FileInfo) bool {
	if t.Source.OriginalFileHandling != config.HandlingCopy {
		return false
	}
	done, err := p.Processed.IsProcessed(t.Name, f.Name, f.ModTime, f.Size)
	if err != nil {
		p.emit(logging.Event{Topic: t.Name, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("%v. the file %q stays a re-collection candidate", err, f.Name)})
		return true // 照合できない間は再収集候補として残す (この回はスキップ)
	}
	return done
}

// markerReadiness は marker 方式の取り込み判定に使う、レディ集合と名前→FileInfo の対応を返す。
func markerReadiness(files []source.FileInfo, suffix string) (map[string]bool, map[string]source.FileInfo) {
	names := make([]string, len(files))
	byName := make(map[string]source.FileInfo, len(files))
	for i, f := range files {
		names[i] = f.Name
		byName[f.Name] = f
	}
	return domain.ReadyByMarker(names, suffix), byName
}

// skipByCompletion は inbox の完了検知方式に応じて、ファイルを収集対象から外すべきかを返す。
// rename は一時名 (suffix 付き) を、marker はマーカー自身と対応マーカー未着の本体を除外する (SPEC-014-03)。
func skipByCompletion(c config.Completion, name string, markerReady map[string]bool) bool {
	switch c.Mode {
	case config.CompletionRename:
		return domain.HasCompletionSuffix(name, c.Suffix)
	case config.CompletionMarker:
		if domain.HasCompletionSuffix(name, c.Suffix) {
			return true // マーカー自身は配信対象外
		}
		return !markerReady[name] // 対応マーカー未着の本体は収集しない
	default:
		return false
	}
}

// markerProcessed は marker が既に処理済み管理に記録済みかを返す。copy で残置されたマーカーを
// 新たな完了契機にしないための判定に使う (SPEC-014-02)。照合エラー時は安全側 (処理済み扱い =
// 新たな完了契機にしない) に倒す。skipProcessed と同じ fail-closed 方針。
func (p *Pipeline) markerProcessed(t *config.Topic, mf source.FileInfo) bool {
	done, err := p.Processed.IsProcessed(t.Name, mf.Name, mf.ModTime, mf.Size)
	return err != nil || done
}

// maxMessageIDSeq は uniqueMessageID の連番探索の上限。同一秒の同名衝突は実務上ごく少数で、
// 上限到達は存在確認の継続失敗など異常時のみ。無限ループを防ぐ backstop。
const maxMessageIDSeq = 10000

// uniqueMessageID は base が既存の Manifest と衝突する場合に連番 (_2, _3 …) を付与して
// 一意な message_id を返す (SPEC-007-01)。同一収集秒に同名ファイルを複数回収集しても
// 先行メッセージの Archive・Manifest を上書きしない。存在確認に失敗した場合は既存を
// 上書きしないよう次の候補へ進める (fail-closed)。上限到達時は最後の候補を返す (異常時)。
func (p *Pipeline) uniqueMessageID(base string) string {
	id := base
	for seq := 2; seq <= maxMessageIDSeq; seq++ {
		exists, err := p.Manifests.Exists(id)
		if err == nil && !exists {
			return id // 未使用を確認できた候補を採用
		}
		// 衝突、または存在確認に失敗 → 既存を上書きしないよう次候補へ
		id = fmt.Sprintf("%s_%d", base, seq)
	}
	return id
}

// collectFile は f を収集し (Fetch → work→archive 昇格 → Manifest → 原本処理)、メッセージを発生させる。
// companions は marker 方式のマーカーなど、本体と同じ扱い (回収=削除 / 残す=処理済み記録) で後始末する
// 付随ファイル (LR-305)。原本・付随の処理はいずれも Archive 保存成功後にのみ行う (LR-303)。
func (p *Pipeline) collectFile(ctx context.Context, t *config.Topic, conn source.Connector, fetchDir string, f source.FileInfo, companions ...source.FileInfo) error {
	name := f.Name
	now := p.now()
	msg := domain.Message{
		MessageID:        p.uniqueMessageID(domain.NewMessageID(now, t.Name, name)),
		Topic:            t.Name,
		OriginalFileName: name,
		CollectedAt:      now,
	}

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
		// メッセージは collected のまま残り、次サイクルの resumeArchiving が再試行する。
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. check the archive directory permissions and disk space; retried on the next polling cycle", err)})
		return nil
	}
	if err := p.finalizeArchive(m); err != nil {
		p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "archive_failed", ErrorDetail: fmt.Sprintf("%v. retried on the next polling cycle", err)})
		return nil
	}

	// 原本の処理はアーカイブ保存が成功した後にのみ行う (LR-303)。本体と付随ファイル (marker) を同じ扱いで後始末する。
	p.handleOriginals(ctx, conn, t, m, append([]source.FileInfo{f}, companions...))
	if p.Metrics != nil {
		p.Metrics.IncProcessed(t.Name)
	}
	return nil
}

// handleOriginals は本体と付随ファイルの原本処理を行う: 回収 (delete) は削除、残す (copy) は
// 処理済み管理に記録する。Archive 保存が成功した後にのみ呼ぶこと (LR-303 / LR-305)。
func (p *Pipeline) handleOriginals(ctx context.Context, conn source.Connector, t *config.Topic, m *store.Manifest, originals []source.FileInfo) {
	if t.Source.OriginalFileHandling == config.HandlingCopy {
		for _, o := range originals {
			if err := p.Processed.MarkProcessed(t.Name, o.Name, o.ModTime, o.Size, p.now()); err != nil {
				p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "collect_failed", ErrorDetail: fmt.Sprintf("%v. %q stays a re-collection candidate until the processed record is persisted", err, o.Name)})
			}
		}
		return
	}
	for _, o := range originals {
		if err := conn.Remove(ctx, o.Name); err != nil {
			p.emit(logging.Event{MessageID: m.MessageID, Topic: m.Topic, EventType: "original_delete_failed", ErrorDetail: fmt.Sprintf("remove %q: %v. check the source directory permissions; the delete is retried on the next polling cycle", o.Name, err)})
		}
	}
}

// finalizeArchive はマニフェストを archived に遷移させ、アーカイブパス・保存時刻・
// 保持期限を記録する。以降は何も削除しない: ここから先はアーカイブが
// 正本となる (SP-001)。
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
