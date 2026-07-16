# Agent Instructions for latr

## Project Overview

`latr` (Linode API Token Rotator) is a Go daemon that automatically rotates Linode API tokens based on configurable validity thresholds. It uses HashiCorp Vault for secure storage and implements comprehensive observability with OpenTelemetry.

## Architecture

- **Clean Architecture**: Dependencies inject interfaces; core logic has no external dependencies
- **Component Separation**: Clear boundaries between config, rotation engine, scheduler, and clients
- **Interface-Driven Design**: All external dependencies (Linode, Vault) are abstracted behind interfaces for testing

### Key Components

- `internal/rotation/engine.go`: Core business logic for token lifecycle management
- `internal/scheduler/scheduler.go`: Handles daemon vs one-shot execution modes
- `internal/config/`: YAML config loading with environment variable expansion
- `pkg/models/`: Domain models for tokens and state tracking
- External clients: `internal/linode/` and `internal/vault/`

## Development Patterns

### Testing Strategy (TDD)

- **Mock-based testing**: Use `testify/mock` for external dependencies
- **Table-driven tests**: Standard Go pattern for validation logic
- **E2E tests**: Docker Compose with real Vault + mock Linode API (`test/e2e/`)
- Run tests using mise:
  - Unit tests: `mise run test`
  - E2E tests: `mise run test-e2e`
  - All tests: `mise run test-all`

### Structured Logging

- **slog package**: All logging uses Go's standard library `log/slog` with JSON output
- **Trace context**: OpenTelemetry trace/span IDs automatically attached via `observability.TraceAttrs(ctx)`
- **Rich context**: Log entries include structured fields (token labels, IDs, errors, timestamps)
- **Logger access**: Use `observability.GetLogger()` to get the global logger instance
- **Pattern**: Always include trace context: `logger.InfoContext(ctx, "message", append([]any{...fields}, observability.TraceAttrs(ctx)...)...)`

### Configuration

- Single YAML file or glob pattern support (`-config "configs/*.yaml"`)
- Environment variable expansion: `${VAULT_ROLE_ID}` syntax
- Validation at startup with descriptive error messages
- See `examples/config.yaml` for complete configuration reference

### Error Handling

- Context-aware errors with wrapped error chains
- Graceful degradation: Continue processing other tokens if one fails
- State persistence: Track rotation attempts in Vault metadata for retry logic

### Build & Deployment

- **Container-first**: Primary deployment via `ghcr.io/linode-obs/latr`
- **Multi-arch**: linux/amd64 and linux/arm64 support
- **GoReleaser**: Automated releases on git tags
- Local build: `go build -o latr ./cmd/latr` (or use a custom `mise` task if defined)

## Critical Workflows

### Token Rotation Flow

1. Discovery: Find existing tokens by label in Linode API
2. Evaluation: Calculate validity percentage remaining
3. Creation: Generate new token if rotation needed
4. Storage: Write new token + state to Vault

Note: Expired tokens are automatically pruned by the Linode API - no manual cleanup required

### Daemon vs One-Shot

- **Daemon mode**: Continuous operation with configurable `check_interval`
- **One-shot**: Single execution then exit
- Both modes support graceful shutdown on SIGTERM/SIGINT

### State Management

- Vault metadata stores `TokenState` for rotation history
- Tracks current/previous token IDs and expiry times
- Enables retry logic if Linode succeeds but Vault fails

## Key Files to Understand

- `cmd/latr/main.go`: Application bootstrap and dependency wiring
- `internal/rotation/engine.go`: Core rotation algorithm and state management
- `internal/config/config.go`: Configuration structure and validation
- `pkg/models/token.go`: Domain models with business logic methods
- `test/e2e/e2e_test.go`: Integration test patterns and test environment setup

## Dependencies & Integration

- **Linode API**: OAuth2 token-based authentication via `linodego` client
- **HashiCorp Vault**: AppRole authentication, KV v2 secrets engine
- **OpenTelemetry**: Metrics, traces, and structured logging
- **Required env vars**: `LINODE_TOKEN` (always), `VAULT_ROLE_ID`/`VAULT_SECRET_ID` (if not in config)

## Common Development Tasks

- Add new storage backend: Implement `VaultClient` interface in new package
- Extend observability: Add metrics/traces in `internal/observability/telemetry.go`
- Configuration changes: Update structs in `internal/config/config.go` + validation
- New rotation logic: Modify `ProcessToken` method in `internal/rotation/engine.go`
- Testing: Follow existing mock patterns; use `testify/assert` and `testify/require`
