package domain

import "time"

// RetentionDeadline はアーカイブの保持期限を返す:
// savedAt + retentionDays (SP-006, manifest_json.retention_deadline)。
func RetentionDeadline(savedAt time.Time, retentionDays int) time.Time {
	return savedAt.AddDate(0, 0, retentionDays)
}

// IsExpired はアーカイブが保持期限を過ぎて削除対象になったかどうかを返す。
// 期限内のファイルは保持しなければならない。
func IsExpired(deadline, now time.Time) bool {
	return now.After(deadline)
}
