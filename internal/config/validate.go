package config

import (
	"fmt"
	"path"
	"strings"
)

// ValidationError reports one config violation as "key path + cause + remedy"
// (ui-design.md config validate output, SR-101).
type ValidationError struct {
	KeyPath string
	Cause   string
	Remedy  string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("NG: %s\ncause: %s\nremedy: %s", e.KeyPath, e.Cause, e.Remedy)
}

// ValidationErrors aggregates every violation so the caller can report all of
// them at once and exit with code 2.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	msgs := make([]string, len(e))
	for i, v := range e {
		msgs[i] = v.Error()
	}
	return strings.Join(msgs, "\n")
}

// Validate checks required keys, enum values, name duplication and reference
// integrity (SR-101). It returns all violations, not just the first one.
func Validate(c *Config) ValidationErrors {
	var errs ValidationErrors
	add := func(keyPath, cause, remedy string) {
		errs = append(errs, ValidationError{KeyPath: keyPath, Cause: cause, Remedy: remedy})
	}

	if c.PollingInterval <= 0 {
		add("polling_interval", "polling interval must be a positive number of seconds", "set polling_interval to a value such as 60")
	}
	if c.ArchiveRetention <= 0 {
		add("archive_retention", "archive retention must be a positive number of days", "set archive_retention to a value such as 90")
	}
	if c.RetryMaxCount <= 0 {
		add("retry_max_count", "retry max count must be a positive number", "set retry_max_count to a value such as 5")
	}
	if c.MetricsPort <= 0 || c.MetricsPort > 65535 {
		add("metrics_port", "metrics port must be between 1 and 65535", "set metrics_port to a free port such as 9090")
	}
	if len(c.Topics) == 0 {
		add("topics", "no topics are defined", "define at least one topic with a source and subscriptions")
	}

	topicNames := map[string]bool{}
	for i, t := range c.Topics {
		tp := fmt.Sprintf("topics[%d]", i)
		if t.Name == "" {
			add(tp+".name", "topic name is missing", "set a unique topic name")
		} else if topicNames[t.Name] {
			add(tp+".name", fmt.Sprintf("topic name %q is duplicated", t.Name), "make every topic name unique")
		} else {
			topicNames[t.Name] = true
		}

		validateSource(t.Source, tp+".source", add)

		if len(t.Subscriptions) == 0 {
			add(tp+".subscriptions", "no subscriptions are defined for the topic", "define at least one subscription with a target directory")
		}
		subNames := map[string]bool{}
		for j, s := range t.Subscriptions {
			sp := fmt.Sprintf("%s.subscriptions[%d]", tp, j)
			if s.Name == "" {
				add(sp+".name", "subscription name is missing", "set a subscription name such as current / next")
			} else if subNames[s.Name] {
				add(sp+".name", fmt.Sprintf("subscription name %q is duplicated within the topic", s.Name), "make subscription names unique within the topic")
			} else {
				subNames[s.Name] = true
			}
			if s.Directory == "" {
				add(sp+".directory", "target directory path is missing", "set the directory the subscription delivers to")
			}
		}
	}
	return errs
}

func validateSource(s Source, keyPath string, add func(keyPath, cause, remedy string)) {
	switch s.Type {
	case SourceTypeLocal, SourceTypeFTP, SourceTypeSFTP, SourceTypeSCP:
	case "":
		add(keyPath+".type", "source type is missing", "set type to one of local / ftp / sftp / scp")
		return
	default:
		add(keyPath+".type", fmt.Sprintf("source type %q is not supported", s.Type), "set type to one of local / ftp / sftp / scp")
		return
	}

	if s.Directory == "" {
		add(keyPath+".directory", "source directory path is missing", "set the directory to collect files from")
	}
	if s.Port < 0 || s.Port > 65535 {
		add(keyPath+".port", "port must be between 0 and 65535 (0 = protocol default)", "set a valid port or omit it")
	}
	switch s.OriginalFileHandling {
	case HandlingDelete, HandlingCopy:
	default:
		add(keyPath+".original_file_handling", fmt.Sprintf("original file handling %q is not supported", s.OriginalFileHandling), "set original_file_handling to delete or copy")
	}
	if s.StabilityCheck.Interval <= 0 {
		add(keyPath+".stability_check.interval", "stability check interval must be a positive number of seconds", "set stability_check.interval to a value such as 10")
	}
	for k, p := range s.ExcludePatterns {
		if _, err := path.Match(p, ""); err != nil {
			add(fmt.Sprintf("%s.exclude_patterns[%d]", keyPath, k), fmt.Sprintf("exclude pattern %q is not a valid glob", p), "fix the glob pattern (e.g. *.tmp)")
		}
	}

	if s.Type != SourceTypeLocal {
		if s.Host == "" {
			add(keyPath+".host", "host is required for remote source types", "set the host of the remote file area")
		}
		if s.Auth.Username == "" {
			add(keyPath+".auth.username", "username is required for remote source types", "set auth.username (a ${ENV_VAR} reference is recommended)")
		}
		if s.Auth.Password == "" && s.Auth.KeyFile == "" {
			add(keyPath+".auth", "either password or key_file is required for remote source types", "set auth.password (a ${ENV_VAR} reference is recommended) or auth.key_file")
		}
	}
}
