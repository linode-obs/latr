package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/linode-obs/latr/internal/config"
	"github.com/linode-obs/latr/internal/linode"
	"github.com/linode-obs/latr/internal/observability"
	"github.com/linode-obs/latr/internal/rotation"
	"github.com/linode-obs/latr/internal/scheduler"
	"github.com/linode-obs/latr/internal/vault"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Parse CLI flags
	configPath := flag.String("config", "", "Path to configuration file or glob pattern (required)")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("latr version %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	// This will be overriden when we setup telemetry after loading the config
	observability.SetLogger(logger)

	if *configPath == "" {
		logger.Error("Missing required flag", slog.String("flag", "config"))
		os.Exit(1)
	}

	// Load and validate configuration
	logger.Info("Loading configuration", slog.String("path", *configPath))
	cfg, err := config.LoadAndValidate(*configPath)
	if err != nil {
		logger.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("Configuration loaded successfully",
		slog.String("mode", cfg.Daemon.Mode),
		slog.Int("token_count", len(cfg.Tokens)),
		slog.Int("rotation_threshold_percent", cfg.Rotation.ThresholdPercent),
		slog.Bool("dry_run", cfg.Daemon.DryRun))

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OpenTelemetry
	telemetryConfig := &observability.Config{
		ServiceName:  "latr",
		OTelEndpoint: cfg.Observability.OTelEndpoint,
		Enabled:      cfg.Observability.OTelEndpoint != "",
		LogLevel:     cfg.Observability.LogLevel,
	}

	telemetryCleanup, err := observability.Setup(ctx, telemetryConfig)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to initialize telemetry", slog.Any("error", err))
		os.Exit(1)
	}
	logger = observability.GetLogger()
	defer telemetryCleanup()

	// Management PAT: file (hot-reload) preferred over static env. Token value is never logged.
	tokenProvider, tokenSource, err := linode.TokenProviderFromEnv(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to load Linode API token", slog.Any("error", err))
		os.Exit(1)
	}
	linodeClient := linode.NewClientWithTokenProvider(tokenProvider)
	logger.InfoContext(ctx, "Linode client initialized",
		slog.String("token_source", tokenSource))

	// Create Vault client
	vaultConfig := &vault.Config{
		Address:   cfg.Vault.Address,
		RoleID:    cfg.Vault.RoleID,
		SecretID:  cfg.Vault.SecretID,
		MountPath: cfg.Vault.MountPath,
	}

	vaultClient, err := vault.NewClient(vaultConfig)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create Vault client",
			slog.Any("error", err),
			slog.String("vault_address", cfg.Vault.Address))
		os.Exit(1)
	}
	logger.InfoContext(ctx, "Vault client initialized and authenticated",
		slog.String("vault_address", cfg.Vault.Address))

	// Create rotation engine
	engine := rotation.NewEngine(linodeClient, vaultClient, cfg.Daemon.DryRun)

	// Create scheduler
	sched := scheduler.NewScheduler(cfg, engine)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		logger.Info("Initiating graceful shutdown")
		cancel()
	}()

	// Run scheduler
	logger.InfoContext(ctx, "Starting latr",
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("build_date", date))
	if err := sched.Run(ctx); err != nil {
		if err == context.Canceled {
			logger.Info("Shutdown complete")
			os.Exit(0)
		}
		logger.Error("Scheduler error", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("latr finished successfully")
}
