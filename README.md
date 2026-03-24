# latr - Linode API Token Rotator

A Go application for automatically managing and rotating Linode API tokens with configurable validity periods and secure storage in HashiCorp Vault.

## Features

- **Automatic Token Rotation**: Automatically rotates tokens based on configurable thresholds (default: 10% validity remaining)
- **Secure Storage**: Stores rotated tokens in HashiCorp Vault (KV v2)
- **State Tracking**: Tracks token rotation history and state via Vault metadata
- **Graceful Token Management**: Keeps old tokens until the Linode API auto-expires them
- **Multi-Account Support**: Manage tokens across multiple Linode accounts with separate API credentials
- **Account Token Management**: Optionally rotate the account's own API token
- **Multiple Tokens**: Manage multiple API tokens with different configurations
- **Team Metadata**: Associate tokens with owning teams for organization
- **Flexible Configuration**: Single YAML file or glob pattern for multi-account setups
- **Custom API Endpoints**: Override the Linode API URL per account (e.g., for staging)
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
docker pull ghcr.io/wbh1/latr:latest

# Or pull a specific version
docker pull ghcr.io/wbh1/latr:v1.0.0
```

#### One-Shot Mode (Run Once)

```bash
docker run --rm \
  -v $(pwd)/config.yaml:/config/config.yaml:ro \
  -e LINODE_TOKEN="your-linode-api-token" \
  -e VAULT_ROLE_ID="your-vault-role-id" \
  -e VAULT_SECRET_ID="your-vault-secret-id" \
  ghcr.io/wbh1/latr:latest \
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
  ghcr.io/wbh1/latr:latest \
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
    image: ghcr.io/wbh1/latr:latest
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

Download the latest release for your platform from the [releases page](https://github.com/wbh1/latr/releases).

### Build from Source

```bash
# Clone the repository
git clone https://github.com/wbh1/latr.git
cd latr

# Build the binary
go build -o latr ./cmd/latr

# Or install directly
go install ./cmd/latr
```

## Configuration

latr supports single-file and multi-file configurations. In multi-file mode,
a global config (`global: true`) provides defaults that account configs inherit
and can override. Environment variables are expanded using `${VAR_NAME}` syntax.

- `LINODE_TOKEN`: Your Linode API token (optional if in config)
- `VAULT_ROLE_ID`: Vault AppRole role ID (optional if in config)
- `VAULT_SECRET_ID`: Vault AppRole secret ID (optional if in config)

### Credential Resolution

Credentials can be set at multiple levels. For each credential, latr checks
in order: account config → global config → environment variable.

| Credential | Account config | Global config | Env var |
|---|---|---|---|
| Linode API token | `account.token.storage` | — | `LINODE_TOKEN` |
| Linode API URL | `account.api_url` | — | `LINODE_API_URL` |
| Vault role ID | `account.vault.role_id` | `vault.role_id` | `VAULT_ROLE_ID` |
| Vault secret ID | `account.vault.secret_id` | `vault.secret_id` | `VAULT_SECRET_ID` |

If no source provides a required value (Linode token, Vault role/secret ID),
latr exits with an error identifying which credential is missing and for
which account. The Linode API URL defaults to `https://api.linode.com` if
not set anywhere.

### Single-File Configuration

For simple setups with one Linode account, everything goes in a single file:

```yaml
# Linode Account config (self)
account:
  label: "lcid-1234"
  team: "platform-team"
  token:
    storage:
      - type: "vault"
        path: "linode/accounts"

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
        path: "linode/tokens/my-api-token"
        # key: "token"  # Optional: key name within the Vault secret (default: "token")

  - label: "backup-token"
    team: "sre-team"
    validity: "180d" # Maximum allowed
    scopes: "linodes:read_only,domains:read_only"
    rotation_threshold: 15 # Override global threshold for this token
    storage:
      - type: "vault"
        path: "linode/tokens/backup"
```

Storage entries support an optional `key` field that specifies the key name
within the Vault KV v2 secret. It defaults to `"token"` if not set. The
`account.token` storage uses `account.label` as the key automatically, which
allows multiple accounts to share a single Vault path with separate keys.
When a non-default key is used, token state metadata is tracked at a derived
path to avoid collisions.

### Multi-File Configuration

For multiple accounts, use a global config for shared defaults and separate
files per account. The global config is identified by `global: true` — file
order doesn't matter. Only one global config is allowed.

**Global config** (`global: true`) — shared defaults. Can optionally include
`account` and `tokens` if it also serves as an account config (useful for
single-file setups that need `global: true`):

```yaml
global: true

daemon:
  mode: "daemon"
  check_interval: "30m"

rotation:
  threshold_percent: 10

vault:
  address: "https://vault.example.com"
  role_id: "${VAULT_ROLE_ID}"
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "secret"

observability:
  log_level: "info"
  otel_endpoint: "localhost:4317"
```

**Account configs** — inherit global defaults, override what's needed:

```yaml
account:
  label: "lcid-1234"
  team: "platform-team"
  vault:
    role_id: "account-specific-role-id"   # Overrides global vault.role_id
    secret_id: "account-specific-secret"  # Overrides global vault.secret_id
  token:
    storage:
      - type: "vault"
        path: "linode/accounts"

tokens:
  - label: "my-api-token"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "linode/tokens/my-api-token"
```

Account configs can also override `rotation` and `vault` settings from the
global config. Any field not specified is inherited from the global
defaults. Observability and daemon settings (`observability`, `mode`,
`check_interval`, `dry_run`) are global-only — any per-account values for
these fields are currently ignored and have no effect (a warning is logged
if they differ from the global values).

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

### Multiple Accounts

Use a glob pattern to load a global config and per-account configs:

```bash
./latr -config "configs/*.yaml"
```

```
configs/
├── globals.yaml       # global: true — shared defaults
├── production.yaml    # account + tokens (inherits globals)
└── staging.yaml       # account + tokens (inherits globals, overrides as needed)
```

**configs/globals.yaml:**

```yaml
global: true

daemon:
  mode: "daemon"
  check_interval: "30m"

vault:
  address: "https://vault.example.com"
  role_id: "${VAULT_ROLE_ID}"
  secret_id: "${VAULT_SECRET_ID}"
```

**configs/production.yaml:**

```yaml
account:
  label: "lcid-1234"
  team: "platform-team"
  vault:
    role_id: "prod-role-id"     # Override global credentials
    secret_id: "prod-secret-id"
  token:
    storage:
      - type: "vault"
        path: "linode/accounts"

tokens:
  - label: "linode-api"
    validity: "180d"
    scopes: "linodes:read_write"
    storage:
      - type: "vault"
        path: "prod/linode-api"
```

**configs/staging.yaml:**

```yaml
account:
  label: "lcid-5678"
  team: "platform-team"
  api_url: "https://api.staging.example.com"
  token:
    storage:
      - type: "vault"
        path: "linode/accounts"

# No account.vault — inherits global vault credentials

rotation:
  threshold_percent: 20  # Override: rotate earlier in staging

tokens:
  - label: "staging-api"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "staging/api"
```

All tokens in a configuration file use that file's account credentials. Tokens
from `production.yaml` are rotated using the production account's API token;
tokens from `staging.yaml` use the staging account's token.

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

### Account Token Management

The `account.token` field optionally configures latr to rotate the account's own
API token — the one configured in `account.token`. When rotation occurs,
the new token is stored in Vault. The old token remains valid until Linode
auto-expires it. The operator is responsible for restarting latr to pick up the
new token (e.g., via ArgoCD syncing a ConfigMap update).

### Important Behaviors

- **Automatic cleanup**: Expired tokens are automatically pruned by the Linode API - no manual cleanup needed
- **Only manages configured tokens**: Only rotates tokens specified in the configuration
- **Account isolation**: Each config file's tokens are managed using that file's account credentials
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
   - Publishes images to `ghcr.io/wbh1/latr`
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
- `latr_rotations_total{status="success|failure"}` - Rotation attempts
- `latr_rotation_duration_seconds` - Rotation operation duration
- `latr_token_validity_remaining_seconds` - Time until rotation needed
- `latr_vault_storage_errors_total` - Vault write failures

### Traces

Distributed tracing for rotation operations (when OTel endpoint configured)

### Logs

Structured logging with rotation events, errors, and state changes

## Security Considerations

- Use `account.token.storage` to avoid plaintext tokens in config files
- If using plain string form, store the value securely (env var expansion, K8s secrets)
- Use Vault AppRole with minimal required permissions
- Enable TLS for Vault communication in production
- Rotate Vault AppRole secret IDs regularly
- Review token scopes - use least privilege principle

## Roadmap

- [x] Add proper tracing with OTel (<https://github.com/wbh1/latr/pull/11>)
- [x] Use structured logging (<https://github.com/wbh1/latr/issues/16>)
- [ ] Additional storage backends (<https://github.com/wbh1/latr/issues/17>)
- [x] Prometheus metrics exporter (<https://github.com/wbh1/latr/pull/11>)
- [x] Integration tests with Docker Compose (<https://github.com/wbh1/latr/pull/2>)

## Contributing

Contributions welcome! Please:

1. Write tests for new features
2. Follow existing code structure
3. Update documentation
4. Run `go test ./...` before submitting
