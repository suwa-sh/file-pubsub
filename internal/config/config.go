// Package config は単一 YAML 設定ファイル (CTP-003) の読み込みとバリデーションを担う。
// 認証情報などの文字列値は ${ENV_VAR} 展開に対応する (CTP-002)。
// バリデーション失敗は exit code 2 に対応する (ui-design.md)。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// ソース種別 (ui-design.md: ftp / sftp / scp / local / inbox)。
// inbox は push(put)受信モード (REQ-012)。pull 型と排他で Topic ごとに選択する。
const (
	SourceTypeLocal = "local"
	SourceTypeFTP   = "ftp"
	SourceTypeSFTP  = "sftp"
	SourceTypeSCP   = "scp"
	SourceTypeInbox = "inbox"
)

// 元ファイルの扱い (SP-004): delete (収集して削除、デフォルト) / copy (残す)。
const (
	HandlingDelete = "delete"
	HandlingCopy   = "copy"
)

// 完了検知方式 (REQ-014, SPEC-014-03)。push 受信モードで書き込み完了を判定する方式。
// stability が既定 (pull 型と同一の安定待ち)。
const (
	CompletionStability = "stability"
	CompletionRename    = "rename"
	CompletionMarker    = "marker"
)

// rename / marker のサフィックス既定値 (SPEC-014-03)。Producer 規約に合わせ設定可能で、
// 省略時にこの既定が適用される。
const (
	DefaultRenameSuffix = ".tmp"
	DefaultMarkerSuffix = ".done"
)

// Config は単一 YAML 設定の全体を表す (E-001)。
type Config struct {
	PollingInterval  int     `yaml:"polling_interval"`  // 秒
	ArchiveRetention int     `yaml:"archive_retention"` // 日 (SP-006)
	RetryMaxCount    int     `yaml:"retry_max_count"`   // SR-004
	MetricsPort      int     `yaml:"metrics_port"`
	DataDir          string  `yaml:"data_dir"` // デフォルトは config.yaml のあるディレクトリ
	Topics           []Topic `yaml:"topics"`
}

// Topic は論理的なファイル種別 1 つとそのソース / subscription 群を定義する (E-002)。
type Topic struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Source        Source         `yaml:"source"`
	Subscriptions []Subscription `yaml:"subscriptions"`
}

// Source はファイルの収集元を定義する (E-004)。
type Source struct {
	Type                 string         `yaml:"type"`
	Host                 string         `yaml:"host"` // local / inbox では使わない
	Port                 int            `yaml:"port"` // 任意。0 のときはプロトコル既定値
	Directory            string         `yaml:"directory"`
	OriginalFileHandling string         `yaml:"original_file_handling"` // delete (デフォルト) / copy
	StabilityCheck       StabilityCheck `yaml:"stability_check"`
	ExcludePatterns      []string       `yaml:"exclude_patterns"`
	Auth                 Auth           `yaml:"auth"`
	// 以下は push 受信モード (type=inbox) 専用 (REQ-013, REQ-014)。pull 型では使わない。
	Completion           Completion `yaml:"completion"`             // 完了検知方式 (mode + suffix)
	FallbackPollInterval int        `yaml:"fallback_poll_interval"` // 秒。省略時は polling_interval を流用
}

// Completion は push 受信モードの書き込み完了検知設定 (SPEC-014-03)。
type Completion struct {
	Mode   string `yaml:"mode"`   // stability (既定) / rename / marker
	Suffix string `yaml:"suffix"` // rename の一時拡張子・marker のマーカー拡張子。省略時は mode 別の既定値
}

// StabilityCheck は書き込み完了 (安定) 判定の設定 (SP-003)。
type StabilityCheck struct {
	Interval int `yaml:"interval"` // サイズ/mtime の安定観測の間隔 (秒)
}

// Auth はリモートソース用の認証情報を保持する (E-005, CTP-002)。平文も許容するが、
// ${ENV_VAR} 参照と鍵ファイルが推奨形式。
type Auth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	KeyFile  string `yaml:"key_file"`
}

// Subscription は配信先ディレクトリ 1 つを定義する (E-003)。
type Subscription struct {
	Name      string `yaml:"name"`
	Directory string `yaml:"directory"`
}

var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv は raw 中のすべての ${ENV_VAR} 参照を環境変数の値で置換する。
// 未定義の変数への参照はバリデーションエラーとして収集される。
func ExpandEnv(raw []byte) ([]byte, ValidationErrors) {
	var errs ValidationErrors
	seen := map[string]bool{}
	expanded := envRefPattern.ReplaceAllFunc(raw, func(ref []byte) []byte {
		name := string(envRefPattern.FindSubmatch(ref)[1])
		value, ok := os.LookupEnv(name)
		if !ok {
			if !seen[name] {
				seen[name] = true
				errs = append(errs, ValidationError{
					KeyPath: "${" + name + "}",
					Cause:   fmt.Sprintf("environment variable %q is not set", name),
					Remedy:  fmt.Sprintf("export %s before starting, or replace the reference with a literal value", name),
				})
			}
			return ref
		}
		return []byte(value)
	})
	return expanded, errs
}

// Load は path の YAML を読み込み、${ENV_VAR} 参照を展開し、デフォルト値を適用して
// バリデーションする。呼び出し側が exit code 2 に対応づけられるよう、バリデーション
// エラーは ValidationErrors としてまとめて返す。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded, envErrs := ExpandEnv(raw)

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults(path)

	errs := append(envErrs, Validate(&cfg)...)
	if len(errs) > 0 {
		return nil, errs
	}
	return &cfg, nil
}

// applyDefaults は spec がデフォルト値を定める項目を補完する: data_dir は
// config.yaml のあるディレクトリ (object-storage-schema.yaml)、元ファイルの扱いは
// delete がデフォルト (SP-004)。
func (c *Config) applyDefaults(configPath string) {
	if c.DataDir == "" {
		c.DataDir = filepath.Dir(configPath)
	}
	for i := range c.Topics {
		src := &c.Topics[i].Source
		if src.OriginalFileHandling == "" {
			src.OriginalFileHandling = HandlingDelete
		}
		// inbox の完了検知は既定 stability、rename/marker の suffix 省略時は方式別の既定値、
		// フォールバック間隔は省略時 polling_interval を流用 (REQ-013, REQ-014, SPEC-014-03)。
		if src.Type == SourceTypeInbox {
			if src.Completion.Mode == "" {
				src.Completion.Mode = CompletionStability
			}
			if src.Completion.Suffix == "" {
				switch src.Completion.Mode {
				case CompletionRename:
					src.Completion.Suffix = DefaultRenameSuffix
				case CompletionMarker:
					src.Completion.Suffix = DefaultMarkerSuffix
				}
			}
			if src.FallbackPollInterval == 0 {
				src.FallbackPollInterval = c.PollingInterval
			}
		}
	}
}
