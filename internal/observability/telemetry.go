package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	logger   *slog.Logger
	logLevel *slog.LevelVar = &slog.LevelVar{}
)

// Config holds observability configuration
type Config struct {
	ServiceName  string
	OTelEndpoint string
	Enabled      bool
	LogLevel     string
}

// Metrics holds all the metric instruments
type Metrics struct {
	TokensTotal             metric.Int64Gauge
	RotationsTotal          metric.Int64Counter
	RotationDuration        metric.Float64Histogram
	TokenValidityRemaining  metric.Float64Gauge
	VaultStorageErrorsTotal metric.Int64Counter
}

var (
	globalMetrics *Metrics
	tracer        trace.Tracer
)

// Setup initializes OpenTelemetry with both tracing and metrics
func Setup(ctx context.Context, cfg *Config) (func(), error) {
	// Initialize our long-term logger (since a default may already exist from prior to calling this)
	handlerOpts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}

	// TODO: Support different log formats
	SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, handlerOpts)))
	logLevel.Set(ParseLogLevel(cfg.LogLevel))

	if !cfg.Enabled || cfg.OTelEndpoint == "" {
		logger.InfoContext(ctx, "OpenTelemetry disabled or no endpoint configured")
		return func() {}, nil
	}

	logger.InfoContext(ctx, "Initializing OpenTelemetry",
		slog.String("endpoint", cfg.OTelEndpoint),
		slog.String("service", cfg.ServiceName))

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Initialize tracing
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTelEndpoint),
		otlptracegrpc.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(traceProvider)
	tracer = otel.Tracer(cfg.ServiceName)

	// Initialize metrics
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.OTelEndpoint),
		otlpmetricgrpc.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)
	meter := otel.Meter(cfg.ServiceName)

	// Create metric instruments
	globalMetrics, err = createMetrics(meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	logger.InfoContext(ctx, "OpenTelemetry initialized successfully")

	// Return cleanup function
	cleanup := func() {
		logger.Info("Shutting down OpenTelemetry")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := traceProvider.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "Error shutting down trace provider", slog.Any("error", err))
		}

		if err := meterProvider.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "Error shutting down meter provider", slog.Any("error", err))
		}
	}

	return cleanup, nil
}

// createMetrics creates all metric instruments
func createMetrics(meter metric.Meter) (*Metrics, error) {
	tokensTotal, err := meter.Int64Gauge(
		"latr_tokens_total",
		metric.WithDescription("Total number of configured tokens"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tokens_total counter: %w", err)
	}

	rotationsTotal, err := meter.Int64Counter(
		"latr_rotations_total",
		metric.WithDescription("Total number of rotation attempts"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rotations_total counter: %w", err)
	}

	rotationDuration, err := meter.Float64Histogram(
		"latr_rotation_duration_seconds",
		metric.WithDescription("Duration of rotation operations in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rotation_duration histogram: %w", err)
	}

	tokenValidityRemaining, err := meter.Float64Gauge(
		"latr_token_validity_remaining_seconds",
		metric.WithDescription("Time remaining until token rotation is needed"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create token_validity_remaining gauge: %w", err)
	}

	vaultStorageErrorsTotal, err := meter.Int64Counter(
		"latr_vault_storage_errors_total",
		metric.WithDescription("Total number of Vault storage errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault_storage_errors_total counter: %w", err)
	}

	return &Metrics{
		TokensTotal:             tokensTotal,
		RotationsTotal:          rotationsTotal,
		RotationDuration:        rotationDuration,
		TokenValidityRemaining:  tokenValidityRemaining,
		VaultStorageErrorsTotal: vaultStorageErrorsTotal,
	}, nil
}

// GetTracer returns a tracer for creating spans
func GetTracer() trace.Tracer {
	if tracer == nil {
		// Return a no-op tracer if telemetry is not initialized
		return otel.Tracer("noop")
	}
	return tracer
}

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	return globalMetrics
}

// RecordTokenCount records the total number of configured tokens
func RecordTokenCount(ctx context.Context, count int64) {
	if globalMetrics == nil {
		return
	}
	globalMetrics.TokensTotal.Record(ctx, count)
}

// tokenMetricAttrs returns attributes for per-token metrics that already use label.
// team is included so alerts can be routed to the owning team (same value as config team).
func tokenMetricAttrs(label, team string, extra ...attribute.KeyValue) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2+len(extra))
	attrs = append(attrs,
		attribute.String("label", label),
		attribute.String("team", team),
	)
	attrs = append(attrs, extra...)
	return attrs
}

// RecordRotation records a rotation attempt with success/failure status
func RecordRotation(ctx context.Context, label, team string, success bool) {
	if globalMetrics == nil {
		return
	}
	status := "success"
	if !success {
		status = "failure"
	}
	globalMetrics.RotationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			tokenMetricAttrs(label, team, attribute.String("status", status))...,
		),
	)
}

// RecordRotationDuration records the duration of a rotation operation
func RecordRotationDuration(ctx context.Context, label, team string, duration time.Duration) {
	if globalMetrics == nil {
		return
	}
	globalMetrics.RotationDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(tokenMetricAttrs(label, team)...),
	)
}

// RecordTokenValidityRemaining records the time remaining until rotation is needed
func RecordTokenValidityRemaining(ctx context.Context, label, team string, seconds float64) {
	if globalMetrics == nil {
		return
	}
	globalMetrics.TokenValidityRemaining.Record(ctx, seconds,
		metric.WithAttributes(tokenMetricAttrs(label, team)...),
	)
}

// RecordVaultStorageError records a Vault storage error
func RecordVaultStorageError(ctx context.Context, path string) {
	if globalMetrics == nil {
		return
	}
	globalMetrics.VaultStorageErrorsTotal.Add(ctx, 1,
		metric.WithAttributes(attribute.String("path", path)),
	)
}

// TraceAttrs extracts OpenTelemetry trace context attributes for structured logging
// Returns attributes as []any for use with slog methods
func TraceAttrs(ctx context.Context) []any {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return nil
	}

	return []any{
		slog.String("trace_id", spanCtx.TraceID().String()),
		slog.String("span_id", spanCtx.SpanID().String()),
	}
}

// SetLogger sets the global logger instance
func SetLogger(l *slog.Logger) {
	logger = l
	slog.SetDefault(l)
}

func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		slog.Warn("Invalid log level provided. Defaulting to INFO", "level", level)
		return slog.LevelInfo
	}
}

// GetLogger returns the global logger instance
func GetLogger() *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger
}
