# latr Helm Chart

This Helm chart deploys [latr](https://github.com/wbh1/latr) (Linode API Token Rotator) on a Kubernetes cluster.

## Overview

latr is an automation tool that manages and automatically rotates Linode API tokens with configurable validity periods and secure storage in HashiCorp Vault.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- A running HashiCorp Vault instance with:
  - AppRole authentication enabled
  - KV v2 secret engine mounted
  - Appropriate policies configured
- A valid Linode API token
- Container image available at `ghcr.io/wbh1/latr`

## Installing the Chart

### Basic Installation

```bash
helm install latr ./helm/latr \
  --set config.account.label="lcid-1234" \
  --set secrets.vaultRoleId="your-vault-role-id" \
  --set secrets.vaultSecretId="your-vault-secret-id" \
  --set secrets.linodeToken="your-linode-token" \
  --set config.vault.address="https://vault.example.com:8200"
```

### Installation with Vault Token Source (Recommended)

When using `account.token.storage`, the Linode API token is read from Vault at
startup — no plaintext secrets in config or K8s Secrets.

Create a `custom-values.yaml` file:

```yaml
config:
  account:
    label: "lcid-1234"
    team: "platform-team"
    token:
      storage:
        - type: vault
          path: "linode/accounts"

  vault:
    address: "https://vault.example.com:8200"

  tokens:
    - label: "production-api-token"
      team: "platform-team"
      validity: "90d"
      scopes: "*"
      storage:
        - type: "vault"
          path: "linode/tokens/production"

secrets:
  vaultRoleId: "your-vault-role-id"
  vaultSecretId: "your-vault-secret-id"
```

### Installation with LINODE_TOKEN Env Var

If not using `account.token.storage`, the `LINODE_TOKEN` env var is used:

```yaml
config:
  account:
    label: "production"
    team: "platform-team"

  vault:
    address: "https://vault.example.com:8200"

  tokens:
    - label: "production-api-token"
      team: "platform-team"
      validity: "90d"
      scopes: "*"
      storage:
        - type: "vault"
          path: "linode/tokens/production"

secrets:
  linodeToken: "your-linode-token"
  vaultRoleId: "your-vault-role-id"
  vaultSecretId: "your-vault-secret-id"
```

Then install:

```bash
helm install latr ./helm/latr -f custom-values.yaml
```

### Installation with Existing Secret

If you already have a Kubernetes secret with the required credentials:

```bash
kubectl create secret generic latr-credentials \
  --from-literal=linode-token="your-linode-token" \
  --from-literal=vault-role-id="your-vault-role-id" \
  --from-literal=vault-secret-id="your-vault-secret-id"

helm install latr ./helm/latr \
  --set secrets.existingSecret="latr-credentials" \
  --set config.vault.address="https://vault.example.com:8200"
```

## Upgrading the Chart

```bash
helm upgrade latr ./helm/latr -f custom-values.yaml
```

## Uninstalling the Chart

```bash
helm uninstall latr
```

## Configuration

The following table lists the configurable parameters of the latr chart and their default values.

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of latr replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/wbh1/latr` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (defaults to chart appVersion) | `""` |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override the name of the chart | `""` |
| `fullnameOverride` | Override the fullname of the chart | `""` |

### Service Account Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a service account | `true` |
| `serviceAccount.automount` | Automount service account token | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Security Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContext` | Pod security context | See values.yaml |
| `securityContext` | Container security context | See values.yaml |

### Resource Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Pod Disruption Budget Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podDisruptionBudget.enabled` | Enable PodDisruptionBudget | `false` |
| `podDisruptionBudget.minAvailable` | Minimum available pods | `1` |

### Account Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.account.label` | Account label (required). Also used as storage key | `""` |
| `config.account.team` | Team that owns this account | `""` |
| `config.account.vault.roleId` | Vault AppRole role ID for this account | `"${VAULT_ROLE_ID}"` |
| `config.account.vault.secretId` | Vault AppRole secret ID for this account | `"${VAULT_SECRET_ID}"` |
| `config.account.vault.address` | Override global Vault address for this account | `""` |
| `config.account.vault.mountPath` | Override global Vault mount path for this account | `""` |
| `config.account.apiUrl` | Linode API URL override | `""` (uses `https://api.linode.com`) |
| `config.account.token` | Account token config — storage source + optional rotation | `{}` |

### latr Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.daemon.mode` | Execution mode: `daemon` or `one-shot` | `daemon` |
| `config.daemon.checkInterval` | Check interval for daemon mode | `30m` |
| `config.daemon.dryRun` | Enable dry-run mode | `false` |
| `config.rotation.thresholdPercent` | Rotation threshold percentage | `10` |
| `config.rotation.pruneExpired` | Prune expired tokens | `false` |
| `config.vault.address` | Global Vault server address | `""` |
| `config.vault.mountPath` | Global Vault KV v2 mount path | `secret` |
| `config.observability.otelEndpoint` | OpenTelemetry endpoint | `""` |
| `config.observability.logLevel` | Log level | `info` |
| `config.tokens` | Token configurations (list) | `[]` |

### Secrets Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `secrets.linodeToken` | Linode API token | `""` |
| `secrets.vaultRoleId` | Vault AppRole role ID | `""` |
| `secrets.vaultSecretId` | Vault AppRole secret ID | `""` |
| `secrets.existingSecret` | Use existing secret | `""` |

### Other Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |
| `env` | Additional environment variables | `[]` |
| `envFrom` | Additional environment from sources | `[]` |
| `volumes` | Additional volumes | `[]` |
| `volumeMounts` | Additional volume mounts | `[]` |

## Examples

### Example 1: Basic Daemon Mode with Vault Token Source

Deploy latr in daemon mode, reading the Linode API token from Vault:

```yaml
config:
  account:
    label: "lcid-1234"
    team: "platform"
    token:
      storage:
        - type: vault
          path: "linode/accounts"

  daemon:
    mode: daemon
    checkInterval: "1h"

  vault:
    address: "https://vault.example.com:8200"

  tokens:
    - label: "my-api-token"
      team: "platform"
      validity: "90d"
      scopes: "*"
      storage:
        - type: "vault"
          path: "linode/tokens/my-token"

secrets:
  vaultRoleId: "your-vault-role-id"
  vaultSecretId: "your-vault-secret-id"
```

### Example 2: Multiple Tokens with Different Configurations

```yaml
config:
  account:
    label: "lcid-1234"
    team: "platform"
    token:
      storage:
        - type: vault
          path: "linode/accounts"

  vault:
    address: "https://vault.example.com:8200"

  tokens:
    - label: "prod-full-access"
      team: "platform"
      validity: "90d"
      scopes: "*"
      rotationThreshold: 10
      storage:
        - type: "vault"
          path: "linode/tokens/prod-full"

    - label: "dev-limited-access"
      team: "development"
      validity: "30d"
      scopes: "linodes:read_only,images:read_only"
      rotationThreshold: 20
      storage:
        - type: "vault"
          path: "linode/tokens/dev-limited"
```

### Example 3: With OpenTelemetry

```yaml
config:
  account:
    label: "lcid-1234"
    team: "ops"
    token:
      storage:
        - type: vault
          path: "linode/accounts"

  vault:
    address: "https://vault.example.com:8200"

  observability:
    otelEndpoint: "otel-collector:4317"
    logLevel: "debug"

  tokens:
    - label: "monitored-token"
      team: "ops"
      validity: "90d"
      scopes: "*"
      storage:
        - type: "vault"
          path: "linode/tokens/monitored"
```

### Example 4: High Availability Setup

```yaml
replicaCount: 3

podDisruptionBudget:
  enabled: true
  minAvailable: 2

resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 256Mi

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/name
                operator: In
                values:
                  - latr
          topologyKey: kubernetes.io/hostname
```

## Vault Setup

Before deploying latr, ensure your Vault instance is properly configured:

### 1. Enable AppRole Authentication

```bash
vault auth enable approle
```

### 2. Create a Policy

```bash
vault policy write latr-policy - <<EOF
path "secret/data/linode/tokens/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "secret/metadata/linode/tokens/*" {
  capabilities = ["read", "update", "delete"]
}
EOF
```

### 3. Create an AppRole

```bash
vault write auth/approle/role/latr \
  token_policies="latr-policy" \
  token_ttl=1h \
  token_max_ttl=4h
```

### 4. Get Role ID and Secret ID

```bash
vault read auth/approle/role/latr/role-id
vault write -f auth/approle/role/latr/secret-id
```

Use the returned `role_id` and `secret_id` values in your Helm values.

## Monitoring

View logs from the latr deployment:

```bash
kubectl logs -l app.kubernetes.io/name=latr -f
```

Check deployment status:

```bash
kubectl get deployment latr
kubectl describe deployment latr
```

## Troubleshooting

### Pods not starting

Check pod events:
```bash
kubectl describe pod -l app.kubernetes.io/name=latr
```

### Authentication errors

Verify Vault credentials:
```bash
kubectl get secret latr -o yaml
```

Check Vault connectivity from pod:
```bash
kubectl exec -it <pod-name> -- sh
# (if shell is available in the image)
```

### Configuration issues

View the ConfigMap:
```bash
kubectl get configmap latr -o yaml
```

## License

This chart is provided under the same license as the latr application.

## Links

- [latr GitHub Repository](https://github.com/wbh1/latr)
- [Linode API Documentation](https://www.linode.com/docs/api/)
- [HashiCorp Vault Documentation](https://www.vaultproject.io/docs)
