package config

import "testing"

// inboxYAML は push 受信モード(inbox)の最小設定テンプレート。
// %s に completion ブロック等を差し込んで使う。
const inboxYAMLHead = `
polling_interval: 60
archive_retention: 90
retry_max_count: 5
metrics_port: 9090
topics:
  - name: invoices
    source:
      type: inbox
      directory: /inbox/invoices
      stability_check:
        interval: 10
`

const inboxYAMLTail = `
    subscriptions:
      - name: current
        directory: /pub/invoices/current
`

func TestLoad_inbox完了検知を省略した場合_modeがstabilityでsuffixは空かつfallbackがpolling_intervalになること(t *testing.T) {
	// Arrange
	path := writeConfig(t, inboxYAMLHead+inboxYAMLTail)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	src := cfg.Topics[0].Source
	if src.Completion.Mode != CompletionStability {
		t.Errorf("default completion mode = %q, want %q", src.Completion.Mode, CompletionStability)
	}
	if src.Completion.Suffix != "" {
		t.Errorf("stability completion must not set a suffix, got %q", src.Completion.Suffix)
	}
	if src.FallbackPollInterval != cfg.PollingInterval {
		t.Errorf("fallback_poll_interval default = %d, want polling_interval %d", src.FallbackPollInterval, cfg.PollingInterval)
	}
}

func TestLoad_inboxのrename方式でsuffixを省略した場合_既定のtmpになること(t *testing.T) {
	// Arrange
	path := writeConfig(t, inboxYAMLHead+"      completion:\n        mode: rename\n"+inboxYAMLTail)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Topics[0].Source.Completion.Suffix; got != DefaultRenameSuffix {
		t.Errorf("rename default suffix = %q, want %q", got, DefaultRenameSuffix)
	}
}

func TestLoad_inboxのmarker方式でsuffixを省略した場合_既定のdoneになること(t *testing.T) {
	// Arrange
	path := writeConfig(t, inboxYAMLHead+"      completion:\n        mode: marker\n"+inboxYAMLTail)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Topics[0].Source.Completion.Suffix; got != DefaultMarkerSuffix {
		t.Errorf("marker default suffix = %q, want %q", got, DefaultMarkerSuffix)
	}
}

func TestLoad_inboxのrename方式でsuffixを明示した場合_その値が保持されること(t *testing.T) {
	// Arrange
	path := writeConfig(t, inboxYAMLHead+"      completion:\n        mode: rename\n        suffix: .part\n"+inboxYAMLTail)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Topics[0].Source.Completion.Suffix; got != ".part" {
		t.Errorf("explicit suffix = %q, want %q", got, ".part")
	}
}

func TestLoad_inboxでfallback_poll_intervalを明示した場合_その値が保持されること(t *testing.T) {
	// Arrange
	path := writeConfig(t, inboxYAMLHead+"      fallback_poll_interval: 5\n"+inboxYAMLTail)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Topics[0].Source.FallbackPollInterval; got != 5 {
		t.Errorf("explicit fallback_poll_interval = %d, want 5", got)
	}
}

func TestValidate_inboxの完了検知方式が有効な場合_違反が無いこと(t *testing.T) {
	// Arrange
	modes := []string{CompletionStability, CompletionRename, CompletionMarker}
	for _, mode := range modes {
		t.Run(mode+"の場合_違反が無いこと", func(t *testing.T) {
			// Arrange
			cfg := inboxConfig(mode, "")

			// Act
			verrs := Validate(cfg)

			// Assert
			if len(verrs) != 0 {
				t.Errorf("valid inbox (mode=%s) must pass, got %v", mode, verrs)
			}
		})
	}
}

func TestValidate_inboxで未対応の完了検知modeの場合_completionのエラーになること(t *testing.T) {
	// Arrange
	cfg := inboxConfig("bogus", "")

	// Act
	verrs := Validate(cfg)

	// Assert
	assertKeyPath(t, verrs, "topics[0].source.completion.mode")
}

func TestValidate_inboxのstability方式でintervalが未設定の場合_エラーになること(t *testing.T) {
	// Arrange
	cfg := inboxConfig(CompletionStability, "")
	cfg.Topics[0].Source.StabilityCheck.Interval = 0

	// Act
	verrs := Validate(cfg)

	// Assert
	assertKeyPath(t, verrs, "topics[0].source.stability_check.interval")
}

func TestValidate_inboxのrename方式でstability_checkが未設定でも違反が無いこと(t *testing.T) {
	// Arrange
	cfg := inboxConfig(CompletionRename, ".tmp")
	cfg.Topics[0].Source.StabilityCheck.Interval = 0

	// Act
	verrs := Validate(cfg)

	// Assert
	if len(verrs) != 0 {
		t.Errorf("rename mode must not require stability_check, got %v", verrs)
	}
}

func TestValidate_inboxのmarker方式でstability_checkが未設定でも違反が無いこと(t *testing.T) {
	// Arrange
	cfg := inboxConfig(CompletionMarker, ".done")
	cfg.Topics[0].Source.StabilityCheck.Interval = 0

	// Act
	verrs := Validate(cfg)

	// Assert
	if len(verrs) != 0 {
		t.Errorf("marker mode must not require stability_check, got %v", verrs)
	}
}

func TestValidate_inboxのfallback_poll_intervalが負の場合_エラーになること(t *testing.T) {
	// Arrange
	cfg := inboxConfig(CompletionStability, "")
	cfg.Topics[0].Source.FallbackPollInterval = -1

	// Act
	verrs := Validate(cfg)

	// Assert
	assertKeyPath(t, verrs, "topics[0].source.fallback_poll_interval")
}

func TestValidate_inboxではhostやauthが無くても違反が無いこと(t *testing.T) {
	// Arrange
	cfg := inboxConfig(CompletionStability, "")

	// Act
	verrs := Validate(cfg)

	// Assert
	for _, v := range verrs {
		if v.KeyPath == "topics[0].source.host" || v.KeyPath == "topics[0].source.auth.username" || v.KeyPath == "topics[0].source.auth" {
			t.Errorf("inbox must not require remote host/auth, got %v", v)
		}
	}
}

// inboxConfig はバリデーションを通過する基準の inbox Config を生成するヘルパー。
func inboxConfig(mode, suffix string) *Config {
	return &Config{
		PollingInterval:  60,
		ArchiveRetention: 90,
		RetryMaxCount:    5,
		MetricsPort:      9090,
		DataDir:          "/var/lib/file-pubsub",
		Topics: []Topic{
			{
				Name: "invoices",
				Source: Source{
					Type:                 SourceTypeInbox,
					Directory:            "/inbox/invoices",
					OriginalFileHandling: HandlingDelete,
					Completion:           Completion{Mode: mode, Suffix: suffix},
					StabilityCheck:       StabilityCheck{Interval: 10},
				},
				Subscriptions: []Subscription{
					{Name: "current", Directory: "/pub/invoices/current"},
				},
			},
		},
	}
}

// assertKeyPath は verrs に指定キーパスの違反(原因・対処つき)が含まれることを検証する。
func assertKeyPath(t *testing.T, verrs ValidationErrors, keyPath string) {
	t.Helper()
	for _, v := range verrs {
		if v.KeyPath == keyPath {
			if v.Cause == "" || v.Remedy == "" {
				t.Errorf("error for %s must carry cause and remedy: %+v", keyPath, v)
			}
			return
		}
	}
	t.Errorf("missing validation error for %s, got %v", keyPath, verrs)
}
