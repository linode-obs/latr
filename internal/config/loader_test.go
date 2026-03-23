package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

vault:
  address: "https://vault.example.com"
  mount_path: "secret"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "linode/tokens/test"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "production", cfg.Account.Label)
	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "test-token", cfg.Tokens[0].Label)
}

func TestLoadFileNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadAllMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	config1 := `
account:
  label: "production"
  team: "platform-team"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "token1"
    team: "team1"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "linode/tokens/token1"
`

	config2 := `
account:
  label: "staging"
  team: "platform-team"
  vault:
    role_id: "staging-role-id"
    secret_id: "staging-secret-id"
  api_url: "https://api.staging.example.com"

tokens:
  - label: "token2"
    team: "team2"
    validity: "180d"
    scopes: "linodes:read_only"
    storage:
      - type: "vault"
        path: "linode/tokens/token2"
`

	err := os.WriteFile(filepath.Join(tmpDir, "config1.yaml"), []byte(config1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "config2.yaml"), []byte(config2), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAll(pattern)
	require.NoError(t, err)
	require.Len(t, configs, 2)

	// Each config should have its own account
	labels := []string{configs[0].Account.Label, configs[1].Account.Label}
	assert.Contains(t, labels, "production")
	assert.Contains(t, labels, "staging")

	// Tokens should NOT be merged
	for _, cfg := range configs {
		require.Len(t, cfg.Tokens, 1)
	}
}

func TestLoadAllSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
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

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	configs, err := LoadAll(configPath)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "production", configs[0].Account.Label)
}

func TestLoadAllNoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	pattern := filepath.Join(tmpDir, "*.yaml")

	configs, err := LoadAll(pattern)
	require.Error(t, err)
	assert.Nil(t, configs)
	assert.Contains(t, err.Error(), "no config files found")
}

func TestLoadAndValidateMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	config1 := `
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

daemon:
  mode: "daemon"
  check_interval: "30m"

rotation:
  threshold_percent: 10

vault:
  address: "https://vault.example.com"

tokens:
  - label: "token1"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path1"
`

	config2 := `
account:
  label: "staging"
  vault:
    role_id: "staging-role-id"
    secret_id: "staging-secret-id"
  api_url: "https://api.staging.example.com"

tokens:
  - label: "token2"
    validity: "180d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path2"
`

	err := os.WriteFile(filepath.Join(tmpDir, "config1.yaml"), []byte(config1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "config2.yaml"), []byte(config2), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.NoError(t, err)
	require.Len(t, configs, 2)

	// Global settings should be propagated to second config
	assert.Equal(t, "https://vault.example.com", configs[1].Vault.Address)
	assert.Equal(t, "staging-role-id", configs[1].Account.Vault.RoleID)
	assert.Equal(t, "daemon", configs[1].Daemon.Mode)
	assert.Equal(t, 10, configs[1].Rotation.ThresholdPercent)

	// Account-specific settings should remain independent
	assert.Equal(t, "production", configs[0].Account.Label)
	assert.Equal(t, "staging", configs[1].Account.Label)
	assert.Equal(t, "https://api.staging.example.com", configs[1].Account.APIURL)
}

func TestLoadAndValidateSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
account:
  label: "production"
  vault:
    role_id: "test-role-id"
    secret_id: "test-secret-id"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "test-token"
    team: "platform-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "linode/tokens/test"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	configs, err := LoadAndValidate(configPath)
	require.NoError(t, err)
	require.Len(t, configs, 1)

	assert.Equal(t, "daemon", configs[0].Daemon.Mode)
	assert.Equal(t, "30m", configs[0].Daemon.CheckInterval)
	assert.Equal(t, 10, configs[0].Rotation.ThresholdPercent)
	assert.Equal(t, "", configs[0].Account.APIURL)
}

func TestLoadAndValidateGlobalsOnlyPlusAccounts(t *testing.T) {
	tmpDir := t.TempDir()

	globals := `
global: true

daemon:
  mode: "daemon"
  check_interval: "30m"

rotation:
  threshold_percent: 10

vault:
  address: "https://vault.example.com"
  role_id: "global-role-id"
  secret_id: "global-secret-id"

observability:
  log_level: "info"
`

	account := `
account:
  label: "lcid-1234"
  team: "platform-team"

tokens:
  - label: "my-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "tokens/my-token"
`

	err := os.WriteFile(filepath.Join(tmpDir, "globals.yaml"), []byte(globals), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "account.yaml"), []byte(account), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.NoError(t, err)

	// Should have 2 configs — global + account
	require.Len(t, configs, 2)

	// Find the global config
	var globalIdx, acctIdx int
	for i, cfg := range configs {
		if cfg.IsGlobal() {
			globalIdx = i
		} else {
			acctIdx = i
		}
	}
	assert.True(t, configs[globalIdx].IsGlobal())

	// Account config should have inherited global settings
	acct := configs[acctIdx]
	assert.Equal(t, "lcid-1234", acct.Account.Label)
	assert.Equal(t, "https://vault.example.com", acct.Vault.Address)
	assert.Equal(t, "global-role-id", acct.Vault.RoleID)
	assert.Equal(t, "daemon", acct.Daemon.Mode)
	assert.Equal(t, 10, acct.Rotation.ThresholdPercent)
}

func TestLoadAndValidateOnlyGlobalConfig_Fails(t *testing.T) {
	tmpDir := t.TempDir()

	globals := `
global: true

daemon:
  mode: "daemon"

vault:
  address: "https://vault.example.com"
`

	err := os.WriteFile(filepath.Join(tmpDir, "globals.yaml"), []byte(globals), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.Error(t, err)
	assert.Nil(t, configs)
	assert.Contains(t, err.Error(), "at least one config file must have an account block")
}

func TestLoadAndValidateGlobalWithAccount(t *testing.T) {
	tmpDir := t.TempDir()

	combined := `
global: true

account:
  label: "lcid-1234"
  team: "platform-team"

daemon:
  mode: "one-shot"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "my-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "tokens/my-token"
`

	err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(combined), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.NoError(t, err)
	require.Len(t, configs, 1)

	cfg := configs[0]
	assert.True(t, cfg.IsGlobal())
	assert.Equal(t, "lcid-1234", cfg.Account.Label)
	assert.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "one-shot", cfg.Daemon.Mode)
}

func TestLoadAndValidateMultipleGlobals_Fails(t *testing.T) {
	tmpDir := t.TempDir()

	globals1 := `
global: true
vault:
  address: "https://vault1.example.com"
`
	globals2 := `
global: true
vault:
  address: "https://vault2.example.com"
`
	account := `
account:
  label: "test"
tokens:
  - label: "t"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "p"
`

	err := os.WriteFile(filepath.Join(tmpDir, "a-globals1.yaml"), []byte(globals1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "b-globals2.yaml"), []byte(globals2), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "c-account.yaml"), []byte(account), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.Error(t, err)
	assert.Nil(t, configs)
	assert.Contains(t, err.Error(), "multiple configs marked as global")
}

func TestLoadAndValidateDuplicateAccountLabels(t *testing.T) {
	tmpDir := t.TempDir()

	config1 := `
account:
  label: "same-label"
  vault:
    role_id: "r1"
    secret_id: "s1"

vault:
  address: "https://vault.example.com"

tokens:
  - label: "token1"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path1"
`

	config2 := `
account:
  label: "same-label"
  vault:
    role_id: "r2"
    secret_id: "s2"

tokens:
  - label: "token2"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path2"
`

	err := os.WriteFile(filepath.Join(tmpDir, "config1.yaml"), []byte(config1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "config2.yaml"), []byte(config2), 0644)
	require.NoError(t, err)

	pattern := filepath.Join(tmpDir, "*.yaml")
	configs, err := LoadAndValidate(pattern)
	require.Error(t, err)
	assert.Nil(t, configs)
	assert.Contains(t, err.Error(), "duplicate account label")
	assert.Contains(t, err.Error(), "same-label")
}

func TestLoadAndValidateInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
vault:
  address: "https://vault.example.com"

tokens:
  - label: "test-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "linode/tokens/test"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	configs, err := LoadAndValidate(configPath)
	require.Error(t, err)
	assert.Nil(t, configs)
	assert.Contains(t, err.Error(), "account label is required")
}
