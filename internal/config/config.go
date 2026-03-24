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
	// Global marks this config as the global defaults file. When set to true,
	// account and tokens are not required — the file only provides default
	// settings (daemon, vault, rotation, observability) for other configs.
	Global        bool                `yaml:"global"`
	Account       AccountConfig       `yaml:"account"`
	Daemon        DaemonConfig        `yaml:"daemon"`
	Rotation      RotationConfig      `yaml:"rotation"`
	Vault         VaultConfig         `yaml:"vault"`
	Observability ObservabilityConfig `yaml:"observability"`
	Tokens        []TokenConfig       `yaml:"tokens"`
}

// AccountConfig represents the Linode account used by this configuration file.
// For non-global configs, exactly one account block is required.
// Global configs (global: true) do not require an account block.
type AccountConfig struct {
	// Label identifies this account (e.g., "lcid-1234").
	// Also used as the key when reading/writing the token via storage.
	Label string `yaml:"label"`
	// Team that owns this account
	Team string `yaml:"team"`
	// APIURL overrides the Linode API endpoint (default: https://api.linode.com)
	APIURL string `yaml:"api_url"`
	// Vault contains the AppRole credentials for this account, plus optional
	// overrides for the global Vault address and mount path.
	Vault AccountVaultConfig `yaml:"vault"`
	// Token configures the account's Linode API token.
	// If omitted, the LINODE_TOKEN environment variable is used.
	// If storage is set, the token is read from storage at startup.
	// If rotation fields (label, validity, scopes) are also set, latr will
	// manage and rotate this token. The operator is responsible for
	// restarting latr to pick up the new token after rotation.
	Token *AccountTokenConfig `yaml:"token"`
}

// AccountTokenConfig configures the account's Linode API token source and
// optional rotation management.
//
// Storage determines where the token is read from (and written back to after
// rotation). If storage is not set, the LINODE_TOKEN env var is used.
//
// If Label, Validity, and Scopes are set, latr will manage this token's
// lifecycle — rotating it like any other managed token. When managed, storage
// is required so the rotated token can be written back. The key used within
// storage is derived from account.label.
type AccountTokenConfig struct {
	Storage           []StorageConfig `yaml:"storage"`
	Label             string          `yaml:"label"`
	Validity          string          `yaml:"validity"`
	Scopes            string          `yaml:"scopes"`
	RotationThreshold int             `yaml:"rotation_threshold"`
}

// IsManaged returns true if the token is configured for rotation management.
func (c *AccountTokenConfig) IsManaged() bool {
	return c.Label != "" && c.Validity != "" && c.Scopes != ""
}

// HasStorage returns true if the token should be read from a storage backend.
func (c *AccountTokenConfig) HasStorage() bool {
	return len(c.Storage) > 0
}

// AccountVaultConfig contains per-account Vault credentials and optional
// overrides for the global Vault address and mount path.
type AccountVaultConfig struct {
	RoleID    string `yaml:"role_id"`
	SecretID  string `yaml:"secret_id"`
	Address   string `yaml:"address"`    // Optional: overrides global vault.address
	MountPath string `yaml:"mount_path"` // Optional: overrides global vault.mount_path
}

// Resolved returns a fully resolved VaultConfig by overlaying account-level
// overrides on top of the global defaults.
func (c *AccountVaultConfig) Resolved(global VaultConfig) VaultConfig {
	resolved := global
	if c.Address != "" {
		resolved.Address = c.Address
	}
	if c.RoleID != "" {
		resolved.RoleID = c.RoleID
	}
	if c.SecretID != "" {
		resolved.SecretID = c.SecretID
	}
	if c.MountPath != "" {
		resolved.MountPath = c.MountPath
	}
	return resolved
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

// VaultConfig contains Vault connection and authentication settings.
// These serve as global defaults. Per-account overrides can be set in
// account.vault (AccountVaultConfig).
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
	Storage           []StorageConfig `yaml:"storage"`
}

// StorageConfig represents where to store the rotated token
type StorageConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
	// Key is the key name within the secret. Defaults to "token" if empty.
	Key string `yaml:"key,omitempty"`
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
	// Note: Account.APIURL is intentionally NOT defaulted here.
	// An empty APIURL allows main.go to fall back to LINODE_API_URL env var.
	// The linode client defaults to https://api.linode.com when both are empty.
}

// IsGlobal returns true if this config is marked as the global defaults file
// via `global: true`. Global configs provide default settings for other configs
// and do not require account or tokens.
func (c *Config) IsGlobal() bool {
	return c.Global
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Global {
		// Global config only provides defaults. It must not set account or
		// tokens, because cmd/latr skips global configs entirely and any such
		// settings would be silently ignored.
		emptyAccount := AccountConfig{}
		if c.Account != emptyAccount || len(c.Tokens) > 0 {
			return fmt.Errorf("global config must not set account or tokens; move these settings to a non-global config file")
		}
		return nil
	}

	// Validate account config
	if c.Account.Label == "" {
		return fmt.Errorf("account label is required")
	}

	// Validate Vault address is set somewhere (globally or per-account)
	resolvedVault := c.Account.Vault.Resolved(c.Vault)
	if resolvedVault.Address == "" {
		return fmt.Errorf("vault address is required (set globally in vault.address or per-account in account.vault.address)")
	}

	// Note: account.vault.role_id and secret_id are resolved at runtime —
	// they can come from the config file or VAULT_ROLE_ID / VAULT_SECRET_ID
	// environment variables.

	// Validate account token if configured
	if c.Account.Token != nil {
		if err := c.validateAccountToken(); err != nil {
			return err
		}
	}

	// Tokens list can be empty if managing the account token
	if len(c.Tokens) == 0 && (c.Account.Token == nil || !c.Account.Token.IsManaged()) {
		return fmt.Errorf("at least one token must be configured (in tokens list or as a managed account.token)")
	}

	for i, token := range c.Tokens {
		if err := validateToken(&token, fmt.Sprintf("token[%d]", i)); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) validateAccountToken() error {
	t := c.Account.Token

	// Validate storage entries if present
	if t.HasStorage() {
		for i, s := range t.Storage {
			if s.Type == "" {
				return fmt.Errorf("account.token.storage[%d]: type is required", i)
			}
			if s.Path == "" {
				return fmt.Errorf("account.token.storage[%d]: path is required", i)
			}
		}
	}

	// If managed (has rotation fields), validate them
	if t.IsManaged() {
		if !t.HasStorage() {
			return fmt.Errorf("account.token: managed token (with label/validity/scopes) requires storage so the rotated token can be written back")
		}

		duration, err := ParseValidityDuration(t.Validity)
		if err != nil {
			return fmt.Errorf("account.token: invalid validity period: %w", err)
		}
		maxValidity := 180 * 24 * time.Hour
		if duration > maxValidity {
			return fmt.Errorf("account.token: validity period must be <= 6 months (180d), got %s", t.Validity)
		}
	}

	return nil
}

func validateToken(token *TokenConfig, prefix string) error {
	if token.Label == "" {
		return fmt.Errorf("%s: token label is required", prefix)
	}
	if token.Validity == "" {
		return fmt.Errorf("%s: token validity is required", prefix)
	}
	if token.Scopes == "" {
		return fmt.Errorf("%s: token scopes is required", prefix)
	}
	if len(token.Storage) == 0 {
		return fmt.Errorf("%s: at least one storage backend is required", prefix)
	}
	for i, s := range token.Storage {
		if s.Type == "" {
			return fmt.Errorf("%s: storage[%d]: type is required", prefix, i)
		}
		if s.Path == "" {
			return fmt.Errorf("%s: storage[%d]: path is required", prefix, i)
		}
	}

	// Validate validity period
	duration, err := ParseValidityDuration(token.Validity)
	if err != nil {
		return fmt.Errorf("%s: invalid validity period: %w", prefix, err)
	}

	// Check that validity is <= 6 months (180 days)
	maxValidity := 180 * 24 * time.Hour
	if duration > maxValidity {
		return fmt.Errorf("%s: validity period must be <= 6 months (180d), got %s", prefix, token.Validity)
	}

	return nil
}

// AllTokens returns all tokens for this config, including the account's own
// token (if managed) prepended to the list. The account token's storage key
// is set to account.label so it reads/writes to the same location.
func (c *Config) AllTokens() []TokenConfig {
	var tokens []TokenConfig
	if c.Account.Token != nil && c.Account.Token.IsManaged() {
		// Derive storage with account.label as the key
		var storage []StorageConfig
		for _, s := range c.Account.Token.Storage {
			storage = append(storage, StorageConfig{
				Type: s.Type,
				Path: s.Path,
				Key:  c.Account.Label,
			})
		}
		tokens = append(tokens, TokenConfig{
			Label:             c.Account.Token.Label,
			Team:              c.Account.Team,
			Validity:          c.Account.Token.Validity,
			Scopes:            c.Account.Token.Scopes,
			RotationThreshold: c.Account.Token.RotationThreshold,
			Storage:           storage,
		})
	}
	tokens = append(tokens, c.Tokens...)
	return tokens
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
