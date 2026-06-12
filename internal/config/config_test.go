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

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Valid(t *testing.T) {
	t.Setenv("TEST_SFTP_USER", "legacy_user")
	t.Setenv("TEST_SFTP_PASSWORD", "s3cret")
	path := writeConfig(t, validYAML)

	cfg, err := Load(path)
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

func TestLoad_DefaultHandlingIsDelete(t *testing.T) {
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
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Topics[0].Source.OriginalFileHandling != HandlingDelete {
		t.Errorf("default handling = %q, want delete", cfg.Topics[0].Source.OriginalFileHandling)
	}
}

func TestLoad_UndefinedEnvVar(t *testing.T) {
	os.Unsetenv("TEST_UNDEFINED_VAR_XYZ")
	yaml := strings.ReplaceAll(validYAML, "${TEST_SFTP_USER}", "${TEST_UNDEFINED_VAR_XYZ}")
	t.Setenv("TEST_SFTP_PASSWORD", "s3cret")

	_, err := Load(writeConfig(t, yaml))
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("want ValidationErrors, got %v", err)
	}
	if !strings.Contains(verrs.Error(), "TEST_UNDEFINED_VAR_XYZ") {
		t.Errorf("error must name the undefined variable: %v", verrs)
	}
}

func TestLoad_AllErrorsReturnedTogether(t *testing.T) {
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
	_, err := Load(writeConfig(t, yaml))
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

func TestValidate_Violations(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		keyPath string
	}{
		{"duplicate topic name", func(c *Config) { c.Topics[1].Name = "orders" }, "topics[1].name"},
		{"duplicate subscription name", func(c *Config) { c.Topics[0].Subscriptions[1].Name = "current" }, "topics[0].subscriptions[1].name"},
		{"missing subscription directory", func(c *Config) { c.Topics[0].Subscriptions[0].Directory = "" }, "topics[0].subscriptions[0].directory"},
		{"missing source directory", func(c *Config) { c.Topics[0].Source.Directory = "" }, "topics[0].source.directory"},
		{"missing host for remote", func(c *Config) { c.Topics[0].Source.Host = "" }, "topics[0].source.host"},
		{"missing username for remote", func(c *Config) { c.Topics[0].Source.Auth.Username = "" }, "topics[0].source.auth.username"},
		{"missing password and key_file for remote", func(c *Config) { c.Topics[0].Source.Auth.Password = "" }, "topics[0].source.auth"},
		{"invalid handling", func(c *Config) { c.Topics[0].Source.OriginalFileHandling = "move" }, "topics[0].source.original_file_handling"},
		{"invalid stability interval", func(c *Config) { c.Topics[0].Source.StabilityCheck.Interval = 0 }, "topics[0].source.stability_check.interval"},
		{"invalid exclude pattern", func(c *Config) { c.Topics[0].Source.ExcludePatterns = []string{"[bad"} }, "topics[0].source.exclude_patterns[0]"},
		{"port out of range", func(c *Config) { c.Topics[0].Source.Port = 70000 }, "topics[0].source.port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.mutate(cfg)
			verrs := Validate(cfg)
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

func TestValidate_OK(t *testing.T) {
	if verrs := Validate(baseConfig()); len(verrs) != 0 {
		t.Errorf("valid config must pass, got %v", verrs)
	}
}

func TestExpandEnv_KeepsLiteralText(t *testing.T) {
	t.Setenv("TEST_EXPAND_X", "value")
	out, errs := ExpandEnv([]byte("a: ${TEST_EXPAND_X}\nb: plain\n"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if string(out) != "a: value\nb: plain\n" {
		t.Errorf("unexpected expansion: %q", out)
	}
}

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
