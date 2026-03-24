package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/internal/linode"
	"github.com/wbh1/latr/internal/observability"
	"github.com/wbh1/latr/internal/rotation"
	"github.com/wbh1/latr/internal/scheduler"
	"github.com/wbh1/latr/internal/vault"
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

	// Load and validate configuration(s)
	logger.Info("Loading configuration", slog.String("path", *configPath))
	configs, err := config.LoadAndValidate(*configPath)
	if err != nil {
		logger.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Use global settings from the global config, falling back to the first config
	var primaryCfg *config.Config
	for _, cfg := range configs {
		if cfg.IsGlobal() {
			primaryCfg = cfg
			break
		}
	}
	if primaryCfg == nil {
		primaryCfg = configs[0]
	}

	// Daemon and observability settings are global-only — enforce by overwriting
	// on all non-primary configs. We don't warn here because ApplyDefaults()
	// populates these fields before we can distinguish "explicitly set" from
	// "defaulted", which would cause misleading warnings.
	for _, cfg := range configs {
		if cfg != primaryCfg {
			cfg.Daemon = primaryCfg.Daemon
			cfg.Observability = primaryCfg.Observability
		}
	}

	// Log each loaded configuration
	var totalTokens int
	for i, cfg := range configs {
		tokenCount := len(cfg.AllTokens())
		totalTokens += tokenCount
		logger.Info("Configuration loaded",
			slog.Int("config", i+1),
			slog.String("account_label", cfg.Account.Label),
			slog.Int("token_count", tokenCount))
	}

	// Log summary if multiple configs
	if len(configs) > 1 {
		accountCount := 0
		for _, cfg := range configs {
			if !cfg.IsGlobal() {
				accountCount++
			}
		}
		logger.Info("All configurations loaded",
			slog.Int("account_count", accountCount),
			slog.Int("total_token_count", totalTokens),
			slog.String("mode", primaryCfg.Daemon.Mode),
			slog.Int("rotation_threshold_percent", primaryCfg.Rotation.ThresholdPercent),
			slog.Bool("dry_run", primaryCfg.Daemon.DryRun))
	}

	if err := run(primaryCfg, configs); err != nil {
		logger.Error("Fatal error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run handles telemetry setup, client initialization, and scheduling. Extracted
// from main so that deferred cleanup functions (telemetry flush, context cancel)
// execute on all exit paths.
func run(primaryCfg *config.Config, configs []*config.Config) error {
	logger := observability.GetLogger()

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry
	telemetryConfig := &observability.Config{
		ServiceName:  "latr",
		OTelEndpoint: primaryCfg.Observability.OTelEndpoint,
		Enabled:      primaryCfg.Observability.OTelEndpoint != "",
		LogLevel:     primaryCfg.Observability.LogLevel,
	}

	telemetryCleanup, err := observability.Setup(ctx, telemetryConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	logger = observability.GetLogger()
	defer telemetryCleanup()

	// Create per-account entries (each account gets its own Vault + Linode client)
	accounts := make([]scheduler.AccountEntry, 0, len(configs))
	for _, cfg := range configs {
		// Skip globals-only configs (no account, just defaults)
		if cfg.Account.Label == "" {
			continue
		}

		acctLabel := cfg.Account.Label

		// Resolve Vault config: account.vault overrides global vault, then env var fallback
		resolvedVault := cfg.Account.Vault.Resolved(cfg.Vault)

		if resolvedVault.RoleID == "" {
			resolvedVault.RoleID = os.Getenv("VAULT_ROLE_ID")
		}
		if resolvedVault.RoleID == "" {
			return fmt.Errorf("account %q: vault role_id not set in config or VAULT_ROLE_ID env var", acctLabel)
		}

		if resolvedVault.SecretID == "" {
			resolvedVault.SecretID = os.Getenv("VAULT_SECRET_ID")
		}
		if resolvedVault.SecretID == "" {
			return fmt.Errorf("account %q: vault secret_id not set in config or VAULT_SECRET_ID env var", acctLabel)
		}

		vaultConfig := &vault.Config{
			Address:   resolvedVault.Address,
			RoleID:    resolvedVault.RoleID,
			SecretID:  resolvedVault.SecretID,
			MountPath: resolvedVault.MountPath,
		}

		vaultClient, err := vault.NewClient(vaultConfig)
		if err != nil {
			return fmt.Errorf("account %q: failed to create Vault client (address: %s): %w", acctLabel, resolvedVault.Address, err)
		}
		logger.InfoContext(ctx, "Vault client initialized for account",
			slog.String("account_label", acctLabel),
			slog.String("vault_address", resolvedVault.Address))

		// Resolve Linode API token: account.token.storage first, then env var fallback
		var linodeToken string
		if cfg.Account.Token != nil && cfg.Account.Token.HasStorage() {
			for _, s := range cfg.Account.Token.Storage {
				if s.Type == "vault" {
					vaultKey := acctLabel
					if s.Key != "" {
						vaultKey = s.Key
					}
					logger.InfoContext(ctx, "Reading Linode token from Vault",
						slog.String("account_label", acctLabel),
						slog.String("vault_path", s.Path),
						slog.String("vault_key", vaultKey))

					linodeToken, err = vaultClient.ReadSecretKey(ctx, s.Path, vaultKey)
					if err != nil {
						return fmt.Errorf("account %q: failed to read Linode token from Vault (path: %s): %w", acctLabel, s.Path, err)
					}
					break
				}
			}
		}
		if linodeToken == "" {
			linodeToken = os.Getenv("LINODE_TOKEN")
		}
		if linodeToken == "" {
			return fmt.Errorf("account %q: linode token not available from account.token.storage or LINODE_TOKEN env var", acctLabel)
		}

		// Resolve Linode API URL: config api_url takes precedence, then LINODE_API_URL env var
		linodeAPIURL := cfg.Account.APIURL
		if linodeAPIURL == "" {
			linodeAPIURL = os.Getenv("LINODE_API_URL")
		}

		linodeClient := linode.NewClient(linodeToken, linodeAPIURL)
		engine := rotation.NewEngine(linodeClient, vaultClient, primaryCfg.Daemon.DryRun)

		tokens := cfg.AllTokens()

		logger.InfoContext(ctx, "Initialized account",
			slog.String("account_label", cfg.Account.Label),
			slog.String("account_team", cfg.Account.Team),
			slog.String("api_url", linodeAPIURL),
			slog.Int("token_count", len(tokens)))

		accounts = append(accounts, scheduler.AccountEntry{
			Account:  cfg.Account,
			Tokens:   tokens,
			Engine:   engine,
			Rotation: cfg.Rotation,
		})
	}

	// Create scheduler
	sched := scheduler.NewScheduler(primaryCfg.Daemon, accounts)

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
			return nil
		}
		return fmt.Errorf("scheduler error: %w", err)
	}

	logger.Info("latr finished successfully")
	return nil
}
