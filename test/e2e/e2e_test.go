// ABOUTME: End-to-end tests for latr using Docker Compose
// ABOUTME: Tests complete workflows with real Vault and mock Linode API
//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	latrBinary  string
	vaultAddr   = "http://localhost:8200"
	vaultToken  = "root"
	roleID      string
	secretID    string
	mockLinode  = "http://localhost:8080"
	composeFile string
)

func TestMain(m *testing.M) {
	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	// Get absolute path to project root
	projectRoot, err := getProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get project root: %v\n", err)
		exitCode = 1
		return
	}

	// Set compose file path
	composeFile = filepath.Join(projectRoot, "test", "e2e", "docker-compose.yml")

	// Build latr binary
	fmt.Println("Building latr binary...")
	latrBinary = filepath.Join(projectRoot, "latr-e2e")
	buildCmd := exec.Command("go", "build", "-o", latrBinary, "./cmd/latr")
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build latr: %v\n%s\n", err, output)
		exitCode = 1
		return
	}
	defer os.Remove(latrBinary)

	// Start Docker Compose
	fmt.Println("Starting Docker Compose...")
	upCmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--build")
	if output, err := upCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start Docker Compose: %v\n%s\n", err, output)
		exitCode = 1
		return
	}

	// Ensure cleanup happens
	defer func() {
		fmt.Println("Cleaning up Docker Compose...")
		downCmd := exec.Command("docker", "compose", "-f", composeFile, "down", "-v")
		if output, err := downCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clean up Docker Compose: %v\n%s\n", err, output)
		}
	}()

	// Wait for services to be ready
	fmt.Println("Waiting for services to be ready...")
	if err := waitForService(vaultAddr+"/v1/sys/health", 30*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Vault failed to become ready: %v\n", err)
		exitCode = 1
		return
	}
	if err := waitForService(mockLinode+"/health", 30*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Mock Linode failed to become ready: %v\n", err)
		exitCode = 1
		return
	}

	// Initialize Vault
	fmt.Println("Initializing Vault...")
	if err := initializeVault(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Vault: %v\n", err)
		exitCode = 1
		return
	}

	// Run tests
	fmt.Println("Running tests...")
	exitCode = m.Run()
}

func getProjectRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(output)), nil
}

func waitForService(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for service at %s", url)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

func initializeVault() error {
	// Enable KV v2 secrets engine (already enabled in dev mode at "secret/")
	// Enable AppRole auth (ignore error if already enabled)
	if err := vaultExec("auth", "enable", "approle"); err != nil {
		// Check if error is because approle is already enabled
		if !strings.Contains(err.Error(), "path is already in use") {
			return fmt.Errorf("failed to enable approle: %w", err)
		}
	}

	// Create policy
	policy := `
path "secret/data/e2e/*" {
  capabilities = ["create", "read", "update", "patch", "delete"]
}
path "secret/metadata/e2e/*" {
  capabilities = ["create", "read", "update", "list", "delete"]
}
`
	// Write policy using stdin instead of a file
	if err := vaultExecWithStdin(policy, "policy", "write", "latr-e2e", "-"); err != nil {
		return fmt.Errorf("failed to write policy: %w", err)
	}

	// Create AppRole
	if err := vaultExec("write", "auth/approle/role/latr-e2e",
		"token_policies=latr-e2e",
		"token_ttl=1h",
		"token_max_ttl=4h"); err != nil {
		return fmt.Errorf("failed to create approle: %w", err)
	}

	// Get role ID
	output, err := vaultExecOutput("read", "-field=role_id", "auth/approle/role/latr-e2e/role-id")
	if err != nil {
		return fmt.Errorf("failed to get role_id: %w", err)
	}
	roleID = string(bytes.TrimSpace(output))

	// Generate secret ID
	output, err = vaultExecOutput("write", "-field=secret_id", "-f", "auth/approle/role/latr-e2e/secret-id")
	if err != nil {
		return fmt.Errorf("failed to generate secret_id: %w", err)
	}
	secretID = string(bytes.TrimSpace(output))

	return nil
}

func vaultExec(args ...string) error {
	_, err := vaultExecOutput(args...)
	return err
}

func vaultExecOutput(args ...string) ([]byte, error) {
	// Run vault commands inside the Docker container
	dockerArgs := []string{"compose", "-f", composeFile, "exec", "-T",
		"-e", "VAULT_ADDR=http://127.0.0.1:8200",
		"-e", "VAULT_TOKEN=" + vaultToken,
		"vault", "vault"}
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("vault command failed: %w\n%s", err, output)
	}
	return output, nil
}

func vaultExecWithStdin(stdin string, args ...string) error {
	// Run vault commands inside the Docker container with stdin
	dockerArgs := []string{"compose", "-f", composeFile, "exec", "-T",
		"-e", "VAULT_ADDR=http://127.0.0.1:8200",
		"-e", "VAULT_TOKEN=" + vaultToken,
		"vault", "vault"}
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("vault command failed: %w\n%s", err, output)
	}
	return nil
}

// Helper function to run latr with config
func runLatr(t *testing.T, configPath string) (stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, latrBinary, "-config", configPath)
	cmd.Env = append(os.Environ(),
		"LINODE_TOKEN=test-token",
		"LINODE_API_URL="+mockLinode,
		"VAULT_ROLE_ID="+roleID,
		"VAULT_SECRET_ID="+secretID,
	)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("latr command timed out. stdout: %s\nstderr: %s", stdout, stderr)
	}

	if err != nil {
		t.Logf("latr failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	return stdout, stderr
}

// Helper function to get Vault secret
func getVaultSecret(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	url := fmt.Sprintf("%s/v1/%s", vaultAddr, path)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", vaultToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil
	}
	require.Equal(t, 200, resp.StatusCode, "unexpected status code from Vault")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &result))

	return result.Data.Data
}

// Helper function to get Vault metadata
func getVaultMetadata(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	// Convert data path to metadata path
	metadataPath := path
	if strings.Contains(path, "/data/") {
		metadataPath = strings.ReplaceAll(path, "/data/", "/metadata/")
	}

	url := fmt.Sprintf("%s/v1/%s", vaultAddr, metadataPath)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", vaultToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil
	}
	require.Equal(t, 200, resp.StatusCode, "unexpected status code from Vault")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Data struct {
			CustomMetadata map[string]interface{} `json:"custom_metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &result))

	return result.Data.CustomMetadata
}

// Helper function to reset mock Linode state
func resetMockLinode(t *testing.T) {
	t.Helper()

	resp, err := http.Post(mockLinode+"/reset", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
}

// Helper function to setup mock Linode token
func setupMockLinodeToken(t *testing.T, label string, expiry time.Time) int {
	t.Helper()

	reqBody := map[string]string{
		"label":  label,
		"scopes": "*",
		"expiry": expiry.Format(time.RFC3339),
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := http.Post(mockLinode+"/v4/profile/tokens", "application/json", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var result struct {
		ID int `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	return result.ID
}

// Helper function to get mock Linode tokens
func getMockLinodeTokens(t *testing.T) []map[string]interface{} {
	t.Helper()

	resp, err := http.Get(mockLinode + "/v4/profile/tokens")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	return result.Data
}

func TestE2E_CreateToken(t *testing.T) {
	// Setup: Reset mock state
	resetMockLinode(t)

	// Create config file using environment variable references
	configContent := `account:
  label: "e2e-create"
  team: "test-team"
  vault:
    role_id: "${VAULT_ROLE_ID}"
    secret_id: "${VAULT_SECRET_ID}"

daemon:
  mode: "one-shot"
  dry_run: false

rotation:
  threshold_percent: 10

vault:
  address: "http://localhost:8200"
  mount_path: "secret"

observability:
  log_level: "info"

tokens:
  - label: "e2e-test-create"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "e2e/test-create"
`

	configPath := filepath.Join(os.TempDir(), "latr-e2e-create-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(configPath)

	// Execute: Run latr
	stdout, stderr := runLatr(t, configPath)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Validate: Token created in mock Linode
	tokens := getMockLinodeTokens(t)
	require.Len(t, tokens, 1, "expected exactly one token in mock Linode")
	assert.Equal(t, "e2e-test-create", tokens[0]["label"])
	assert.Equal(t, "*", tokens[0]["scopes"])

	// Validate: Token stored in Vault
	secret := getVaultSecret(t, "secret/data/e2e/test-create")
	require.NotNil(t, secret, "expected secret to exist in Vault")
	assert.NotEmpty(t, secret["token"], "expected token value in Vault")

	// Validate: Vault metadata
	metadata := getVaultMetadata(t, "secret/data/e2e/test-create")
	require.NotNil(t, metadata, "expected metadata to exist in Vault")
	t.Logf("Metadata: %+v", metadata)
	assert.Equal(t, "e2e-test-create", metadata["label"])
	assert.NotEmpty(t, metadata["current_linode_id"])
	assert.Equal(t, "0", fmt.Sprintf("%v", metadata["rotation_count"]))
}

func TestE2E_RotateToken(t *testing.T) {
	// Setup: Reset mock state
	resetMockLinode(t)

	// Setup: Create existing token that's 95% expired (5% validity remaining)
	now := time.Now()
	validity := 90 * 24 * time.Hour          // 90 days
	created := now.Add(-validity * 95 / 100) // Created 95% of validity ago
	expiry := created.Add(validity)          // Expires 5% from now

	oldTokenID := setupMockLinodeToken(t, "e2e-test-rotate", expiry)
	t.Logf("Setup old token with ID: %d, expiry: %s", oldTokenID, expiry.Format(time.RFC3339))

	// Create config file using environment variable references
	configContent := `account:
  label: "e2e-rotate"
  team: "test-team"
  vault:
    role_id: "${VAULT_ROLE_ID}"
    secret_id: "${VAULT_SECRET_ID}"

daemon:
  mode: "one-shot"
  dry_run: false

rotation:
  threshold_percent: 10

vault:
  address: "http://localhost:8200"
  mount_path: "secret"

observability:
  log_level: "info"

tokens:
  - label: "e2e-test-rotate"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "e2e/test-rotate"
`

	configPath := filepath.Join(os.TempDir(), "latr-e2e-rotate-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(configPath)

	// Execute: Run latr
	stdout, stderr := runLatr(t, configPath)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Validate: New token created in mock Linode
	tokens := getMockLinodeTokens(t)
	require.Len(t, tokens, 2, "expected old token + new token in mock Linode")

	// Find the new token (different ID from old)
	var newToken map[string]interface{}
	for _, token := range tokens {
		if int(token["id"].(float64)) != oldTokenID {
			newToken = token
			break
		}
	}
	require.NotNil(t, newToken, "expected to find new token")
	assert.Equal(t, "e2e-test-rotate", newToken["label"])

	// Validate: New token stored in Vault
	secret := getVaultSecret(t, "secret/data/e2e/test-rotate")
	require.NotNil(t, secret, "expected secret to exist in Vault")
	assert.NotEmpty(t, secret["token"], "expected new token value in Vault")

	// Validate: Vault metadata shows rotation
	metadata := getVaultMetadata(t, "secret/data/e2e/test-rotate")
	require.NotNil(t, metadata, "expected metadata to exist in Vault")
	assert.Equal(t, "1", fmt.Sprintf("%v", metadata["rotation_count"]))
	assert.NotEmpty(t, metadata["previous_linode_id"])
}

func TestE2E_DryRunMode(t *testing.T) {
	// Setup: Reset mock state
	resetMockLinode(t)

	// Create config file with dry_run enabled using environment variable references
	configContent := `account:
  label: "e2e-dryrun"
  team: "test-team"
  vault:
    role_id: "${VAULT_ROLE_ID}"
    secret_id: "${VAULT_SECRET_ID}"

daemon:
  mode: "one-shot"
  dry_run: true

rotation:
  threshold_percent: 10

vault:
  address: "http://localhost:8200"
  mount_path: "secret"

observability:
  log_level: "info"

tokens:
  - label: "e2e-test-dryrun"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "e2e/test-dryrun"
`

	configPath := filepath.Join(os.TempDir(), "latr-e2e-dryrun-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(configPath)

	// Execute: Run latr in dry-run mode
	stdout, stderr := runLatr(t, configPath)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Validate: NO token created in mock Linode
	tokens := getMockLinodeTokens(t)
	assert.Len(t, tokens, 0, "expected no tokens in mock Linode (dry-run mode)")

	// Validate: NO data written to Vault
	secret := getVaultSecret(t, "secret/data/e2e/test-dryrun")
	assert.Nil(t, secret, "expected no secret in Vault (dry-run mode)")

	// Validate: Logs indicate dry-run mode
	combinedOutput := stdout + stderr
	assert.Contains(t, combinedOutput, "DRY RUN", "expected dry-run indicator in logs")
}

func TestE2E_DaemonMode(t *testing.T) {
	// Setup: Reset mock state
	resetMockLinode(t)

	// Setup: Create token that needs rotation
	now := time.Now()
	validity := 90 * 24 * time.Hour
	created := now.Add(-validity * 95 / 100)
	expiry := created.Add(validity)

	setupMockLinodeToken(t, "e2e-test-daemon", expiry)

	// Create config file using environment variable references
	configContent := `account:
  label: "e2e-daemon"
  team: "test-team"
  vault:
    role_id: "${VAULT_ROLE_ID}"
    secret_id: "${VAULT_SECRET_ID}"

daemon:
  mode: "daemon"
  check_interval: "5s"
  dry_run: false

rotation:
  threshold_percent: 10

vault:
  address: "http://localhost:8200"
  mount_path: "secret"

observability:
  log_level: "info"

tokens:
  - label: "e2e-test-daemon"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "e2e/test-daemon"
`

	configPath := filepath.Join(os.TempDir(), "latr-e2e-daemon-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(configPath)

	// Execute: Run latr in daemon mode (background)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, latrBinary, "-config", configPath)
	cmd.Env = append(os.Environ(),
		"LINODE_TOKEN=test-token",
		"LINODE_API_URL="+mockLinode,
		"VAULT_ROLE_ID="+roleID,
		"VAULT_SECRET_ID="+secretID,
	)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	require.NoError(t, cmd.Start())

	// Wait for first rotation cycle (up to 10 seconds)
	var secret map[string]interface{}
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		secret = getVaultSecret(t, "secret/data/e2e/test-daemon")
		if secret != nil {
			break
		}
	}

	// Send SIGTERM for graceful shutdown
	require.NoError(t, cmd.Process.Signal(os.Interrupt))

	// Wait for process to exit
	_ = cmd.Wait()

	stdout := outBuf.String()
	stderr := errBuf.String()
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Validate: Token was rotated
	require.NotNil(t, secret, "expected token to be rotated in Vault")
	assert.NotEmpty(t, secret["token"])

	// Validate: Logs show daemon mode and graceful shutdown
	combinedOutput := stdout + stderr
	assert.Contains(t, combinedOutput, "daemon mode", "expected daemon mode indicator in logs")
}
