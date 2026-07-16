package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/linode-obs/latr/internal/config"
	"github.com/linode-obs/latr/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Engine defines the interface for the rotation engine
type Engine interface {
	ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error
}

// Scheduler manages the execution schedule for token rotation
type Scheduler struct {
	config *config.Config
	engine Engine
}

// NewScheduler creates a new scheduler
func NewScheduler(cfg *config.Config, engine Engine) *Scheduler {
	return &Scheduler{
		config: cfg,
		engine: engine,
	}
}

// Run starts the scheduler based on the configured mode
func (s *Scheduler) Run(ctx context.Context) error {
	if s.config.Daemon.Mode == "one-shot" {
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
	attrs := append([]any{slog.String("check_interval", s.config.Daemon.CheckInterval)}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Running in daemon mode", attrs...)

	interval, err := time.ParseDuration(s.config.Daemon.CheckInterval)
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

// executeCycle processes all configured tokens
func (s *Scheduler) executeCycle(ctx context.Context) error {
	logger := observability.GetLogger()

	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "ExecuteRotationCycle")
	defer span.End()

	tokenCount := int64(len(s.config.Tokens))
	span.SetAttributes(attribute.Int64("tokens.count", tokenCount))

	attrs := append([]any{slog.Int64("token_count", tokenCount)}, observability.TraceAttrs(ctx)...)
	logger.InfoContext(ctx, "Starting rotation cycle", attrs...)

	// Record total configured tokens
	observability.RecordTokenCount(ctx, tokenCount)

	if tokenCount == 0 {
		logger.InfoContext(ctx, "No tokens configured", observability.TraceAttrs(ctx)...)
		span.SetStatus(codes.Ok, "no tokens configured")
		return nil
	}

	// Process each token
	for _, tokenConfig := range s.config.Tokens {
		// Determine threshold (use token-specific if set, otherwise global)
		threshold := s.config.Rotation.ThresholdPercent
		if tokenConfig.RotationThreshold > 0 {
			threshold = tokenConfig.RotationThreshold
		}

		if err := s.engine.ProcessToken(ctx, tokenConfig, threshold); err != nil {
			attrs := append([]any{
				slog.String("token_label", tokenConfig.Label),
				slog.Any("error", err),
			}, observability.TraceAttrs(ctx)...)
			logger.ErrorContext(ctx, "Failed to process token", attrs...)
			// Continue processing other tokens
		}
	}

	logger.InfoContext(ctx, "Rotation cycle completed", observability.TraceAttrs(ctx)...)
	span.SetStatus(codes.Ok, "rotation cycle completed")
	return nil
}
