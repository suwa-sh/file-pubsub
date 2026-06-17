package config

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ValidationError は設定違反 1 件を「キーパス + 原因 + 対処」で報告する
// (ui-design.md config validate 出力, SR-101)。
type ValidationError struct {
	KeyPath string
	Cause   string
	Remedy  string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("NG: %s\ncause: %s\nremedy: %s", e.KeyPath, e.Cause, e.Remedy)
}

// ValidationErrors は全違反を集約し、呼び出し側が一括報告して exit code 2 で
// 終了できるようにする。
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	msgs := make([]string, len(e))
	for i, v := range e {
		msgs[i] = v.Error()
	}
	return strings.Join(msgs, "\n")
}

// namePattern は topic / subscription 名を安全なパス要素に制限する:
// これらは data_dir 配下 (archive / dlq / processed) のディレクトリ・ファイル名に
// なるため、パス区切り文字やトラバーサル列は拒否しなければならない。
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// isSafeName は name が安全な単一パス要素かどうかを返す。
func isSafeName(name string) bool {
	return name != "." && name != ".." && namePattern.MatchString(name)
}

const nameRemedy = `use only letters, digits, ".", "_" and "-" (no path separators; "." and ".." alone are not allowed)`

// Validate は必須キー・enum 値・名前の重複・参照整合性を検査する (SR-101)。
// 最初の 1 件だけでなく、すべての違反を返す。
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
		} else if !isSafeName(t.Name) {
			add(tp+".name", fmt.Sprintf("topic name %q is not a safe path component", t.Name), nameRemedy)
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
			} else if !isSafeName(s.Name) {
				add(sp+".name", fmt.Sprintf("subscription name %q is not a safe path component", s.Name), nameRemedy)
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

// validateSource はソース定義 1 件を検査し、違反を add で報告する。
func validateSource(s Source, keyPath string, add func(keyPath, cause, remedy string)) {
	switch s.Type {
	case SourceTypeLocal, SourceTypeFTP, SourceTypeSFTP, SourceTypeSCP, SourceTypeInbox:
	case "":
		add(keyPath+".type", "source type is missing", "set type to one of local / ftp / sftp / scp / inbox")
		return
	default:
		add(keyPath+".type", fmt.Sprintf("source type %q is not supported", s.Type), "set type to one of local / ftp / sftp / scp / inbox")
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
	// 安定判定は pull 型と inbox の完了検知=stability で使う。inbox の rename / marker では使わない (SP-014)。
	if requiresStabilityCheck(s) && s.StabilityCheck.Interval <= 0 {
		add(keyPath+".stability_check.interval", "stability check interval must be a positive number of seconds", "set stability_check.interval to a value such as 10")
	}
	for k, p := range s.ExcludePatterns {
		if _, err := path.Match(p, ""); err != nil {
			add(fmt.Sprintf("%s.exclude_patterns[%d]", keyPath, k), fmt.Sprintf("exclude pattern %q is not a valid glob", p), "fix the glob pattern (e.g. *.tmp)")
		}
	}

	switch s.Type {
	case SourceTypeInbox:
		validateInbox(s, keyPath, add)
	case SourceTypeFTP, SourceTypeSFTP, SourceTypeSCP:
		// リモート収集は host と認証情報が要る。local / inbox はローカル FS のため不要。
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

// requiresStabilityCheck は安定判定設定が必須かを返す。pull 型は常に必須。
// inbox は完了検知 mode=stability (既定) のときだけ必須で、rename / marker では使わない (SPEC-014-03)。
func requiresStabilityCheck(s Source) bool {
	if s.Type != SourceTypeInbox {
		return true
	}
	return s.Completion.Mode == "" || s.Completion.Mode == CompletionStability
}

// validateInbox は push 受信モード固有の設定を検査する (REQ-013, REQ-014, SPEC-014-03)。
// 受信ディレクトリは directory を pull 型と共通で流用し、host / auth は使わない。
// completion.suffix は Producer 規約に合わせたリテラル文字列で、省略時は applyDefaults が
// mode 別の既定 (.tmp / .done) を補完するため、ここでは追加検査しない。
func validateInbox(s Source, keyPath string, add func(keyPath, cause, remedy string)) {
	switch s.Completion.Mode {
	case "", CompletionStability, CompletionRename, CompletionMarker:
	default:
		add(keyPath+".completion.mode", fmt.Sprintf("completion mode %q is not supported", s.Completion.Mode), "set completion.mode to stability / rename / marker (default: stability)")
	}
	if s.FallbackPollInterval < 0 {
		add(keyPath+".fallback_poll_interval", "fallback poll interval must not be negative", "set fallback_poll_interval to a positive number of seconds, or omit it to reuse polling_interval")
	}
}
