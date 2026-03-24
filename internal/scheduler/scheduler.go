package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Engine defines the interface for the rotation engine
type Engine interface {
	ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error
}

// AccountEntry represents an account with its associated engine and tokens
type AccountEntry struct {
	Account  config.AccountConfig
	Tokens   []config.TokenConfig
	Engine   Engine
	Rotation config.RotationConfig
}

// Scheduler manages the execution schedule for token rotation
type Scheduler struct {
	daemon   config.DaemonConfig
	rotation config.RotationConfig
	accounts []AccountEntry
}

// NewScheduler creates a new scheduler
func NewScheduler(daemon config.DaemonConfig, rotation config.RotationConfig, accounts []AccountEntry) *Scheduler {
	return &Scheduler{
		daemon:   daemon,
		rotation: rotation,
		accounts: accounts,
	}
}

// Run starts the scheduler based on the configured mode
func (s *Scheduler) Run(ctx context.Context) error {
	if s.daemon.Mode == "one-shot" {
		return s.runOnce(ctx)
	}
	return s.runDaemon(ctx)
}

// runOnce executes a single rotation cycle
func (s *Scheduler) runOnce(ctx context.Context) error {
	logger := observability.GetLogger()
	attrs := observability.TraceAttrs(ctx)
	logger.InfoContext(ctx, "Running in one-shot mode", attrs...)
	return s.executeCycle(ctx)
}

// runDaemon runs the rotation cycle at regular intervals
func (s *Scheduler) runDaemon(ctx context.Context) error {
	logger := observability.GetLogger()
	attrs := append([]any{slog.String("check_interval", s.daemon.CheckInterval)}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Running in daemon mode", attrs...)

	interval, err := time.ParseDuration(s.daemon.CheckInterval)
	if err != nil {
		return fmt.Errorf("invalid check interval: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.executeCycle(ctx); err != nil {
		attrs := append([]any{slog.Any("error", err)}, observability.TraceAttrs(ctx)...)
		logger.ErrorContext(ctx, "Error in rotation cycle", attrs...)
	}

	// Then run at intervals
	for {
		select {
		case <-ctx.Done():
			attrs := append([]any{slog.Any("reason", ctx.Err())}, observability.TraceAttrs(ctx)...)
			logger.InfoContext(ctx, "Shutting down scheduler", attrs...)
			return ctx.Err()
		case <-ticker.C:
			if err := s.executeCycle(ctx); err != nil {
				attrs := append([]any{slog.Any("error", err)}, observability.TraceAttrs(ctx)...)
				logger.ErrorContext(ctx, "Error in rotation cycle", attrs...)
				// Continue running even if there's an error
			}
		}
	}
}

// executeCycle processes all tokens across all accounts
func (s *Scheduler) executeCycle(ctx context.Context) error {
	logger := observability.GetLogger()

	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "ExecuteRotationCycle")
	defer span.End()

	var totalTokens int64
	for _, acct := range s.accounts {
		totalTokens += int64(len(acct.Tokens))
	}
	span.SetAttributes(
		attribute.Int64("tokens.count", totalTokens),
		attribute.Int("accounts.count", len(s.accounts)),
	)

	attrs := append([]any{
		slog.Int64("token_count", totalTokens),
		slog.Int("account_count", len(s.accounts)),
	}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Starting rotation cycle", attrs...)

	// Record total configured tokens
	observability.RecordTokenCount(ctx, totalTokens)

	if totalTokens == 0 {
		logger.InfoContext(ctx, "No tokens configured", observability.TraceAttrs(ctx)...)
		span.SetStatus(codes.Ok, "no tokens configured")
		return nil
	}

	// Process each account's tokens
	for _, acct := range s.accounts {
		acctAttrs := append([]any{
			slog.String("account_label", acct.Account.Label),
			slog.String("account_team", acct.Account.Team),
			slog.Int("token_count", len(acct.Tokens)),
		}, observability.TraceAttrs(ctx)...)
		logger.InfoContext(ctx, "Processing account", acctAttrs...)

		for _, tokenConfig := range acct.Tokens {
			// Determine threshold (use token-specific if set, otherwise account-level)
			threshold := acct.Rotation.ThresholdPercent
			if tokenConfig.RotationThreshold > 0 {
				threshold = tokenConfig.RotationThreshold
			}

			if err := acct.Engine.ProcessToken(ctx, tokenConfig, threshold); err != nil {
				attrs := append([]any{
					slog.String("account_label", acct.Account.Label),
					slog.String("token_label", tokenConfig.Label),
					slog.Any("error", err),
				}, observability.TraceAttrs(ctx)...)
				logger.ErrorContext(ctx, "Failed to process token", attrs...)
				// Continue processing other tokens
			}
		}
	}

	logger.InfoContext(ctx, "Rotation cycle completed", observability.TraceAttrs(ctx)...)
	span.SetStatus(codes.Ok, "rotation cycle completed")
	return nil
}
