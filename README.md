# latr - Linode API Token Rotator

A Go application for automatically managing and rotating Linode API tokens with configurable validity periods and secure storage in HashiCorp Vault.

## Features

- **Automatic Token Rotation**: Automatically rotates tokens based on configurable thresholds (default: 10% validity remaining)
- **Secure Storage**: Stores rotated tokens in HashiCorp Vault (KV v2)
- **State Tracking**: Tracks token rotation history and state via Vault metadata
- **Graceful Token Management**: Keeps old tokens until expiration (configurable pruning)
- **Multiple Tokens**: Manage multiple API tokens with different configurations
- **Team Metadata**: Associate tokens with owning teams for organization
- **Flexible Configuration**: Single YAML file or glob pattern support
- **Daemon or One-Shot**: Run as a long-running daemon or one-time execution
- **Dry-Run Mode**: Test configuration without making changes
- **OpenTelemetry Support**: Observability via traces, metrics, and logs

## Requirements

- Go 1.21+
- Linode account with API token (for creating/managing tokens)
- HashiCorp Vault with AppRole authentication
- Valid Vault role and secret IDs

## Installation

### Using Container Image (Recommended)

```bash
# Pull the latest image from GitHub Container Registry
docker pull ghcr.io/linode-obs/latr:latest

# Or pull a specific version
docker pull ghcr.io/linode-obs/latr:v1.0.0
```

#### One-Shot Mode (Run Once)

```bash
docker run --rm \
  -v $(pwd)/config.yaml:/config/config.yaml:ro \
  -e LINODE_TOKEN="your-linode-api-token" \
  -e VAULT_ROLE_ID="your-vault-role-id" \
  -e VAULT_SECRET_ID="your-vault-secret-id" \
  ghcr.io/linode-obs/latr:latest \
  -config /config/config.yaml
```

#### Daemon Mode (Continuous Running)

```bash
docker run -d \
  --name latr \
  --restart unless-stopped \
  -v $(pwd)/config.yaml:/config/config.yaml:ro \
  -e LINODE_TOKEN="your-linode-api-token" \
  -e VAULT_ROLE_ID="your-vault-role-id" \
  -e VAULT_SECRET_ID="your-vault-secret-id" \
  ghcr.io/linode-obs/latr:latest \
  -config /config/config.yaml

# View logs
docker logs -f latr

# Stop gracefully
docker stop latr
```

#### Using Docker Compose

```yaml
services:
  latr:
    image: ghcr.io/linode-obs/latr:latest
    container_name: latr
    restart: unless-stopped
    volumes:
      - ./config.yaml:/config/config.yaml:ro
    environment:
      - LINODE_TOKEN=${LINODE_TOKEN}
      - VAULT_ROLE_ID=${VAULT_ROLE_ID}
      - VAULT_SECRET_ID=${VAULT_SECRET_ID}
    command: ["-config", "/config/config.yaml"]
```

#### Multi-Architecture Support

Images are available for:

- `linux/amd64` (x86_64)
- `linux/arm64` (ARM 64-bit, including Apple Silicon and Raspberry Pi 4+)

### Download Binary from Releases

Download the latest release for your platform from the [releases page](https://github.com/linode-obs/latr/releases).

### Build from Source

```bash
# Clone the repository
git clone https://github.com/linode-obs/latr.git
cd latr

# Build the binary
go build -o latr ./cmd/latr

# Or install directly
go install ./cmd/latr
```

## Configuration

### Environment Variables

- `LINODE_TOKEN`: Your Linode API token (required)
- `VAULT_ROLE_ID`: Vault AppRole role ID (optional if in config)
- `VAULT_SECRET_ID`: Vault AppRole secret ID (optional if in config)

### Configuration File

Create a YAML configuration file (see `examples/config.yaml`):

```yaml
# Global daemon settings
daemon:
  mode: "daemon" # "daemon" or "one-shot"
  check_interval: "30m" # How often to check tokens (daemon mode only)
  dry_run: false # If true, no actual changes are made

# Rotation behavior
rotation:
  threshold_percent: 10 # Rotate when <=10% of validity remains

# Vault configuration
vault:
  address: "https://vault.example.com"
  role_id: "${VAULT_ROLE_ID}" # Can use env vars
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "secret" # KV v2 mount path

# Observability settings
observability:
  otel_endpoint: "localhost:4317" # OpenTelemetry collector endpoint (optional)
  log_level: "info" # debug, info, warn, error

# Token definitions
tokens:
  - label: "my-api-token"
    team: "platform-team"
    validity: "90d" # Must be <= 6 months (180d)
    scopes: "*" # "*" for all scopes, or comma-separated list
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/my-api-token"

  - label: "backup-token"
    team: "sre-team"
    validity: "180d" # Maximum allowed
    scopes: "linodes:read_only,domains:read_only"
    rotation_threshold: 15 # Override global threshold for this token
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/backup"
```

## Usage

### One-Shot Mode

Run once and exit:

```bash
export LINODE_TOKEN="your-linode-token"
./latr -config config.yaml
```

### Daemon Mode

Run continuously with periodic checks:

```yaml
# In config.yaml
daemon:
  mode: "daemon"
  check_interval: "30m"
```

```bash
export LINODE_TOKEN="your-linode-token"
./latr -config config.yaml
```

### Dry-Run Mode

Test configuration without making changes:

```yaml
daemon:
  dry_run: true
```

### Multiple Configuration Files

Use a glob pattern to load multiple config files:

```bash
./latr -config "configs/*.yaml"
```

### Version Information

```bash
./latr -version
```

## How It Works

1. **Token Discovery**: latr checks if each configured token exists in Linode
2. **Rotation Check**: Calculates percentage of validity remaining
3. **Token Creation/Rotation**:
   - If token doesn't exist: Creates it
   - If rotation needed: Creates new token with same label
4. **Storage**: Stores new token value in configured Vault paths
5. **State Tracking**: Updates Vault metadata with:
   - Current token ID and value
   - Previous token ID and expiry
   - Rotation count and timestamp

### Important Behaviors

- **Automatic cleanup**: Expired tokens are automatically pruned by the Linode API - no manual cleanup needed
- **Only manages configured tokens**: Only rotates tokens specified in the configuration
- **Vault retry on failure**: If Linode succeeds but Vault fails, state is tracked for retry on next run
- **Graceful shutdown**: Handles SIGTERM/SIGINT for clean daemon shutdown

## Token Validity

- Maximum validity: **6 months (180 days)**
- Supported formats: `90d`, `180d`, `6mo`, `30m`, `1h`
- Default rotation threshold: **10% of validity remaining**
- Token-specific thresholds can override global setting

## Development

### Building Container Images Locally

The `Dockerfile` is optimized for GoReleaser and expects a pre-built binary. For local development builds:

```bash
# Build the binary first
go build -o latr ./cmd/latr

# Build the container image
docker build -t latr:dev .

# Or build and run in one step
go build -o latr ./cmd/latr && docker build -t latr:dev . && \
  docker run --rm latr:dev -version
```

### Creating Releases

Releases are automated via GoReleaser and GitHub Actions:

1. **Tag a new version** (must follow semantic versioning):

   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Automated release process**:
   - Builds binaries for Linux, macOS, Windows (amd64 and arm64)
   - Creates multi-arch Docker images (amd64, arm64)
   - Publishes images to `ghcr.io/linode-obs/latr`
   - Generates release notes from conventional commits
   - Uploads release artifacts to GitHub Releases

3. **Testing releases locally** (without pushing):

   ```bash
   # Install GoReleaser
   go install github.com/goreleaser/goreleaser/v2@latest

   # Test release build (snapshot mode)
   goreleaser release --snapshot --clean

   # Check output in dist/
   ls -la dist/
   ```

4. **Conventional Commit Format** (for better changelogs):
   - `feat:` New features
   - `fix:` Bug fixes
   - `docs:` Documentation changes
   - `chore:` Maintenance tasks
   - `test:` Test additions/changes

### Running Tests

```bash
# Run unit tests
go test ./...
make test

# Run unit tests with coverage
go test ./... -cover

# Run e2e tests (requires Docker)
make test-e2e

# Run all tests
make test-all

# Run specific package tests
go test ./internal/rotation -v
```

### Project Structure

```
latr/
├── cmd/latr/           # Main application entrypoint
├── internal/
│   ├── config/        # Configuration parsing and validation
│   ├── linode/        # Linode API client wrapper
│   ├── vault/         # Vault client with AppRole auth
│   ├── rotation/      # Core rotation engine logic
│   ├── scheduler/     # Daemon/one-shot scheduler
│   └── observability/ # OpenTelemetry setup
├── pkg/models/        # Shared domain models
└── examples/          # Example configurations
```

### Testing Approach

This project was built using **Test-Driven Development (TDD)**:

- All features have comprehensive unit tests
- Mock implementations for external dependencies (Linode, Vault)
- Table-driven tests for validation logic
- ✅ Integration tests with Docker Compose

## Observability

latr supports OpenTelemetry for observability:

### Metrics

- `latr_tokens_total` - Total configured tokens
- `latr_rotations_total{status,label,team}` - Rotation attempts (per token)
- `latr_rotation_duration_seconds{label,team}` - Rotation operation duration
- `latr_token_validity_remaining_seconds{label,team}` - Time until rotation needed
- `latr_vault_storage_errors_total{path}` - Vault write failures

### Grafana dashboard & alerts (mixin)

A [Prometheus monitoring mixin](https://monitoring.mixins.dev/) lives in [`latr-mixin/`](./latr-mixin/). It provides a Grafana dashboard and alerting rules for the metrics above.

```bash
cd latr-mixin
make build
# → dashboards_out/latr.json
# → prometheus_alerts.yaml
```

See [`latr-mixin/README.md`](./latr-mixin/README.md) for configuration (job selectors, thresholds) and kube-prometheus usage.

### Traces

Distributed tracing for rotation operations (when OTel endpoint configured)

### Logs

Structured logging with rotation events, errors, and state changes

## Security Considerations

- Store `LINODE_TOKEN` securely (env var, secrets manager)
- Use Vault AppRole with minimal required permissions
- Enable TLS for Vault communication in production
- Rotate Vault AppRole secret IDs regularly
- Review token scopes - use least privilege principle

## Roadmap

- [x] Add proper tracing with OTel (<https://github.com/linode-obs/latr/pull/11>)
- [ ] Use structured logging (<https://github.com/linode-obs/latr/issues/16>)
- [ ] Additional storage backends (<https://github.com/linode-obs/latr/issues/17>)
- [x] Prometheus metrics exporter (<https://github.com/linode-obs/latr/pull/11>)
- [x] Integration tests with Docker Compose (<https://github.com/linode-obs/latr/pull/2>)

## Contributing

Contributions welcome! Please:

1. Write tests for new features
2. Follow existing code structure
3. Update documentation
4. Run `go test ./...` before submitting
