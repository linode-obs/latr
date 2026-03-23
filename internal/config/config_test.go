package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValidConfig(t *testing.T) {
	yamlContent := `
daemon:
  mode: "daemon"
  check_interval: "30m"
  dry_run: false

rotation:
  threshold_percent: 10
  prune_expired: false

vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"
  mount_path: "secret"

observability:
  otel_endpoint: "localhost:4317"
  log_level: "info"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check daemon settings
	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	assert.Equal(t, "30m", cfg.Daemon.CheckInterval)
	assert.False(t, cfg.Daemon.DryRun)

	// Check rotation settings
	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)

	// Check vault settings
	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	assert.Equal(t, "test-role-id", cfg.Vault.RoleID)
	assert.Equal(t, "test-secret-id", cfg.Vault.SecretID)
	assert.Equal(t, "secret", cfg.Vault.MountPath)

	// Check observability settings
	assert.Equal(t, "localhost:4317", cfg.Observability.OTelEndpoint)
	assert.Equal(t, "info", cfg.Observability.LogLevel)

	// Check tokens
	require.Len(t, cfg.Tokens, 1)
	token := cfg.Tokens[0]
	assert.Equal(t, "test-token", token.Label)
	assert.Equal(t, "platform-team", token.Team)
	assert.Equal(t, "90d", token.Validity)
	assert.Equal(t, "*", token.Scopes)
	require.Len(t, token.Storage, 1)
	assert.Equal(t, "vault", token.Storage[0].Type)
	assert.Equal(t, "secret/data/linode/tokens/test", token.Storage[0].Path)
}

func TestParseConfigWithDefaults(t *testing.T) {
	yamlContent := `
vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Apply defaults
	cfg.ApplyDefaults()

	// Check defaults were applied
	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	assert.Equal(t, "30m", cfg.Daemon.CheckInterval)
	assert.False(t, cfg.Daemon.DryRun)
	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)
	assert.Equal(t, "secret", cfg.Vault.MountPath)
	assert.Equal(t, "info", cfg.Observability.LogLevel)
}

func TestValidateConfig_ValidityPeriodTooLong(t *testing.T) {
	cfg := &Config{
		Vault: VaultConfig{
			Address:   "https://vault.example.com",
			RoleID:    "test-role-id",
			SecretID:  "test-secret-id",
			MountPath: "secret",
		},
		Tokens: []TokenConfig{
			{
				Label:    "test-token",
				Team:     "platform-team",
				Validity: "7mo", // More than 6 months
				Scopes:   "*",
				Storage: []StorageConfig{
					{Type: "vault", Path: "secret/data/linode/tokens/test"},
				},
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validity period must be <= 6 months")
}

func TestValidateConfig_ValidityPeriodExactly6Months(t *testing.T) {
	cfg := &Config{
		Vault: VaultConfig{
			Address:   "https://vault.example.com",
			RoleID:    "test-role-id",
			SecretID:  "test-secret-id",
			MountPath: "secret",
		},
		Tokens: []TokenConfig{
			{
				Label:    "test-token",
				Team:     "platform-team",
				Validity: "180d", // Exactly 6 months
				Scopes:   "*",
				Storage: []StorageConfig{
					{Type: "vault", Path: "secret/data/linode/tokens/test"},
				},
			},
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
}

func TestValidateConfig_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		errMsg string
	}{
		{
			name: "missing vault address",
			config: &Config{
				Vault: VaultConfig{
					RoleID:    "test-role-id",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "vault address is required",
		},
		{
			name: "missing vault role_id",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "vault role_id is required",
		},
		{
			name: "missing vault secret_id",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					RoleID:    "test-role-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "vault secret_id is required",
		},
		{
			name: "missing token label",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					RoleID:    "test-role-id",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Team: "team", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "token label is required",
		},
		{
			name: "missing token validity",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					RoleID:    "test-role-id",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "token validity is required",
		},
		{
			name: "missing token scopes",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					RoleID:    "test-role-id",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Validity: "90d", Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
				},
			},
			errMsg: "token scopes is required",
		},
		{
			name: "no storage configured",
			config: &Config{
				Vault: VaultConfig{
					Address:   "https://vault.example.com",
					RoleID:    "test-role-id",
					SecretID:  "test-secret-id",
					MountPath: "secret",
				},
				Tokens: []TokenConfig{
					{Label: "test", Team: "team", Validity: "90d", Scopes: "*", Storage: []StorageConfig{}},
				},
			},
			errMsg: "at least one storage backend is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestParseValidityDuration(t *testing.T) {
	tests := []struct {
		validity string
		expected time.Duration
		hasError bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"180d", 180 * 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1h", 1 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"6mo", 180 * 24 * time.Hour, false},
		{"3mo", 90 * 24 * time.Hour, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.validity, func(t *testing.T) {
			duration, err := ParseValidityDuration(tt.validity)
			if tt.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, duration)
			}
		})
	}
}

func TestTokenConfigOverrideThreshold(t *testing.T) {
	yamlContent := `
vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"

rotation:
  threshold_percent: 10

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    rotation_threshold: 15
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)
	assert.Equal(t, 15, cfg.Tokens[0].RotationThreshold)
}

func TestParseConfigWithEnvVarSubstitution(t *testing.T) {
	// Set environment variables for testing
	os.Setenv("TEST_VAULT_ADDRESS", "https://vault-from-env.example.com")
	os.Setenv("TEST_VAULT_ROLE_ID", "role-id-from-env")
	os.Setenv("TEST_VAULT_SECRET_ID", "secret-id-from-env")
	os.Setenv("TEST_OTEL_ENDPOINT", "otel-from-env:4317")
	defer func() {
		os.Unsetenv("TEST_VAULT_ADDRESS")
		os.Unsetenv("TEST_VAULT_ROLE_ID")
		os.Unsetenv("TEST_VAULT_SECRET_ID")
		os.Unsetenv("TEST_OTEL_ENDPOINT")
	}()

	yamlContent := `
vault:
  address: "${TEST_VAULT_ADDRESS}"
  role_id: "${TEST_VAULT_ROLE_ID}"
  secret_id: "${TEST_VAULT_SECRET_ID}"
  mount_path: "secret"

observability:
  otel_endpoint: "${TEST_OTEL_ENDPOINT}"
  log_level: "info"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify environment variables were substituted
	assert.Equal(t, "https://vault-from-env.example.com", cfg.Vault.Address)
	assert.Equal(t, "role-id-from-env", cfg.Vault.RoleID)
	assert.Equal(t, "secret-id-from-env", cfg.Vault.SecretID)
	assert.Equal(t, "otel-from-env:4317", cfg.Observability.OTelEndpoint)
}

func TestValidateConfig_MultipleSelfTokens(t *testing.T) {
	cfg := &Config{
		Vault: VaultConfig{
			Address:   "https://vault.example.com",
			RoleID:    "test-role-id",
			SecretID:  "test-secret-id",
			MountPath: "secret",
		},
		Tokens: []TokenConfig{
			{
				Label:    "self-token-1",
				Team:     "team",
				Validity: "90d",
				Scopes:   "*",
				Self:     true,
				Storage:  []StorageConfig{{Type: "vault", Path: "path1"}},
			},
			{
				Label:    "self-token-2",
				Team:     "team",
				Validity: "90d",
				Scopes:   "*",
				Self:     true,
				Storage:  []StorageConfig{{Type: "vault", Path: "path2"}},
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one token may have self: true")
}

func TestValidateConfig_SingleSelfToken(t *testing.T) {
	cfg := &Config{
		Vault: VaultConfig{
			Address:   "https://vault.example.com",
			RoleID:    "test-role-id",
			SecretID:  "test-secret-id",
			MountPath: "secret",
		},
		Tokens: []TokenConfig{
			{
				Label:    "self-token",
				Team:     "team",
				Validity: "90d",
				Scopes:   "*",
				Self:     true,
				Storage:  []StorageConfig{{Type: "vault", Path: "path1"}},
			},
			{
				Label:    "other-token",
				Team:     "team",
				Validity: "90d",
				Scopes:   "*",
				Self:     false,
				Storage:  []StorageConfig{{Type: "vault", Path: "path2"}},
			},
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
}

func TestParseConfigWithSelfField(t *testing.T) {
	yamlContent := `
vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"

tokens:
  - label: "self-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    self: true
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/self"
  - label: "managed-token"
    team: "platform-team"
    validity: "180d"
    scopes: "linodes:read_write"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/managed"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Tokens, 2)

	assert.True(t, cfg.Tokens[0].Self)
	assert.False(t, cfg.Tokens[1].Self)
}

func TestParseConfigWithMissingEnvVar(t *testing.T) {
	yamlContent := `
vault:
  address: "${MISSING_VAULT_ADDRESS}"
  role_id: "test-role-id"
  secret_id: "test-secret-id"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// When env var is missing, viper leaves the literal string as-is
	// This is expected behavior - validation will catch empty required fields
	assert.Equal(t, "", cfg.Vault.Address)
}
