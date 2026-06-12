// Package config loads and validates the single YAML configuration file
// (CTP-003) with ${ENV_VAR} expansion for credentials and other string values
// (CTP-002). Validation failures map to exit code 2 (ui-design.md).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Source types (ui-design.md: ftp / sftp / scp / local).
const (
	SourceTypeLocal = "local"
	SourceTypeFTP   = "ftp"
	SourceTypeSFTP  = "sftp"
	SourceTypeSCP   = "scp"
)

// Original file handling (SP-004): delete (collect & remove, default) / copy (leave).
const (
	HandlingDelete = "delete"
	HandlingCopy   = "copy"
)

// Config is the whole single-YAML configuration (E-001).
type Config struct {
	PollingInterval  int     `yaml:"polling_interval"`  // seconds
	ArchiveRetention int     `yaml:"archive_retention"` // days (SP-006)
	RetryMaxCount    int     `yaml:"retry_max_count"`   // SR-004
	MetricsPort      int     `yaml:"metrics_port"`
	DataDir          string  `yaml:"data_dir"` // defaults to the config.yaml directory
	Topics           []Topic `yaml:"topics"`
}

// Topic defines one logical file kind and its source / subscriptions (E-002).
type Topic struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Source        Source         `yaml:"source"`
	Subscriptions []Subscription `yaml:"subscriptions"`
}

// Source defines where files are collected from (E-004).
type Source struct {
	Type                 string         `yaml:"type"`
	Host                 string         `yaml:"host"` // not used for local
	Port                 int            `yaml:"port"` // optional, protocol default when 0
	Directory            string         `yaml:"directory"`
	OriginalFileHandling string         `yaml:"original_file_handling"` // delete (default) / copy
	StabilityCheck       StabilityCheck `yaml:"stability_check"`
	ExcludePatterns      []string       `yaml:"exclude_patterns"`
	Auth                 Auth           `yaml:"auth"`
}

// StabilityCheck configures the write-completion check (SP-003).
type StabilityCheck struct {
	Interval int `yaml:"interval"` // seconds between size/mtime stability observations
}

// Auth holds credentials for remote sources (E-005, CTP-002). Plain text is
// allowed; ${ENV_VAR} references and key files are the recommended forms.
type Auth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	KeyFile  string `yaml:"key_file"`
}

// Subscription defines one delivery target directory (E-003).
type Subscription struct {
	Name      string `yaml:"name"`
	Directory string `yaml:"directory"`
}

var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces every ${ENV_VAR} reference in raw with the environment
// value. References to undefined variables are collected as validation errors.
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

// Load reads the YAML at path, expands ${ENV_VAR} references, applies
// defaults, and validates. All validation errors are returned together as a
// ValidationErrors so the caller can map them to exit code 2.
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

// applyDefaults fills values the spec defines as defaulted: data_dir is the
// config.yaml directory (object-storage-schema.yaml) and the original file
// handling defaults to delete (SP-004).
func (c *Config) applyDefaults(configPath string) {
	if c.DataDir == "" {
		c.DataDir = filepath.Dir(configPath)
	}
	for i := range c.Topics {
		if c.Topics[i].Source.OriginalFileHandling == "" {
			c.Topics[i].Source.OriginalFileHandling = HandlingDelete
		}
	}
}
