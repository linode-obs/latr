package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validProcessYAML = `
vault:
  address: "https://vault.example.com"
  role_id: "${VAULT_ROLE_ID}"
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "infra"

daemon:
  mode: "daemon"
  check_interval: "6h"

rotation:
  threshold_percent: 10
`

const validTokenYAML = `
tokens:
  - label: "workload-a"
    team: "team-a"
    validity: "90d"
    scopes: "linodes:read_only"
    storage:
      - type: "vault"
        path: "shared-all/team-a/workload-a"
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoadDir_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", validProcessYAML)
	writeFile(t, dir, "team-a-config.yml", validTokenYAML)
	writeFile(t, dir, "team-b-config.yml", `
tokens:
  - label: "workload-b"
    team: "team-b"
    validity: "180d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "shared-all/team-b/workload-b"
`)

	cfg, err := LoadDir(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "https://vault.example.com", cfg.Vault.Address)
	assert.Equal(t, "infra", cfg.Vault.MountPath)
	assert.Equal(t, "daemon", cfg.Daemon.Mode)
	require.Len(t, cfg.Tokens, 2)

	labels := []string{cfg.Tokens[0].Label, cfg.Tokens[1].Label}
	assert.Contains(t, labels, "workload-a")
	assert.Contains(t, labels, "workload-b")
}

func TestLoadDir_MissingProcess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "team-a-config.yml", validTokenYAML)

	cfg, err := LoadDir(dir)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "missing process config")
}

func TestLoadDir_NoTokenFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", validProcessYAML)

	cfg, err := LoadDir(dir)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "at least one token config")
}

func TestLoadDir_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "file.yaml", validProcessYAML)

	cfg, err := LoadDir(path)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestLoadProcessAndTokenConfigs(t *testing.T) {
	dir := t.TempDir()
	process := writeFile(t, dir, "_process.yaml", validProcessYAML)
	token := writeFile(t, dir, "team.yml", validTokenYAML)

	cfg, err := LoadProcessAndTokenConfigs(process, []string{token})
	require.NoError(t, err)
	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "workload-a", cfg.Tokens[0].Label)
}

func TestLoadProcessAndTokenConfigs_RequiresTokenFiles(t *testing.T) {
	dir := t.TempDir()
	process := writeFile(t, dir, "_process.yaml", validProcessYAML)

	cfg, err := LoadProcessAndTokenConfigs(process, nil)
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "at least one token config")
}

func TestCheck_AllowsEmptyVaultCredentials(t *testing.T) {
	// Unset so ${VAULT_*} expand to empty (CI case).
	t.Setenv("VAULT_ROLE_ID", "")
	t.Setenv("VAULT_SECRET_ID", "")

	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", validProcessYAML)
	writeFile(t, dir, "team.yml", validTokenYAML)

	cfg, err := LoadDir(dir)
	require.NoError(t, err)

	// After expand, credentials are empty.
	assert.Empty(t, cfg.Vault.RoleID)
	assert.Empty(t, cfg.Vault.SecretID)

	err = Check(cfg, CheckOptions{})
	require.NoError(t, err)
	assert.Equal(t, vaultCredentialPlaceholder, cfg.Vault.RoleID)
	assert.Equal(t, vaultCredentialPlaceholder, cfg.Vault.SecretID)
}

func TestCheck_RequireVaultCredentials(t *testing.T) {
	t.Setenv("VAULT_ROLE_ID", "")
	t.Setenv("VAULT_SECRET_ID", "")

	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", validProcessYAML)
	writeFile(t, dir, "team.yml", validTokenYAML)

	cfg, err := LoadDir(dir)
	require.NoError(t, err)

	err = Check(cfg, CheckOptions{RequireVaultCredentials: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_id")
}

func TestCheck_RejectsDuplicateLabels(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", `
vault:
  address: "https://vault.example.com"
  role_id: "role"
  secret_id: "secret"
`)
	writeFile(t, dir, "a.yml", `
tokens:
  - label: "same"
    team: "a"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path/a"
`)
	writeFile(t, dir, "b.yml", `
tokens:
  - label: "same"
    team: "b"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path/b"
`)

	cfg, err := LoadDirAndCheck(dir, CheckOptions{})
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "duplicate token label")
	assert.Contains(t, err.Error(), "same")
}

func TestLoadDirAndCheck_OK(t *testing.T) {
	t.Setenv("VAULT_ROLE_ID", "")
	t.Setenv("VAULT_SECRET_ID", "")

	dir := t.TempDir()
	writeFile(t, dir, "_process.yaml", validProcessYAML)
	writeFile(t, dir, "team.yml", validTokenYAML)

	cfg, err := LoadDirAndCheck(dir, CheckOptions{})
	require.NoError(t, err)
	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "workload-a", cfg.Tokens[0].Label)
}

func TestLoadAndCheck_ClassicSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
vault:
  address: "https://vault.example.com"
  role_id: "role"
  secret_id: "secret"

tokens:
  - label: "classic"
    team: "platform"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/classic"
`)

	cfg, err := LoadAndCheck(path, CheckOptions{RequireVaultCredentials: true})
	require.NoError(t, err)
	require.Len(t, cfg.Tokens, 1)
	assert.Equal(t, "classic", cfg.Tokens[0].Label)
	assert.Equal(t, "daemon", cfg.Daemon.Mode) // default
}

func TestLoadAndCheck_InvalidMissingTokens(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
vault:
  address: "https://vault.example.com"
  role_id: "role"
  secret_id: "secret"
`)

	cfg, err := LoadAndCheck(path, CheckOptions{})
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "at least one token")
}

func TestLoadFilesAndCheck_MergeOrder(t *testing.T) {
	dir := t.TempDir()
	p1 := writeFile(t, dir, "base.yaml", `
vault:
  address: "https://vault.example.com"
  role_id: "role"
  secret_id: "secret"
tokens:
  - label: "one"
    team: "t"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path/one"
`)
	p2 := writeFile(t, dir, "extra.yaml", `
tokens:
  - label: "two"
    team: "t"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "path/two"
`)

	cfg, err := LoadFilesAndCheck([]string{p1, p2}, CheckOptions{})
	require.NoError(t, err)
	require.Len(t, cfg.Tokens, 2)
}
