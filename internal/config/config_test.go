package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validYAML = `
polling_interval: 60
archive_retention: 90
retry_max_count: 5
metrics_port: 9090
topics:
  - name: orders
    description: "order files"
    source:
      type: sftp
      host: legacy-host01
      directory: /out/orders
      original_file_handling: delete
      stability_check:
        interval: 10
      exclude_patterns:
        - "*.tmp"
      auth:
        username: ${TEST_SFTP_USER}
        password: ${TEST_SFTP_PASSWORD}
    subscriptions:
      - name: current
        directory: /pub/orders/current
      - name: next
        directory: /pub/orders/next
  - name: customers
    source:
      type: local
      directory: /out/customers
      original_file_handling: copy
      stability_check:
        interval: 10
    subscriptions:
      - name: current
        directory: /pub/customers/current
`

// writeConfig はテスト用の一時ディレクトリに config.yaml を書き出すヘルパー。
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_正しい設定を読み込んだ場合_全項目が展開されて返ること(t *testing.T) {
	// Arrange
	t.Setenv("TEST_SFTP_USER", "legacy_user")
	t.Setenv("TEST_SFTP_PASSWORD", "s3cret")
	path := writeConfig(t, validYAML)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollingInterval != 60 || cfg.ArchiveRetention != 90 || cfg.RetryMaxCount != 5 || cfg.MetricsPort != 9090 {
		t.Errorf("unexpected top-level values: %+v", cfg)
	}
	if cfg.DataDir != filepath.Dir(path) {
		t.Errorf("DataDir = %q, want config directory %q", cfg.DataDir, filepath.Dir(path))
	}
	orders := cfg.Topics[0]
	if orders.Source.Auth.Username != "legacy_user" || orders.Source.Auth.Password != "s3cret" {
		t.Errorf("env refs not expanded: %+v", orders.Source.Auth)
	}
	if orders.Source.StabilityCheck.Interval != 10 {
		t.Errorf("stability interval = %d", orders.Source.StabilityCheck.Interval)
	}
	if len(orders.Subscriptions) != 2 || orders.Subscriptions[1].Directory != "/pub/orders/next" {
		t.Errorf("unexpected subscriptions: %+v", orders.Subscriptions)
	}
	if cfg.Topics[1].Source.OriginalFileHandling != HandlingCopy {
		t.Errorf("copy handling not kept: %q", cfg.Topics[1].Source.OriginalFileHandling)
	}
}

func TestLoad_元ファイルの扱いを省略した場合_deleteがデフォルトになること(t *testing.T) {
	// Arrange
	yaml := `
polling_interval: 60
archive_retention: 90
retry_max_count: 5
metrics_port: 9090
topics:
  - name: orders
    source:
      type: local
      directory: /out/orders
      stability_check:
        interval: 10
    subscriptions:
      - name: current
        directory: /pub/orders/current
`
	path := writeConfig(t, yaml)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Topics[0].Source.OriginalFileHandling != HandlingDelete {
		t.Errorf("default handling = %q, want delete", cfg.Topics[0].Source.OriginalFileHandling)
	}
}

func TestLoad_未定義の環境変数を参照した場合_変数名つきのバリデーションエラーになること(t *testing.T) {
	// Arrange
	os.Unsetenv("TEST_UNDEFINED_VAR_XYZ")
	yaml := strings.ReplaceAll(validYAML, "${TEST_SFTP_USER}", "${TEST_UNDEFINED_VAR_XYZ}")
	t.Setenv("TEST_SFTP_PASSWORD", "s3cret")
	path := writeConfig(t, yaml)

	// Act
	_, err := Load(path)

	// Assert
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("want ValidationErrors, got %v", err)
	}
	if !strings.Contains(verrs.Error(), "TEST_UNDEFINED_VAR_XYZ") {
		t.Errorf("error must name the undefined variable: %v", verrs)
	}
}

func TestLoad_違反が複数ある場合_全エラーがまとめて返ること(t *testing.T) {
	// Arrange
	yaml := `
polling_interval: 0
archive_retention: 0
retry_max_count: 0
metrics_port: 0
topics:
  - name: ""
    source:
      type: bogus
    subscriptions: []
`
	path := writeConfig(t, yaml)

	// Act
	_, err := Load(path)

	// Assert
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("want ValidationErrors, got %v", err)
	}
	for _, keyPath := range []string{"polling_interval", "archive_retention", "retry_max_count", "metrics_port", "topics[0].name", "topics[0].source.type", "topics[0].subscriptions"} {
		found := false
		for _, v := range verrs {
			if v.KeyPath == keyPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing validation error for %s in %v", keyPath, verrs)
		}
	}
}

func TestValidate_違反のある設定の場合_該当キーパスのエラーが原因と対処つきで返ること(t *testing.T) {
	// Arrange
	tests := []struct {
		name    string
		mutate  func(c *Config)
		keyPath string
	}{
		{"topic名が重複する場合_エラーになること", func(c *Config) { c.Topics[1].Name = "orders" }, "topics[1].name"},
		{"topic名にパス区切りを含む場合_エラーになること", func(c *Config) { c.Topics[0].Name = "a/b" }, "topics[0].name"},
		{"topic名がドット2つの場合_エラーになること", func(c *Config) { c.Topics[0].Name = ".." }, "topics[0].name"},
		{"topic名がドット1つの場合_エラーになること", func(c *Config) { c.Topics[0].Name = "." }, "topics[0].name"},
		{"topic名にバックスラッシュを含む場合_エラーになること", func(c *Config) { c.Topics[0].Name = `a\b` }, "topics[0].name"},
		{"subscription名にパス区切りを含む場合_エラーになること", func(c *Config) { c.Topics[0].Subscriptions[0].Name = "../escape" }, "topics[0].subscriptions[0].name"},
		{"subscription名がドット2つの場合_エラーになること", func(c *Config) { c.Topics[0].Subscriptions[0].Name = ".." }, "topics[0].subscriptions[0].name"},
		{"subscription名が重複する場合_エラーになること", func(c *Config) { c.Topics[0].Subscriptions[1].Name = "current" }, "topics[0].subscriptions[1].name"},
		{"subscriptionのdirectoryが無い場合_エラーになること", func(c *Config) { c.Topics[0].Subscriptions[0].Directory = "" }, "topics[0].subscriptions[0].directory"},
		{"sourceのdirectoryが無い場合_エラーになること", func(c *Config) { c.Topics[0].Source.Directory = "" }, "topics[0].source.directory"},
		{"リモートでhostが無い場合_エラーになること", func(c *Config) { c.Topics[0].Source.Host = "" }, "topics[0].source.host"},
		{"リモートでusernameが無い場合_エラーになること", func(c *Config) { c.Topics[0].Source.Auth.Username = "" }, "topics[0].source.auth.username"},
		{"リモートでpasswordとkey_fileの両方が無い場合_エラーになること", func(c *Config) { c.Topics[0].Source.Auth.Password = "" }, "topics[0].source.auth"},
		{"元ファイルの扱いが不正な場合_エラーになること", func(c *Config) { c.Topics[0].Source.OriginalFileHandling = "move" }, "topics[0].source.original_file_handling"},
		{"安定判定intervalが不正な場合_エラーになること", func(c *Config) { c.Topics[0].Source.StabilityCheck.Interval = 0 }, "topics[0].source.stability_check.interval"},
		{"除外パターンが不正な場合_エラーになること", func(c *Config) { c.Topics[0].Source.ExcludePatterns = []string{"[bad"} }, "topics[0].source.exclude_patterns[0]"},
		{"ポートが範囲外の場合_エラーになること", func(c *Config) { c.Topics[0].Source.Port = 70000 }, "topics[0].source.port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cfg := baseConfig()
			tt.mutate(cfg)

			// Act
			verrs := Validate(cfg)

			// Assert
			found := false
			for _, v := range verrs {
				if v.KeyPath == tt.keyPath {
					if v.Cause == "" || v.Remedy == "" {
						t.Errorf("error for %s must carry cause and remedy: %+v", tt.keyPath, v)
					}
					found = true
				}
			}
			if !found {
				t.Errorf("missing validation error for %s, got %v", tt.keyPath, verrs)
			}
		})
	}
}

func TestValidate_正しい設定の場合_違反が無いこと(t *testing.T) {
	// Arrange
	cfg := baseConfig()

	// Act
	verrs := Validate(cfg)

	// Assert
	if len(verrs) != 0 {
		t.Errorf("valid config must pass, got %v", verrs)
	}
}

func TestValidate_安全な名前文字だけを使った場合_違反が無いこと(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.Topics[0].Name = "orders.v2_x-1"
	cfg.Topics[0].Subscriptions[0].Name = "current-01.test_a"

	// Act
	verrs := Validate(cfg)

	// Assert
	if len(verrs) != 0 {
		t.Errorf("letters, digits, dot, underscore and hyphen must be accepted, got %v", verrs)
	}
}

func TestExpandEnv_参照とリテラルが混在する場合_参照だけ展開されリテラルは保持されること(t *testing.T) {
	// Arrange
	t.Setenv("TEST_EXPAND_X", "value")

	// Act
	out, errs := ExpandEnv([]byte("a: ${TEST_EXPAND_X}\nb: plain\n"))

	// Assert
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if string(out) != "a: value\nb: plain\n" {
		t.Errorf("unexpected expansion: %q", out)
	}
}

func TestApplyDefaults_highAvailabilityを省略した場合_HighAvailabilityがnilで単一インスタンスになること(t *testing.T) {
	// Arrange
	yaml := `
polling_interval: 60
archive_retention: 90
retry_max_count: 5
metrics_port: 9090
topics:
  - name: orders
    source:
      type: local
      directory: /out/orders
      stability_check:
        interval: 10
    subscriptions:
      - name: current
        directory: /pub/orders/current
`
	path := writeConfig(t, yaml)

	// Act
	cfg, err := Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HighAvailability != nil {
		t.Errorf("HighAvailability = %+v, want nil (single instance)", cfg.HighAvailability)
	}
}

func TestApplyDefaults_uniquenessMethodを省略した場合_leaseが補完されること(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.HighAvailability = &HighAvailability{}

	// Act
	cfg.applyDefaults("/tmp/config.yaml")

	// Assert
	if cfg.HighAvailability.UniquenessMethod != UniquenessMethodLease {
		t.Errorf("UniquenessMethod = %q, want %q", cfg.HighAvailability.UniquenessMethod, UniquenessMethodLease)
	}
}

func TestApplyDefaults_leaseTTLを省略した場合_90が補完されること(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.HighAvailability = &HighAvailability{}

	// Act
	cfg.applyDefaults("/tmp/config.yaml")

	// Assert
	if cfg.HighAvailability.LeaseTTL != DefaultLeaseTTL {
		t.Errorf("LeaseTTL = %d, want %d", cfg.HighAvailability.LeaseTTL, DefaultLeaseTTL)
	}
}

func TestApplyDefaults_heartbeatIntervalを省略した場合_leaseTTLの3分の1が補完されること(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.HighAvailability = &HighAvailability{}

	// Act
	cfg.applyDefaults("/tmp/config.yaml")

	// Assert
	want := cfg.HighAvailability.LeaseTTL / 3
	if cfg.HighAvailability.HeartbeatInterval != want {
		t.Errorf("HeartbeatInterval = %d, want %d (lease_ttl/3)", cfg.HighAvailability.HeartbeatInterval, want)
	}
}

func TestValidate_uniquenessMethodが不正値の場合_ValidationErrorになること(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.HighAvailability = &HighAvailability{UniquenessMethod: "bogus", LeaseTTL: 90, HeartbeatInterval: 30}

	// Act
	verrs := Validate(cfg)

	// Assert
	if !hasKeyPath(verrs, "high_availability.uniqueness_method") {
		t.Errorf("missing validation error for high_availability.uniqueness_method, got %v", verrs)
	}
}

func TestValidate_heartbeatIntervalがleaseTTL以上の場合_ValidationErrorになること(t *testing.T) {
	// Arrange
	cfg := baseConfig()
	cfg.HighAvailability = &HighAvailability{UniquenessMethod: UniquenessMethodLease, LeaseTTL: 90, HeartbeatInterval: 90}

	// Act
	verrs := Validate(cfg)

	// Assert
	if !hasKeyPath(verrs, "high_availability.heartbeat_interval") {
		t.Errorf("missing validation error for high_availability.heartbeat_interval, got %v", verrs)
	}
}

// hasKeyPath は verrs に指定キーパスの違反が含まれるかを返すヘルパー。
func hasKeyPath(verrs ValidationErrors, keyPath string) bool {
	for _, v := range verrs {
		if v.KeyPath == keyPath {
			return true
		}
	}
	return false
}

// baseConfig はバリデーションを通過する基準 Config を生成するヘルパー。
func baseConfig() *Config {
	return &Config{
		PollingInterval:  60,
		ArchiveRetention: 90,
		RetryMaxCount:    5,
		MetricsPort:      9090,
		DataDir:          "/var/lib/file-pubsub",
		Topics: []Topic{
			{
				Name: "orders",
				Source: Source{
					Type:                 SourceTypeSFTP,
					Host:                 "legacy-host01",
					Directory:            "/out/orders",
					OriginalFileHandling: HandlingDelete,
					StabilityCheck:       StabilityCheck{Interval: 10},
					Auth:                 Auth{Username: "u", Password: "p"},
				},
				Subscriptions: []Subscription{
					{Name: "current", Directory: "/pub/orders/current"},
					{Name: "next", Directory: "/pub/orders/next"},
				},
			},
			{
				Name: "customers",
				Source: Source{
					Type:                 SourceTypeLocal,
					Directory:            "/out/customers",
					OriginalFileHandling: HandlingCopy,
					StabilityCheck:       StabilityCheck{Interval: 10},
				},
				Subscriptions: []Subscription{
					{Name: "current", Directory: "/pub/customers/current"},
				},
			},
		},
	}
}
