package rotation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/internal/observability"
	"github.com/wbh1/latr/pkg/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// LinodeClient defines the interface for Linode API operations
type LinodeClient interface {
	CreateToken(ctx context.Context, label, scopes string, expiry time.Time) (*models.Token, error)
	FindTokenByLabel(ctx context.Context, label string) ([]*models.Token, error)
}

// VaultClient defines the interface for Vault operations
type VaultClient interface {
	WriteToken(ctx context.Context, path, key, token string) error
	WriteTokenState(ctx context.Context, path string, state *models.TokenState) error
	ReadTokenState(ctx context.Context, path string) (*models.TokenState, error)
}

// Engine handles token rotation logic
type Engine struct {
	linodeClient LinodeClient
	vaultClient  VaultClient
	dryRun       bool
}

// statePath returns the Vault path used for token state tracking (custom_metadata).
// When a non-default storage key is used, the key is appended to the path to
// avoid metadata collisions between tokens that share a Vault path but use
// different keys (e.g., account.token using account.label as key vs a regular
// token using the default "token" key on the same path).
func statePath(storage config.StorageConfig) string {
	if storage.Key == "" || storage.Key == "token" {
		return storage.Path
	}
	return storage.Path + "/" + storage.Key
}

// NewEngine creates a new rotation engine
func NewEngine(linodeClient LinodeClient, vaultClient VaultClient, dryRun bool) *Engine {
	return &Engine{
		linodeClient: linodeClient,
		vaultClient:  vaultClient,
		dryRun:       dryRun,
	}
}

// ProcessToken processes a single token configuration
func (e *Engine) ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error {
	logger := observability.GetLogger()

	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "ProcessToken")
	defer span.End()

	span.SetAttributes(
		attribute.String("token.label", tokenConfig.Label),
		attribute.String("token.team", tokenConfig.Team),
	)

	attrs := append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.String("team", tokenConfig.Team),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Processing token", attrs...)

	// Parse validity duration
	validity, err := config.ParseValidityDuration(tokenConfig.Validity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid validity")
		return fmt.Errorf("invalid validity for token %s: %w", tokenConfig.Label, err)
	}

	// Check if token exists in Linode
	existingTokens, err := e.linodeClient.FindTokenByLabel(ctx, tokenConfig.Label)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to find token")
		return fmt.Errorf("failed to find token %s: %w", tokenConfig.Label, err)
	}

	if existingTokens == nil {
		// Token doesn't exist, create it
		return e.createNewToken(ctx, tokenConfig, validity)
	}

	var existingToken *models.Token
	for _, t := range existingTokens {
		// If more than one token exists with the same label, we only want to find the newest one.
		// When more than one token exists with the same label, we assume that the old one(s) already rotated
		// and just hasn't aged out yet.
		if existingToken == nil || t.CreatedAt.After(existingToken.CreatedAt) {
			existingToken = t
		}

	}
	// Token exists, check if it needs rotation
	existingToken.Validity = validity

	// Record token validity remaining metric
	validityRemaining := time.Until(existingToken.ExpiresAt).Seconds()
	observability.RecordTokenValidityRemaining(ctx, tokenConfig.Label, validityRemaining)
	span.SetAttributes(attribute.Float64("token.validity_remaining_seconds", validityRemaining))

	if existingToken.NeedsRotation(thresholdPercent) {
		attrs := append([]any{
			slog.String("token_label", tokenConfig.Label),
			slog.Float64("validity_remaining_percent", existingToken.PercentValidityRemaining()),
		}, observability.TraceAttrs(ctx)...)
		logger.InfoContext(ctx, "Token needs rotation", attrs...)
		return e.rotateToken(ctx, tokenConfig, existingToken, validity)
	}

	attrs = append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.Float64("validity_remaining_percent", existingToken.PercentValidityRemaining()),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Token does not need rotation", attrs...)
	span.SetStatus(codes.Ok, "no rotation needed")
	return nil
}

// createNewToken creates a new token that doesn't exist yet
func (e *Engine) createNewToken(ctx context.Context, tokenConfig config.TokenConfig, validity time.Duration) error {
	logger := observability.GetLogger()

	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "CreateNewToken")
	defer span.End()

	span.SetAttributes(attribute.String("token.label", tokenConfig.Label))

	attrs := append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.Bool("dry_run", e.dryRun),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Creating new token", attrs...)
	startTime := time.Now()

	if e.dryRun {
		logger.InfoContext(ctx, "DRY RUN: Would create new token",
			append([]any{slog.String("token_label", tokenConfig.Label)}, observability.TraceAttrs(ctx)...)...)
		span.SetStatus(codes.Ok, "dry run")
		return nil
	}

	// Read existing state (if any)
	storagePath := statePath(tokenConfig.Storage[0])
	existingState, err := e.vaultClient.ReadTokenState(ctx, storagePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to read token state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		return fmt.Errorf("failed to read token state: %w", err)
	}

	// Calculate expiry
	expiry := time.Now().Add(validity)

	// Create token in Linode
	newToken, err := e.linodeClient.CreateToken(ctx, tokenConfig.Label, tokenConfig.Scopes, expiry)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to create token %s in Linode: %w", tokenConfig.Label, err)
	}

	attrs = append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.Int("token_id", newToken.ID),
		slog.Time("expires_at", newToken.ExpiresAt),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Created token", attrs...)

	// Store token in all configured storage backends
	if err := e.storeTokenInBackends(ctx, tokenConfig.Storage, newToken.Token); err != nil {
		// Track state even if storage fails, so we can retry on next run
		_ = e.updateState(ctx, storagePath, newToken, existingState, expiry)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		observability.RecordVaultStorageError(ctx, storagePath)
		return fmt.Errorf("failed to store token in vault: %w", err)
	}

	// Update state
	if err := e.updateState(ctx, storagePath, newToken, existingState, expiry); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to update state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to update token state: %w", err)
	}

	// Record successful rotation
	span.SetStatus(codes.Ok, "token created successfully")
	observability.RecordRotation(ctx, tokenConfig.Label, true)
	observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))

	return nil
}

// rotateToken rotates an existing token
func (e *Engine) rotateToken(ctx context.Context, tokenConfig config.TokenConfig, existingToken *models.Token, validity time.Duration) error {
	logger := observability.GetLogger()

	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "RotateToken")
	defer span.End()

	span.SetAttributes(
		attribute.String("token.label", tokenConfig.Label),
		attribute.Int("token.existing_id", existingToken.ID),
	)

	attrs := append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.Int("existing_token_id", existingToken.ID),
		slog.Bool("dry_run", e.dryRun),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Rotating token", attrs...)
	startTime := time.Now()

	if e.dryRun {
		logger.InfoContext(ctx, "DRY RUN: Would rotate token",
			append([]any{slog.String("token_label", tokenConfig.Label)}, observability.TraceAttrs(ctx)...)...)
		span.SetStatus(codes.Ok, "dry run")
		return nil
	}

	// Read existing state
	storagePath := statePath(tokenConfig.Storage[0])
	existingState, err := e.vaultClient.ReadTokenState(ctx, storagePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to read token state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		return fmt.Errorf("failed to read token state: %w", err)
	}

	// Calculate new expiry
	newExpiry := time.Now().Add(validity)

	// Create new token in Linode
	newToken, err := e.linodeClient.CreateToken(ctx, tokenConfig.Label, tokenConfig.Scopes, newExpiry)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to create token %s in Linode: %w", tokenConfig.Label, err)
	}

	span.SetAttributes(attribute.Int("token.new_id", newToken.ID))

	attrs = append([]any{
		slog.String("token_label", tokenConfig.Label),
		slog.Int("new_token_id", newToken.ID),
		slog.Time("new_expires_at", newToken.ExpiresAt),
		slog.Int("previous_token_id", existingToken.ID),
		slog.Time("previous_expires_at", existingToken.ExpiresAt),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Created new token during rotation", attrs...)

	// Store new token in all configured storage backends
	if err := e.storeTokenInBackends(ctx, tokenConfig.Storage, newToken.Token); err != nil {
		// Track state even if storage fails
		_ = e.updateStateAfterRotation(ctx, storagePath, newToken, existingToken, existingState)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		observability.RecordVaultStorageError(ctx, storagePath)
		return fmt.Errorf("failed to store token in vault: %w", err)
	}

	// Update state with previous token info
	if err := e.updateStateAfterRotation(ctx, storagePath, newToken, existingToken, existingState); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to update state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to update token state: %w", err)
	}

	// Record successful rotation
	span.SetStatus(codes.Ok, "token rotated successfully")
	observability.RecordRotation(ctx, tokenConfig.Label, true)
	observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))

	return nil
}

// storeTokenInBackends stores the token in all configured storage backends
func (e *Engine) storeTokenInBackends(ctx context.Context, storageConfigs []config.StorageConfig, token string) error {
	logger := observability.GetLogger()

	for _, storage := range storageConfigs {
		switch storage.Type {
		case "vault":
			if err := e.vaultClient.WriteToken(ctx, storage.Path, storage.Key, token); err != nil {
				return err
			}
			attrs := append([]any{
				slog.String("storage_type", "vault"),
				slog.String("vault_path", storage.Path),
			}, observability.TraceAttrs(ctx)...)
			logger.InfoContext(ctx, "Stored token in Vault", attrs...)
		default:
			return fmt.Errorf("unsupported storage type: %s", storage.Type)
		}
	}
	return nil
}

// updateState updates the token state after creation
func (e *Engine) updateState(ctx context.Context, path string, newToken *models.Token, existingState *models.TokenState, expiry time.Time) error {
	rotationCount := 0
	if existingState != nil {
		rotationCount = existingState.RotationCount
	}

	state := &models.TokenState{
		Label:             newToken.Label,
		CurrentLinodeID:   newToken.ID,
		CurrentTokenValue: newToken.Token,
		LastRotatedAt:     time.Now(),
		PreviousLinodeID:  0,
		PreviousExpiresAt: time.Time{},
		RotationCount:     rotationCount,
	}

	return e.vaultClient.WriteTokenState(ctx, path, state)
}

// updateStateAfterRotation updates the token state after rotation
func (e *Engine) updateStateAfterRotation(ctx context.Context, path string, newToken, oldToken *models.Token, existingState *models.TokenState) error {
	rotationCount := 0
	if existingState != nil {
		rotationCount = existingState.RotationCount
	}

	state := &models.TokenState{
		Label:             newToken.Label,
		CurrentLinodeID:   newToken.ID,
		CurrentTokenValue: newToken.Token,
		LastRotatedAt:     time.Now(),
		PreviousLinodeID:  oldToken.ID,
		PreviousExpiresAt: oldToken.ExpiresAt,
		RotationCount:     rotationCount + 1,
	}

	return e.vaultClient.WriteTokenState(ctx, path, state)
}
