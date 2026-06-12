package domain

import "time"

// RetentionDeadline returns the archive retention deadline:
// savedAt + retentionDays (SP-006, manifest_json.retention_deadline).
func RetentionDeadline(savedAt time.Time, retentionDays int) time.Time {
	return savedAt.AddDate(0, 0, retentionDays)
}

// IsExpired reports whether the archive passed its retention deadline and is
// a deletion target. Files within the deadline must be kept.
func IsExpired(deadline, now time.Time) bool {
	return now.After(deadline)
}
