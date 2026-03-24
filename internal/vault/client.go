package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/wbh1/latr/pkg/models"
)

// Config holds Vault client configuration
type Config struct {
	Address   string
	RoleID    string
	SecretID  string
	MountPath string
}

// Client wraps the Vault API client
type Client struct {
	client    *api.Client
	mountPath string
}

// NewClient creates a new Vault client and authenticates using AppRole
func NewClient(config *Config) (*Client, error) {
	vaultConfig := api.DefaultConfig()
	vaultConfig.Address = config.Address

	client, err := api.NewClient(vaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	// Authenticate using AppRole
	if err := authenticateAppRole(client, config.RoleID, config.SecretID); err != nil {
		return nil, fmt.Errorf("failed to authenticate with vault: %w", err)
	}

	return &Client{
		client:    client,
		mountPath: config.MountPath,
	}, nil
}

// authenticateAppRole performs AppRole authentication
func authenticateAppRole(client *api.Client, roleID, secretID string) error {
	data := map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	}

	resp, err := client.Logical().Write("auth/approle/login", data)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if resp == nil || resp.Auth == nil {
		return fmt.Errorf("no auth info returned from vault")
	}

	client.SetToken(resp.Auth.ClientToken)
	return nil
}

// WriteToken writes a token value to a KV v2 path.
// The key parameter specifies the key name within the secret. If empty, defaults to "token".
// Uses JSON Merge Patch (PATCH) to update only the specified key, preserving any other
// keys that may exist at the same path (e.g., multiple accounts sharing a secret path).
func (c *Client) WriteToken(ctx context.Context, path, key, token string) error {
	if key == "" {
		key = "token"
	}

	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)

	data := map[string]interface{}{
		"data": map[string]interface{}{
			key: token,
		},
	}

	_, err := c.client.Logical().JSONMergePatch(ctx, fullPath, data)
	if err != nil {
		var respErr *api.ResponseError
		if !errors.As(err, &respErr) {
			return fmt.Errorf("failed to patch token in vault: %w", err)
		}

		switch respErr.StatusCode {
		case http.StatusNotFound:
			// Secret doesn't exist yet — safe to do a full write
			_, err = c.client.Logical().WriteWithContext(ctx, fullPath, data)
			if err != nil {
				return fmt.Errorf("failed to write token to vault: %w", err)
			}
		case http.StatusForbidden, http.StatusMethodNotAllowed:
			// PATCH not permitted (403) or not supported (405) — read
			// existing data, merge, then write to preserve sibling keys
			merged, readErr := c.readMergeData(ctx, fullPath, key, token)
			if readErr != nil {
				return fmt.Errorf("failed to read existing secret for merge: %w", readErr)
			}
			_, err = c.client.Logical().WriteWithContext(ctx, fullPath, merged)
			if err != nil {
				return fmt.Errorf("failed to write merged token to vault: %w", err)
			}
		default:
			return fmt.Errorf("failed to patch token in vault: %w", err)
		}
	}

	return nil
}

// readMergeData reads the existing secret at fullPath, merges the given key/value
// into its data map, and returns the merged payload ready for a full write.
func (c *Client) readMergeData(ctx context.Context, fullPath, key, token string) (map[string]interface{}, error) {
	existing, err := c.client.Logical().ReadWithContext(ctx, fullPath)
	if err != nil {
		return nil, err
	}

	merged := map[string]interface{}{key: token}
	if existing != nil && existing.Data != nil {
		existingData, ok := existing.Data["data"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid existing data structure at path: %s", fullPath)
		}
		for k, v := range existingData {
			if k != key {
				merged[k] = v
			}
		}
	}

	return map[string]interface{}{"data": merged}, nil
}

// ReadSecretKey reads a specific key from a KV v2 secret at the given path
func (c *Client) ReadSecretKey(ctx context.Context, path, key string) (string, error) {
	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)

	secret, err := c.client.Logical().ReadWithContext(ctx, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret from vault path %s: %w", path, err)
	}

	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("no data found at vault path: %s", path)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid data structure at vault path: %s", path)
	}

	rawValue, exists := data[key]
	if !exists {
		return "", fmt.Errorf("key %q not found at vault path: %s", key, path)
	}

	value, ok := rawValue.(string)
	if !ok {
		return "", fmt.Errorf("key %q at vault path %s has non-string type %T", key, path, rawValue)
	}

	return value, nil
}

// WriteTokenState writes token state to Vault metadata
func (c *Client) WriteTokenState(ctx context.Context, path string, state *models.TokenState) error {
	metadataPath := fmt.Sprintf("%s/metadata/%s", c.mountPath, path)

	customMetadata := map[string]interface{}{
		"label":              state.Label,
		"current_linode_id":  strconv.Itoa(state.CurrentLinodeID),
		"last_rotated_at":    state.LastRotatedAt.Format(time.RFC3339),
		"previous_linode_id": strconv.Itoa(state.PreviousLinodeID),
		"rotation_count":     strconv.Itoa(state.RotationCount),
	}

	if !state.PreviousExpiresAt.IsZero() {
		customMetadata["previous_expires_at"] = state.PreviousExpiresAt.Format(time.RFC3339)
	}

	data := map[string]interface{}{
		"custom_metadata": customMetadata,
	}

	_, err := c.client.Logical().WriteWithContext(ctx, metadataPath, data)
	if err != nil {
		return fmt.Errorf("failed to write token state to vault: %w", err)
	}

	return nil
}

// ReadTokenState reads token state from Vault metadata
func (c *Client) ReadTokenState(ctx context.Context, path string) (*models.TokenState, error) {
	metadataPath := fmt.Sprintf("%s/metadata/%s", c.mountPath, path)

	secret, err := c.client.Logical().ReadWithContext(ctx, metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token state from vault: %w", err)
	}

	// If no metadata exists yet, return nil (this is a new token)
	if secret == nil || secret.Data == nil {
		return nil, nil
	}

	customMetadata, ok := secret.Data["custom_metadata"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	state := &models.TokenState{}

	if label, ok := customMetadata["label"].(string); ok {
		state.Label = label
	}

	if currentID, ok := customMetadata["current_linode_id"].(string); ok {
		if id, err := strconv.Atoi(currentID); err == nil {
			state.CurrentLinodeID = id
		}
	}

	if lastRotated, ok := customMetadata["last_rotated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, lastRotated); err == nil {
			state.LastRotatedAt = t
		}
	}

	if previousID, ok := customMetadata["previous_linode_id"].(string); ok {
		if id, err := strconv.Atoi(previousID); err == nil {
			state.PreviousLinodeID = id
		}
	}

	if previousExpires, ok := customMetadata["previous_expires_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, previousExpires); err == nil {
			state.PreviousExpiresAt = t
		}
	}

	if rotationCount, ok := customMetadata["rotation_count"].(string); ok {
		if count, err := strconv.Atoi(rotationCount); err == nil {
			state.RotationCount = count
		}
	}

	return state, nil
}
