package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/linode-obs/latr/internal/config"
)

// multiFlag collects repeated flag values (e.g. --config a --config b).
type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	*m = append(*m, value)
	return nil
}

// runCheckConfig validates configuration without contacting Linode or Vault.
// Exit 0 when valid; non-zero when invalid or usage error.
func runCheckConfig(args []string) int {
	fs := flag.NewFlagSet("check-config", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var configs multiFlag
	fs.Var(&configs, "config", "Config file path or glob (repeatable; classic single-file / multi-file merge)")
	processPath := fs.String("process", "", "Process config file; requires one or more --config token files")
	dirPath := fs.String("dir", "", "Directory containing _process.yaml plus one or more token YAML files")
	requireVaultCreds := fs.Bool(
		"require-vault-credentials",
		false,
		"Fail if vault role_id/secret_id are empty after env expansion (default: allow empty for CI)",
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: latr check-config [flags]

Validate configuration using the same parse/default/validate path as the daemon.
Does not contact Linode or Vault. Exit 0 if valid, non-zero otherwise.

Modes (pick one):
  Classic single file or glob:
    latr check-config --config config.yaml
    latr check-config --config 'configs/*.yaml'

  Multi-file (process + token files):
    latr check-config --process process.yaml --config team-a.yml --config team-b.yml

  Directory layout (_process.yaml + token files under one dir):
    latr check-config --dir path/to/config-dir

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Notes:
  - Empty vault role_id/secret_id after ${VAR} expansion are accepted by default
    so CI can validate structure without live AppRole secrets.
  - Pass --require-vault-credentials to enforce non-empty credentials.
  - Duplicate token labels across merged files are rejected.
`)
	}

	if err := fs.Parse(args); err != nil {
		// flag package already prints usage on -h / parse errors
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "check-config: unexpected arguments: %v\n", fs.Args())
		fs.Usage()
		return 2
	}

	modeCount := 0
	if *dirPath != "" {
		modeCount++
	}
	if *processPath != "" {
		modeCount++
	}
	// classic: --config without --process/--dir
	classic := *processPath == "" && *dirPath == "" && len(configs) > 0
	if classic {
		modeCount++
	}

	if modeCount == 0 {
		fmt.Fprintf(os.Stderr, "check-config: specify --config, --dir, or --process with --config\n")
		fs.Usage()
		return 2
	}
	if modeCount > 1 {
		fmt.Fprintf(os.Stderr, "check-config: use only one of --dir, --process, or classic --config\n")
		fs.Usage()
		return 2
	}
	if *processPath != "" && len(configs) == 0 {
		fmt.Fprintf(os.Stderr, "check-config: --process requires at least one --config token file\n")
		fs.Usage()
		return 2
	}
	if *dirPath != "" && len(configs) > 0 {
		fmt.Fprintf(os.Stderr, "check-config: --dir cannot be combined with --config\n")
		fs.Usage()
		return 2
	}

	opts := config.CheckOptions{
		RequireVaultCredentials: *requireVaultCreds,
	}

	var (
		cfg *config.Config
		err error
		src string
	)

	switch {
	case *dirPath != "":
		src = *dirPath
		cfg, err = config.LoadDirAndCheck(*dirPath, opts)
	case *processPath != "":
		src = *processPath + " + " + strings.Join(configs, ", ")
		cfg, err = config.LoadProcessAndTokenConfigsAndCheck(*processPath, configs, opts)
	case len(configs) == 1:
		src = configs[0]
		cfg, err = config.LoadAndCheck(configs[0], opts)
	default:
		// Multiple --config without --process: merge in flag order
		src = strings.Join(configs, ", ")
		cfg, err = config.LoadFilesAndCheck(configs, opts)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "check-config: %v\n", err)
		return 1
	}

	fmt.Printf(
		"OK: %d token(s) valid (%s)\n",
		len(cfg.Tokens),
		src,
	)
	return 0
}
