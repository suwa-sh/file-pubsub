package domain

import (
	"path"
	"strings"
	"time"
)

// Observation is one polling-cycle observation of a source file, used for the
// write-completion (stability) check (SP-003, LR-203).
type Observation struct {
	Name       string
	Size       int64
	ModTime    time.Time
	ObservedAt time.Time
}

// IsStable reports whether the file is considered fully written: size and
// mtime are unchanged between the two observations and at least
// stabilityInterval has elapsed between them. Files still being written are
// carried over to the next cycle.
func IsStable(prev, curr Observation, stabilityInterval time.Duration) bool {
	if prev.Size != curr.Size || !prev.ModTime.Equal(curr.ModTime) {
		return false
	}
	return curr.ObservedAt.Sub(prev.ObservedAt) >= stabilityInterval
}

// IsExcluded reports whether the file name must be skipped by collection.
// Temporary names (*.tmp) are always excluded so in-flight files never enter
// the pipeline (LR-303); other glob patterns come from the source config.
// Invalid patterns do not match (they are rejected by config validation).
func IsExcluded(name string, patterns []string) bool {
	if strings.HasSuffix(name, ".tmp") {
		return true
	}
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}
