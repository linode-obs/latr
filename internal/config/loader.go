package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load reads and parses a single configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	return cfg, nil
}

// LoadAll loads one or more configuration files matching a path or glob pattern.
// Each file is loaded independently — accounts are not merged across files.
func LoadAll(pathOrPattern string) ([]*Config, error) {
	var paths []string

	if containsGlobChar(pathOrPattern) {
		matches, err := filepath.Glob(pathOrPattern)
		if err != nil {
			return nil, fmt.Errorf("failed to glob pattern %s: %w", pathOrPattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no config files found matching pattern: %s", pathOrPattern)
		}
		paths = matches
	} else {
		paths = []string{pathOrPattern}
	}

	configs := make([]*Config, 0, len(paths))
	for _, path := range paths {
		cfg, err := Load(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file %s: %w", path, err)
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

// LoadAndValidate loads configuration file(s), applies defaults, and validates.
// Returns a slice of configs — one per file. Global settings (daemon, rotation,
// vault, observability) from the first file are propagated to subsequent files
// that don't specify them.
func LoadAndValidate(pathOrPattern string) ([]*Config, error) {
	configs, err := LoadAll(pathOrPattern)
	if err != nil {
		return nil, err
	}

	// Find the global config (if any) and use it as the source for defaults.
	// If no config is marked global, the first config serves as the default source.
	var globalCfg *Config
	for _, cfg := range configs {
		if cfg.IsGlobal() {
			if globalCfg != nil {
				return nil, fmt.Errorf("multiple configs marked as global: true")
			}
			globalCfg = cfg
		}
	}
	if globalCfg == nil {
		globalCfg = configs[0]
	}

	globalCfg.ApplyDefaults()

	for _, cfg := range configs {
		if cfg != globalCfg {
			propagateGlobals(globalCfg, cfg)
			cfg.ApplyDefaults()
		}
	}

	// Validate all configs
	for i, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			if len(configs) > 1 {
				return nil, fmt.Errorf("config file %d: %w", i+1, err)
			}
			return nil, fmt.Errorf("config validation failed: %w", err)
		}
	}

	// Validate account label uniqueness and ensure at least one account exists
	seenLabels := make(map[string]int)
	for i, cfg := range configs {
		if cfg.IsGlobal() || cfg.Account.Label == "" {
			continue
		}
		if prev, exists := seenLabels[cfg.Account.Label]; exists {
			return nil, fmt.Errorf("duplicate account label %q in config files %d and %d", cfg.Account.Label, prev, i+1)
		}
		seenLabels[cfg.Account.Label] = i + 1
	}
	if len(seenLabels) == 0 {
		return nil, fmt.Errorf("at least one config file must have an account block")
	}

	return configs, nil
}

// propagateGlobals copies global settings from the primary config to a
// secondary config for any fields the secondary config doesn't set.
func propagateGlobals(primary, secondary *Config) {
	// Daemon
	if secondary.Daemon.Mode == "" {
		secondary.Daemon.Mode = primary.Daemon.Mode
	}
	if secondary.Daemon.CheckInterval == "" {
		secondary.Daemon.CheckInterval = primary.Daemon.CheckInterval
	}
	if !secondary.Daemon.DryRun && primary.Daemon.DryRun {
		secondary.Daemon.DryRun = primary.Daemon.DryRun
	}

	// Rotation
	if secondary.Rotation.ThresholdPercent == 0 {
		secondary.Rotation.ThresholdPercent = primary.Rotation.ThresholdPercent
	}

	// Vault
	if secondary.Vault.Address == "" {
		secondary.Vault.Address = primary.Vault.Address
	}
	if secondary.Vault.RoleID == "" {
		secondary.Vault.RoleID = primary.Vault.RoleID
	}
	if secondary.Vault.SecretID == "" {
		secondary.Vault.SecretID = primary.Vault.SecretID
	}
	if secondary.Vault.MountPath == "" {
		secondary.Vault.MountPath = primary.Vault.MountPath
	}

	// Observability
	if secondary.Observability.OTelEndpoint == "" {
		secondary.Observability.OTelEndpoint = primary.Observability.OTelEndpoint
	}
	if secondary.Observability.LogLevel == "" {
		secondary.Observability.LogLevel = primary.Observability.LogLevel
	}
}

// containsGlobChar checks if a path contains glob characters
func containsGlobChar(path string) bool {
	for _, ch := range path {
		if ch == '*' || ch == '?' || ch == '[' {
			return true
		}
	}
	return false
}
