package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ArchiveStore は収集ワークエリア (work/collect/{topic}/{message_id}) と topic の
// アーカイブ (archive/{topic}/{message_id}) を管理する。すべてのメッセージは
// fan-out 開始前にアーカイブされなければならない (SP-001)。message_id をファイル名
// にすることで、同名ファイルの再連携が履歴を上書きしないことを保証する (SR-002)。
type ArchiveStore struct {
	workDir    string
	archiveDir string
}

// NewArchiveStore は dataDir/work/collect と dataDir/archive を起点とするストアを返す。
func NewArchiveStore(dataDir string) *ArchiveStore {
	return &ArchiveStore{
		workDir:    filepath.Join(dataDir, "work", "collect"),
		archiveDir: filepath.Join(dataDir, "archive"),
	}
}

// WorkPath は work/collect/{topic}/{message_id} を返す。
func (s *ArchiveStore) WorkPath(topic, messageID string) string {
	return filepath.Join(s.workDir, topic, messageID)
}

// ArchivePath は archive/{topic}/{message_id} を返す。
func (s *ArchiveStore) ArchivePath(topic, messageID string) string {
	return filepath.Join(s.archiveDir, topic, messageID)
}

// PutWork は収集した内容を AtomicWrite でワークエリアに保存する
// (一時名 {message_id}.tmp → rename — LR-303)。
func (s *ArchiveStore) PutWork(topic, messageID string, r io.Reader) error {
	if err := WriteFileAtomic(s.WorkPath(topic, messageID), r, 0o644); err != nil {
		return fmt.Errorf("put work %s/%s: %w", topic, messageID, err)
	}
	return nil
}

// Promote はワークファイルを AtomicWrite でアーカイブにコピーし、その後ワーク
// ファイルを削除する (以降はアーカイブが正本)。中断後の再実行は同じアーカイブ
// パスを冪等に上書きする。
func (s *ArchiveStore) Promote(topic, messageID string) error {
	if err := CopyFileAtomic(s.WorkPath(topic, messageID), s.ArchivePath(topic, messageID)); err != nil {
		return fmt.Errorf("promote %s/%s to archive: %w", topic, messageID, err)
	}
	if err := os.Remove(s.WorkPath(topic, messageID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("promote %s/%s: remove work file: %w", topic, messageID, err)
	}
	return nil
}

// Open はアーカイブ済みファイルを読み取り用に開く (fan-out / retry / replay の供給元)。
func (s *ArchiveStore) Open(topic, messageID string) (io.ReadCloser, error) {
	f, err := os.Open(s.ArchivePath(topic, messageID))
	if err != nil {
		return nil, fmt.Errorf("open archive %s/%s: %w", topic, messageID, err)
	}
	return f, nil
}

// Exists はアーカイブファイルが最終名で存在するかどうかを返す。
func (s *ArchiveStore) Exists(topic, messageID string) (bool, error) {
	_, err := os.Stat(s.ArchivePath(topic, messageID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat archive %s/%s: %w", topic, messageID, err)
}

// ListMessageIDs は topic のアーカイブ済み message_id を昇順で返す
// (retention スキャンの入力)。一時名は無視される。
func (s *ArchiveStore) ListMessageIDs(topic string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.archiveDir, topic))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list archive %s: %w", topic, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), tempSuffix) {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

// Delete は期限切れのアーカイブファイルを 1 件削除する (retention 削除, SP-006)。
// ファイルが無いことはエラーではない (冪等な再実行)。
func (s *ArchiveStore) Delete(topic, messageID string) error {
	if err := os.Remove(s.ArchivePath(topic, messageID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete archive %s/%s: %w", topic, messageID, err)
	}
	return nil
}

// CleanupWorkTempFiles は topic のワークエリアに残った *.tmp ファイルを削除する
// (ダウンロード中断後の冪等な再起動)。
func (s *ArchiveStore) CleanupWorkTempFiles(topic string) error {
	return CleanupTempFiles(filepath.Join(s.workDir, topic))
}
