package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProcessConfigFileName is the process-wide config file name for multi-file --dir layouts.
// Token files are any other *.yml / *.yaml in the same directory.
const ProcessConfigFileName = "_process.yaml"

// vaultCredentialPlaceholder is used by Check so Validate can run without live AppRole secrets.
// Config files typically use ${VAULT_ROLE_ID} / ${VAULT_SECRET_ID}; CI leaves those unset.
const vaultCredentialPlaceholder = "check-config-placeholder"

// LoadFiles loads and merges configuration files in order.
// Each path may be a literal file or a glob pattern. Non-empty process fields from
// later files override earlier ones; tokens are appended.
func LoadFiles(paths []string) (*Config, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no config files specified")
	}

	expanded, err := expandConfigPaths(paths)
	if err != nil {
		return nil, err
	}

	var merged *Config
	for _, path := range expanded {
		cfg, err := Load(path)
		if err != nil {
			return nil, err
		}
		if merged == nil {
			merged = cfg
			continue
		}
		merged = MergeConfigs(merged, cfg)
	}
	return merged, nil
}

// expandConfigPaths expands globs in order; literal paths are kept as-is.
func expandConfigPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if !containsGlobChar(path) {
			out = append(out, path)
			continue
		}
		matches, err := filepath.Glob(path)
		if err != nil {
			return nil, fmt.Errorf("failed to glob pattern %s: %w", path, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no config files found matching pattern: %s", path)
		}
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out, nil
}

// LoadDir loads a multi-file config directory: exactly one process file
// (_process.yaml) plus one or more other YAML token config files.
func LoadDir(dir string) (*Config, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var processPath string
	tokenPaths := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !isYAMLConfigFile(name) {
			continue
		}

		full := filepath.Join(dir, name)
		switch name {
		case ProcessConfigFileName, "_process.yml":
			if processPath != "" {
				return nil, fmt.Errorf(
					"directory %s: multiple process config files (%s and %s); keep a single %s",
					dir,
					filepath.Base(processPath),
					name,
					ProcessConfigFileName,
				)
			}
			processPath = full
		default:
			tokenPaths = append(tokenPaths, full)
		}
	}

	if processPath == "" {
		return nil, fmt.Errorf(
			"directory %s: missing process config %s",
			dir,
			ProcessConfigFileName,
		)
	}
	if len(tokenPaths) == 0 {
		return nil, fmt.Errorf(
			"directory %s: need at least one token config file (*.yml / *.yaml) besides %s",
			dir,
			ProcessConfigFileName,
		)
	}

	sort.Strings(tokenPaths)
	paths := make([]string, 0, 1+len(tokenPaths))
	paths = append(paths, processPath)
	paths = append(paths, tokenPaths...)
	return LoadFiles(paths)
}

// LoadProcessAndTokenConfigs loads a process config then merges token-only config files.
func LoadProcessAndTokenConfigs(processPath string, tokenPaths []string) (*Config, error) {
	if processPath == "" {
		return nil, fmt.Errorf("process config path is required")
	}
	if len(tokenPaths) == 0 {
		return nil, fmt.Errorf("at least one token config file is required with --process")
	}

	paths := make([]string, 0, 1+len(tokenPaths))
	paths = append(paths, processPath)
	paths = append(paths, tokenPaths...)
	return LoadFiles(paths)
}

// CheckOptions controls Check (CI validation) behavior.
type CheckOptions struct {
	// RequireVaultCredentials fails when role_id/secret_id are empty after env expansion.
	// Default false so CI can validate structure without live AppRole secrets.
	RequireVaultCredentials bool
}

// Check applies defaults and validates a loaded config the same way the daemon
// does, without contacting Linode or Vault.
//
// When RequireVaultCredentials is false (default), empty vault role_id/secret_id
// after ${VAR} expansion are filled with placeholders so Validate can run.
func Check(cfg *Config, opts CheckOptions) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	cfg.ApplyDefaults()

	if !opts.RequireVaultCredentials {
		if cfg.Vault.RoleID == "" {
			cfg.Vault.RoleID = vaultCredentialPlaceholder
		}
		if cfg.Vault.SecretID == "" {
			cfg.Vault.SecretID = vaultCredentialPlaceholder
		}
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	if err := validateUniqueTokenLabels(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	return nil
}

// LoadAndCheck loads pathOrPattern (single file or glob), applies defaults, and validates.
func LoadAndCheck(pathOrPattern string, opts CheckOptions) (*Config, error) {
	cfg, err := loadPathOrPattern(pathOrPattern)
	if err != nil {
		return nil, err
	}
	if err := Check(cfg, opts); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadFilesAndCheck loads explicit files in order, applies defaults, and validates.
func LoadFilesAndCheck(paths []string, opts CheckOptions) (*Config, error) {
	cfg, err := LoadFiles(paths)
	if err != nil {
		return nil, err
	}
	if err := Check(cfg, opts); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadDirAndCheck loads a multi-file directory layout and validates it.
func LoadDirAndCheck(dir string, opts CheckOptions) (*Config, error) {
	cfg, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}
	if err := Check(cfg, opts); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadProcessAndTokenConfigsAndCheck loads process + token files and validates.
func LoadProcessAndTokenConfigsAndCheck(
	processPath string,
	tokenPaths []string,
	opts CheckOptions,
) (*Config, error) {
	cfg, err := LoadProcessAndTokenConfigs(processPath, tokenPaths)
	if err != nil {
		return nil, err
	}
	if err := Check(cfg, opts); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadPathOrPattern(pathOrPattern string) (*Config, error) {
	if containsGlobChar(pathOrPattern) {
		return LoadGlob(pathOrPattern)
	}
	return Load(pathOrPattern)
}

func isYAMLConfigFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

func validateUniqueTokenLabels(cfg *Config) error {
	seen := make(map[string]int, len(cfg.Tokens))
	for i, token := range cfg.Tokens {
		label := token.Label
		if label == "" {
			continue
		}
		if prev, ok := seen[label]; ok {
			return fmt.Errorf(
				"duplicate token label %q (token[%d] and token[%d])",
				label,
				prev,
				i,
			)
		}
		seen[label] = i
	}
	return nil
}
