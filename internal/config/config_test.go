package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to create a minimal valid account config for tests
func testAccount() AccountConfig {
	return AccountConfig{
		Label: "production",
		Vault: AccountVaultConfig{
			RoleID:   "test-role-id",
			SecretID: "test-secret-id",
		},
	}
}

func TestParseValidConfig(t *testing.T) {
	yamlContent := `
account:
  label: "production"
  team: "platform-team"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

daemon:
  mode: "daemon"
  check_interval: "30m"
  dry_run: false

rotation:
  threshold_percent: 10

vault:
  address: "https://vault.example.com"
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

	assert.Equal(t, "production", cfg.Account.Label)
	assert.Equal(t, "platform-team", cfg.Account.Team)
	assert.Equal(t, "test-role-id", cfg.Account.Vault.RoleID)
	assert.Equal(t, "test-secret-id", cfg.Account.Vault.SecretID)
	assert.Nil(t, cfg.Account.Token)

	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	assert.Equal(t, "secret", cfg.Vault.MountPath)

	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "test-token", cfg.Tokens[0].Label)
}

func TestParseConfigWithDefaults(t *testing.T) {
	yamlContent := `
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "test-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	cfg.ApplyDefaults()

	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	assert.Equal(t, "30m", cfg.Daemon.CheckInterval)
	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)
	assert.Equal(t, "secret", cfg.Vault.MountPath)
	assert.Equal(t, "info", cfg.Observability.LogLevel)
	assert.Equal(t, "https://api.linode.com", cfg.Account.APIURL)
}

func TestParseConfigWithAccountToken_StorageOnly(t *testing.T) {
	yamlContent := `
account:
  label: "lcid-1234"
  team: "platform-team"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"
  token:
    storage:
      - type: "vault"
        path: "secret/data/linode/accounts"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "managed-token"
    validity: "90d"
    scopes: "linodes:read_write"
    storage:
      - type: "vault"
        path: "secret/data/linode/managed"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)

	require.NotNil(t, cfg.Account.Token)
	assert.True(t, cfg.Account.Token.HasStorage())
	assert.False(t, cfg.Account.Token.IsManaged())

	// AllTokens should NOT include the account token (not managed)
	allTokens := cfg.AllTokens()
	require.Len(t, allTokens, 1)
	assert.Equal(t, "managed-token", allTokens[0].Label)
}

func TestParseConfigWithAccountToken_Managed(t *testing.T) {
	yamlContent := `
account:
  label: "lcid-1234"
  team: "platform-team"
  api_url: "https://api.staging.example.com"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"
  token:
    storage:
      - type: "vault"
        path: "secret/data/linode/accounts"
    label: "latr-main"
    validity: "180d"
    scopes: "*"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "managed-token"
    validity: "90d"
    scopes: "linodes:read_write"
    storage:
      - type: "vault"
        path: "secret/data/linode/managed"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)

	assert.Equal(t, "https://api.staging.example.com", cfg.Account.APIURL)
	require.NotNil(t, cfg.Account.Token)
	assert.True(t, cfg.Account.Token.IsManaged())
	assert.True(t, cfg.Account.Token.HasStorage())

	allTokens := cfg.AllTokens()
	require.Len(t, allTokens, 2)
	assert.Equal(t, "latr-main", allTokens[0].Label)
	require.Len(t, allTokens[0].Storage, 1)
	assert.Equal(t, "lcid-1234", allTokens[0].Storage[0].Key)
	assert.Equal(t, "managed-token", allTokens[1].Label)
}

func TestParseConfigAccountTokenOnly(t *testing.T) {
	yamlContent := `
account:
  label: "lcid-1234"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"
  token:
    storage:
      - type: "vault"
        path: "secret/data/linode/accounts"
    label: "latr-main"
    validity: "180d"
    scopes: "*"

vault:
  address: "https://vault.example.com"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	cfg.ApplyDefaults()

	err = cfg.Validate()
	require.NoError(t, err)

	allTokens := cfg.AllTokens()
	require.Len(t, allTokens, 1)
	assert.Equal(t, "latr-main", allTokens[0].Label)
	assert.Equal(t, "lcid-1234", allTokens[0].Storage[0].Key)
}

func TestParseConfigWithAccountVaultOverride(t *testing.T) {
	yamlContent := `
account:
  label: "production"
  vault:
    role_id: "account-role-id"
    secret_id: "account-secret-id"
    address: "https://account-vault.example.com"
    mount_path: "custom-mount"

vault:
  address: "https://global-vault.example.com"
  role_id: "global-role-id"
  secret_id: "global-secret-id"
  mount_path: "secret"

tokens:
  - label: "test-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	cfg.ApplyDefaults()

	// Account vault should override global for all fields
	resolved := cfg.Account.Vault.Resolved(cfg.Vault)
	assert.Equal(t, "https://account-vault.example.com", resolved.Address)
	assert.Equal(t, "account-role-id", resolved.RoleID)
	assert.Equal(t, "account-secret-id", resolved.SecretID)
	assert.Equal(t, "custom-mount", resolved.MountPath)
}

func TestAccountVaultResolved_FallbackToGlobal(t *testing.T) {
	global := VaultConfig{
		Address:   "https://global.example.com",
		RoleID:    "global-role",
		SecretID:  "global-secret",
		MountPath: "secret",
	}

	// Account vault with no overrides — should get all global values
	acctVault := AccountVaultConfig{}
	resolved := acctVault.Resolved(global)

	assert.Equal(t, "https://global.example.com", resolved.Address)
	assert.Equal(t, "global-role", resolved.RoleID)
	assert.Equal(t, "global-secret", resolved.SecretID)
	assert.Equal(t, "secret", resolved.MountPath)
}

func TestAccountVaultResolved_PartialOverride(t *testing.T) {
	global := VaultConfig{
		Address:   "https://global.example.com",
		RoleID:    "global-role",
		SecretID:  "global-secret",
		MountPath: "secret",
	}

	// Account overrides only credentials, inherits address and mount_path
	acctVault := AccountVaultConfig{
		RoleID:   "account-role",
		SecretID: "account-secret",
	}
	resolved := acctVault.Resolved(global)

	assert.Equal(t, "https://global.example.com", resolved.Address)
	assert.Equal(t, "account-role", resolved.RoleID)
	assert.Equal(t, "account-secret", resolved.SecretID)
	assert.Equal(t, "secret", resolved.MountPath)
}

func TestParseConfigNoAccountToken_EnvVarFallback(t *testing.T) {
	yamlContent := `
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "test-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)
	cfg.ApplyDefaults()

	assert.Nil(t, cfg.Account.Token)
	err = cfg.Validate()
	require.NoError(t, err)
}

func TestValidateConfig_MissingAccountLabel(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
		},
		Vault:  VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account label is required")
}

func TestValidateConfig_VaultCredentialsOptionalInConfig(t *testing.T) {
	// role_id and secret_id are resolved at runtime (config or env var),
	// so config validation should pass without them
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{}, // No credentials
		},
		Vault:  VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
	}

	err := cfg.Validate()
	require.NoError(t, err)
}

func TestValidateConfig_MissingVaultAddress(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
		},
		Vault:  VaultConfig{}, // No address
		Tokens: []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault address is required")
}

func TestValidateConfig_VaultAddressFromAccountOverride(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{
				RoleID:   "r",
				SecretID: "s",
				Address:  "https://account-vault.example.com", // Override
			},
		},
		Vault:  VaultConfig{}, // No global address — account override should suffice
		Tokens: []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
	}

	cfg.ApplyDefaults()
	err := cfg.Validate()
	require.NoError(t, err)
}

func TestValidateConfig_ManagedTokenRequiresStorage(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
			Token: &AccountTokenConfig{
				Label: "latr-main", Validity: "180d", Scopes: "*",
			},
		},
		Vault: VaultConfig{Address: "https://vault.example.com"},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "managed token")
	assert.Contains(t, err.Error(), "requires storage")
}

func TestValidateConfig_InvalidAccountTokenValidity(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
			Token: &AccountTokenConfig{
				Storage: []StorageConfig{{Type: "vault", Path: "p"}},
				Label: "latr-main", Validity: "7mo", Scopes: "*",
			},
		},
		Vault: VaultConfig{Address: "https://vault.example.com"},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validity period must be <= 6 months")
}

func TestValidateConfig_StorageMissingPath(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
			Token: &AccountTokenConfig{
				Storage: []StorageConfig{{Type: "vault", Path: ""}},
			},
		},
		Vault:  VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestValidateConfig_NoTokensAtAll(t *testing.T) {
	cfg := &Config{
		Account: AccountConfig{
			Label: "production",
			Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
		},
		Vault:  VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one token must be configured")
}

func TestValidateConfig_MissingRequiredFields(t *testing.T) {
	acct := AccountConfig{
		Label: "production",
		Vault: AccountVaultConfig{RoleID: "r", SecretID: "s"},
	}

	tests := []struct {
		name   string
		config *Config
		errMsg string
	}{
		{
			name: "missing token label",
			config: &Config{
				Account: acct,
				Vault:   VaultConfig{Address: "https://vault.example.com"},
				Tokens:  []TokenConfig{{Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "p"}}}},
			},
			errMsg: "token label is required",
		},
		{
			name: "missing token storage",
			config: &Config{
				Account: acct,
				Vault:   VaultConfig{Address: "https://vault.example.com"},
				Tokens:  []TokenConfig{{Label: "t", Validity: "90d", Scopes: "*", Storage: []StorageConfig{}}},
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

func TestValidateConfig_ValidityPeriodTooLong(t *testing.T) {
	cfg := &Config{
		Account: testAccount(),
		Vault:   VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{
			{Label: "test-token", Validity: "7mo", Scopes: "*",
				Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validity period must be <= 6 months")
}

func TestValidateConfig_ValidityPeriodExactly6Months(t *testing.T) {
	cfg := &Config{
		Account: testAccount(),
		Vault:   VaultConfig{Address: "https://vault.example.com"},
		Tokens: []TokenConfig{
			{Label: "test-token", Validity: "180d", Scopes: "*",
				Storage: []StorageConfig{{Type: "vault", Path: "path"}}},
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
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
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

vault:
  address: "https://vault.example.com"

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

	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)
	assert.Equal(t, 15, cfg.Tokens[0].RotationThreshold)
}

func TestParseConfigWithEnvVarSubstitution(t *testing.T) {
	os.Setenv("TEST_VAULT_ADDRESS", "https://vault-from-env.example.com")
	os.Setenv("TEST_VAULT_ROLE_ID", "role-id-from-env")
	os.Setenv("TEST_VAULT_SECRET_ID", "secret-id-from-env")
	defer func() {
		os.Unsetenv("TEST_VAULT_ADDRESS")
		os.Unsetenv("TEST_VAULT_ROLE_ID")
		os.Unsetenv("TEST_VAULT_SECRET_ID")
	}()

	yamlContent := `
account:
  label: "production"
  vault:
    role_id: "${TEST_VAULT_ROLE_ID}"
    secret_id: "${TEST_VAULT_SECRET_ID}"

vault:
  address: "${TEST_VAULT_ADDRESS}"

tokens:
  - label: "test-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/test"
`

	cfg, err := Parse([]byte(yamlContent))
	require.NoError(t, err)

	assert.Equal(t, "https://vault-from-env.example.com", cfg.Vault.Address)
	assert.Equal(t, "role-id-from-env", cfg.Account.Vault.RoleID)
	assert.Equal(t, "secret-id-from-env", cfg.Account.Vault.SecretID)
}
