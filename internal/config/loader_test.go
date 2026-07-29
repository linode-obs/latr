package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSingleFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"
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

	// Load the config
	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify loaded config
	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	assert.Equal(t, "test-role-id", cfg.Vault.RoleID)
	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "test-token", cfg.Tokens[0].Label)
}

func TestLoadFileNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadGlobPattern(t *testing.T) {
	// Create temporary config files
	tmpDir := t.TempDir()

	config1 := `
vault:
  address: "https://vault.example.com"
  role_id: "test-role-id"
  secret_id: "test-secret-id"

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

	// Load with glob pattern
	pattern := filepath.Join(tmpDir, "*.yaml")
	cfg, err := LoadGlob(pattern)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify merged config
	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	require.Len(t, cfg.Tokens, 2)

	// Tokens should be present
	labels := []string{cfg.Tokens[0].Label, cfg.Tokens[1].Label}
	assert.Contains(t, labels, "token1")
	assert.Contains(t, labels, "token2")
}

func TestLoadGlobNoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	pattern := filepath.Join(tmpDir, "*.yaml")

	cfg, err := LoadGlob(pattern)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "no config files found")
}

func TestMergeConfigs(t *testing.T) {
	config1 := &Config{
		Vault: VaultConfig{
			Address:   "https://vault.example.com",
			RoleID:    "role-id",
			SecretID:  "secret-id",
			MountPath: "secret",
		},
		Daemon: DaemonConfig{
			Mode:          "daemon",
			CheckInterval: "30m",
		},
		Rotation: RotationConfig{
			ThresholdPercent: 10,
		},
		Tokens: []TokenConfig{
			{Label: "token1", Team: "team1", Validity: "90d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path1"}}},
		},
	}

	config2 := &Config{
		Tokens: []TokenConfig{
			{Label: "token2", Team: "team2", Validity: "180d", Scopes: "*", Storage: []StorageConfig{{Type: "vault", Path: "path2"}}},
		},
	}

	merged := MergeConfigs(config1, config2)

	// Vault config should come from config1
	assert.Equal(t, "https://vault.example.com", merged.Vault.Address)
	assert.Equal(t, "role-id", merged.Vault.RoleID)

	// Daemon config should come from config1
	assert.Equal(t, "daemon", merged.Daemon.Mode)

	// Tokens should be merged
	require.Len(t, merged.Tokens, 2)
	labels := []string{merged.Tokens[0].Label, merged.Tokens[1].Label}
	assert.Contains(t, labels, "token1")
	assert.Contains(t, labels, "token2")
}

func TestMergeConfigsOverride(t *testing.T) {
	config1 := &Config{
		Vault: VaultConfig{
			Address: "https://vault1.example.com",
		},
		Rotation: RotationConfig{
			ThresholdPercent: 10,
		},
	}

	config2 := &Config{
		Vault: VaultConfig{
			Address: "https://vault2.example.com",
		},
		Rotation: RotationConfig{
			ThresholdPercent: 15,
		},
	}

	merged := MergeConfigs(config1, config2)

	// Second config should override first for non-empty values
	assert.Equal(t, "https://vault2.example.com", merged.Vault.Address)
	assert.Equal(t, 15, merged.Rotation.ThresholdPercent)
}

func TestLoadAndValidate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
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
        path: "linode/tokens/test"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load and validate
	cfg, err := LoadAndValidate(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Defaults should be applied
	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	assert.Equal(t, "30m", cfg.Daemon.CheckInterval)
	assert.Equal(t, 10, cfg.Rotation.ThresholdPercent)
}

func TestLoadAndValidateInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Missing required vault fields
	configContent := `
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

	cfg, err := LoadAndValidate(configPath)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "vault address is required")
}
