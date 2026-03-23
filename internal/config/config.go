package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete configuration for the token rotator
type Config struct {
	Daemon        DaemonConfig        `yaml:"daemon"`
	Rotation      RotationConfig      `yaml:"rotation"`
	Vault         VaultConfig         `yaml:"vault"`
	Observability ObservabilityConfig `yaml:"observability"`
	Tokens        []TokenConfig       `yaml:"tokens"`
}

// DaemonConfig contains settings for daemon behavior
type DaemonConfig struct {
	Mode          string `yaml:"mode"`
	CheckInterval string `yaml:"check_interval"`
	DryRun        bool   `yaml:"dry_run"`
}

// RotationConfig contains settings for token rotation
type RotationConfig struct {
	ThresholdPercent int `yaml:"threshold_percent"`
}

// VaultConfig contains Vault connection and authentication settings
type VaultConfig struct {
	Address   string `yaml:"address"`
	RoleID    string `yaml:"role_id"`
	SecretID  string `yaml:"secret_id"`
	MountPath string `yaml:"mount_path"`
}

// ObservabilityConfig contains settings for telemetry and logging
type ObservabilityConfig struct {
	OTelEndpoint string `yaml:"otel_endpoint"`
	LogLevel     string `yaml:"log_level"`
}

// TokenConfig represents a single token to manage
type TokenConfig struct {
	Label             string          `yaml:"label"`
	Team              string          `yaml:"team"`
	Validity          string          `yaml:"validity"`
	Scopes            string          `yaml:"scopes"`
	RotationThreshold int             `yaml:"rotation_threshold"`
	Self              bool            `yaml:"self"`
	Storage           []StorageConfig `yaml:"storage"`
}

// StorageConfig represents where to store the rotated token
type StorageConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

// Parse parses YAML configuration data into a Config struct.
//
// Environment variables are automatically expanded before parsing the YAML.
// Supported formats:
//   - ${VAR_NAME} - expands to the value of VAR_NAME
//   - $VAR_NAME   - expands to the value of VAR_NAME
//
// If an environment variable is not set, it expands to an empty string.
// This is useful for keeping secrets out of config files:
//
//	vault:
//	  address: "https://vault.example.com"
//	  role_id: "${VAULT_ROLE_ID}"      # Expanded from environment
//	  secret_id: "${VAULT_SECRET_ID}"  # Expanded from environment
func Parse(data []byte) (*Config, error) {
	// Expand environment variables in the YAML content before parsing
	// This uses os.Expand which replaces ${VAR} and $VAR with their values
	expandedData := []byte(os.Expand(string(data), os.Getenv))

	var cfg Config
	if err := yaml.Unmarshal(expandedData, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// ApplyDefaults sets default values for optional configuration fields
func (c *Config) ApplyDefaults() {
	if c.Daemon.Mode == "" {
		c.Daemon.Mode = "daemon"
	}
	if c.Daemon.CheckInterval == "" {
		c.Daemon.CheckInterval = "30m"
	}
	if c.Rotation.ThresholdPercent == 0 {
		c.Rotation.ThresholdPercent = 10
	}
	if c.Vault.MountPath == "" {
		c.Vault.MountPath = "secret"
	}
	if c.Observability.LogLevel == "" {
		c.Observability.LogLevel = "info"
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Validate Vault config
	if c.Vault.Address == "" {
		return fmt.Errorf("vault address is required")
	}
	if c.Vault.RoleID == "" {
		return fmt.Errorf("vault role_id is required")
	}
	if c.Vault.SecretID == "" {
		return fmt.Errorf("vault secret_id is required")
	}

	// Validate tokens
	if len(c.Tokens) == 0 {
		return fmt.Errorf("at least one token must be configured")
	}

	selfCount := 0
	for i, token := range c.Tokens {
		if err := c.validateToken(&token, i); err != nil {
			return err
		}
		if token.Self {
			selfCount++
		}
	}

	if selfCount > 1 {
		return fmt.Errorf("only one token may have self: true, found %d", selfCount)
	}

	return nil
}

func (c *Config) validateToken(token *TokenConfig, index int) error {
	if token.Label == "" {
		return fmt.Errorf("token[%d]: token label is required", index)
	}
	if token.Validity == "" {
		return fmt.Errorf("token[%d]: token validity is required", index)
	}
	if token.Scopes == "" {
		return fmt.Errorf("token[%d]: token scopes is required", index)
	}
	if len(token.Storage) == 0 {
		return fmt.Errorf("token[%d]: at least one storage backend is required", index)
	}

	// Validate validity period
	duration, err := ParseValidityDuration(token.Validity)
	if err != nil {
		return fmt.Errorf("token[%d]: invalid validity period: %w", index, err)
	}

	// Check that validity is <= 6 months (180 days)
	maxValidity := 180 * 24 * time.Hour
	if duration > maxValidity {
		return fmt.Errorf("token[%d]: validity period must be <= 6 months (180d), got %s", index, token.Validity)
	}

	return nil
}

// ParseValidityDuration parses a validity string (e.g., "90d", "6mo") into a time.Duration
func ParseValidityDuration(validity string) (time.Duration, error) {
	// Support formats: 90d, 6mo, 1h, 30m
	re := regexp.MustCompile(`^(\d+)(mo|d|h|m)$`)
	matches := re.FindStringSubmatch(validity)
	if matches == nil {
		return 0, fmt.Errorf("invalid validity format: %s (expected format: <number><unit>, e.g., 90d, 6mo)", validity)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value in validity: %s", validity)
	}

	unit := matches[2]
	switch unit {
	case "mo":
		// Treat 1 month as 30 days
		return time.Duration(value) * 30 * 24 * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	default:
		return 0, fmt.Errorf("unsupported time unit: %s", unit)
	}
}
