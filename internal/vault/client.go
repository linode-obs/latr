package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/linode-obs/latr/internal/observability"
	"github.com/linode-obs/latr/pkg/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

// defaultDataKey is used when WriteToken/ReadToken are called with an empty key.
const defaultDataKey = "token"

// Write actions for KV secret data maps.
const (
	WriteActionReplace = "replace"
	WriteActionAppend  = "append"
)

// maxAppendCASAttempts bounds read-modify-write retries when check-and-set conflicts.
const maxAppendCASAttempts = 5

// resolveDataKey returns key, or defaultDataKey when key is empty after trim.
func resolveDataKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return defaultDataKey
	}
	return key
}

// resolveWriteAction returns a valid write action; empty defaults to replace.
// Comparison is case-insensitive after trim.
func resolveWriteAction(action string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", WriteActionReplace:
		return WriteActionReplace, nil
	case WriteActionAppend:
		return WriteActionAppend, nil
	default:
		return "", fmt.Errorf("invalid write action %q (want %q or %q)", action, WriteActionReplace, WriteActionAppend)
	}
}

// WriteToken writes a token value to a KV v2 path under the given data key.
//
// Path is relative to mount_path (do not include the mount name or a "data/"
// prefix). The client writes to "{mount_path}/data/{path}".
//
//   - key empty → stored under "token"
//   - action "replace" (default): secret data map becomes only {key: token}.
//     Other data keys on that secret are removed. Custom metadata is untouched.
//   - action "append": merge key into existing data via read-modify-write with
//     KV v2 check-and-set (CAS). Other keys are preserved. Missing secret is
//     created with just this key. AppRole policy must allow read+create+update
//     on the data path.
//
// Append retries on CAS conflict up to maxAppendCASAttempts times.
func (c *Client) WriteToken(ctx context.Context, path, token, key, action string) error {
	start := time.Now()
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "Vault.WriteToken")
	defer span.End()

	dataKey := resolveDataKey(key)

	writeAction, err := resolveWriteAction(action)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid action")
		return err
	}

	span.SetAttributes(
		attribute.String("vault.mount_path", c.mountPath),
		attribute.String("vault.path", path),
		attribute.String("vault.key", dataKey),
		attribute.String("vault.action", writeAction),
	)

	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)

	var writeErr error
	switch writeAction {
	case WriteActionReplace:
		writeErr = c.writeSecretData(ctx, fullPath, map[string]interface{}{
			dataKey: token,
		}, nil)
	case WriteActionAppend:
		writeErr = c.writeTokenAppend(ctx, fullPath, path, dataKey, token)
	default:
		// Unreachable when resolveWriteAction is used; keep for exhaustiveness.
		writeErr = fmt.Errorf("unsupported write action %q", writeAction)
	}

	observability.RecordVaultWrite(ctx, writeAction, writeErr == nil, time.Since(start))
	if writeErr != nil {
		span.RecordError(writeErr)
		span.SetStatus(codes.Error, "vault write failed")
		// path label matches existing storage-error series; action is bounded.
		observability.RecordVaultStorageError(ctx, path, writeAction)
		return writeErr
	}

	span.SetStatus(codes.Ok, "ok")
	return nil
}

// writeTokenAppend merges token into an existing KV v2 secret using CAS retries.
// configPath is the mount-relative path for logs (not the full /data/ API path).
func (c *Client) writeTokenAppend(ctx context.Context, fullPath, configPath, dataKey, token string) error {
	logger := observability.GetLogger()
	var lastErr error

	for attempt := 0; attempt < maxAppendCASAttempts; attempt++ {
		snap, err := c.readSecretSnapshot(ctx, fullPath)
		if err != nil {
			return err
		}

		secretData := make(map[string]interface{}, len(snap.data)+1)
		for k, v := range snap.data {
			secretData[k] = v
		}
		secretData[dataKey] = token

		var cas *int
		if snap.exists {
			v := snap.version
			cas = &v
		} else {
			// cas=0: only create if the secret does not already exist
			zero := 0
			cas = &zero
		}

		err = c.writeSecretData(ctx, fullPath, secretData, cas)
		if err == nil {
			if attempt > 0 {
				attrs := append([]any{
					slog.String("vault_path", configPath),
					slog.String("vault_key", dataKey),
					slog.Int("cas_attempts", attempt+1),
					slog.Int("cas_version", snap.version),
				}, observability.TraceAttrs(ctx)...)
				logger.InfoContext(ctx, "Vault append succeeded after CAS retry", attrs...)
			}
			return nil
		}
		if !isCASConflict(err) {
			return err
		}

		observability.RecordVaultAppendCASConflict(ctx)
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			span.AddEvent("vault.append.cas_conflict",
				trace.WithAttributes(
					attribute.Int("attempt", attempt+1),
					attribute.Int("cas_version", snap.version),
				),
			)
		}

		attrs := append([]any{
			slog.String("vault_path", configPath),
			slog.String("vault_key", dataKey),
			slog.Int("attempt", attempt+1),
			slog.Int("max_attempts", maxAppendCASAttempts),
			slog.Int("cas_version", snap.version),
			slog.Any("error", err),
		}, observability.TraceAttrs(ctx)...)
		logger.WarnContext(ctx, "Vault append CAS conflict; retrying", attrs...)

		lastErr = err
	}

	return fmt.Errorf("failed to append token after %d CAS attempts: %w", maxAppendCASAttempts, lastErr)
}

// writeSecretData writes a KV v2 data map. When cas is non-nil, sets options.cas.
func (c *Client) writeSecretData(ctx context.Context, fullPath string, secretData map[string]interface{}, cas *int) error {
	payload := map[string]interface{}{
		"data": secretData,
	}
	if cas != nil {
		payload["options"] = map[string]interface{}{
			"cas": *cas,
		}
	}

	_, err := c.client.Logical().WriteWithContext(ctx, fullPath, payload)
	if err != nil {
		return fmt.Errorf("failed to write token to vault: %w", err)
	}
	return nil
}

// secretSnapshot is the current KV v2 data map and version for CAS writes.
type secretSnapshot struct {
	data    map[string]interface{}
	version int
	exists  bool
}

// readSecretSnapshot returns the current secret data and version, or exists=false if missing.
func (c *Client) readSecretSnapshot(ctx context.Context, fullPath string) (*secretSnapshot, error) {
	secret, err := c.client.Logical().ReadWithContext(ctx, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read vault secret for append: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return &secretSnapshot{exists: false, version: 0, data: map[string]interface{}{}}, nil
	}

	// KV v2 read response: { "data": { ...keys }, "metadata": { "version": N, ... } }
	// When the secret is missing, Logical.Read typically returns nil, nil.
	rawData, hasData := secret.Data["data"]
	if !hasData || rawData == nil {
		return &secretSnapshot{exists: false, version: 0, data: map[string]interface{}{}}, nil
	}

	data, ok := rawData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid vault secret data structure at %s", fullPath)
	}

	version := 0
	if meta, ok := secret.Data["metadata"].(map[string]interface{}); ok {
		version = parseMetadataVersion(meta["version"])
	}

	// Copy so we do not mutate the map owned by the API response.
	out := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		out[k] = v
	}

	return &secretSnapshot{
		data:    out,
		version: version,
		exists:  true,
	}, nil
}

// parseMetadataVersion normalizes Vault metadata version values from JSON decode.
func parseMetadataVersion(v interface{}) int {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

// isCASConflict reports whether err looks like a KV v2 check-and-set version
// mismatch (safe to re-read and retry). It does not treat "cas required" as a
// conflict: that means CAS was omitted when the mount requires it, and retrying
// the same request will not help (append always sends options.cas).
func isCASConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Vault KV v2 version mismatch, e.g.:
	//   "check-and-set parameter did not match the current version"
	return strings.Contains(msg, "check-and-set") ||
		strings.Contains(msg, "did not match the current version")
}

// ReadToken reads a token value from a KV v2 path using the given data key.
// If key is empty, the value is read from "token" (backward compatible).
// Path is relative to mount_path (same rules as WriteToken).
func (c *Client) ReadToken(ctx context.Context, path string, key string) (string, error) {
	fullPath := fmt.Sprintf("%s/data/%s", c.mountPath, path)
	dataKey := resolveDataKey(key)

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

	tokenValue, ok := data[dataKey].(string)
	if !ok {
		return "", fmt.Errorf("token value not found at path %s key %q", path, dataKey)
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
