package vault

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/linode-obs/latr/pkg/models"
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

// WriteToken writes a token value to a KV v2 path
func (c *Client) WriteToken(ctx context.Context, path string, token string) error {
	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)

	data := map[string]interface{}{
		"data": map[string]interface{}{
			"token": token,
		},
	}

	_, err := c.client.Logical().WriteWithContext(ctx, fullPath, data)
	if err != nil {
		return fmt.Errorf("failed to write token to vault: %w", err)
	}

	return nil
}

// ReadToken reads a token value from a KV v2 path
func (c *Client) ReadToken(ctx context.Context, path string) (string, error) {
	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)

	secret, err := c.client.Logical().ReadWithContext(ctx, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read token from vault: %w", err)
	}

	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("no data found at path: %s", path)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid data structure at path: %s", path)
	}

	tokenValue, ok := data["token"].(string)
	if !ok {
		return "", fmt.Errorf("token value not found at path: %s", path)
	}

	return tokenValue, nil
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
