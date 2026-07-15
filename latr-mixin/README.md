# latr mixin

A [Prometheus monitoring mixin](https://monitoring.mixins.dev/) for [latr](https://github.com/linode-obs/latr) (Linode API Token Rotator).

It packages:

- A Grafana dashboard for rotations, token validity, duration, and Vault errors
- Prometheus alerting rules for failed rotations, Vault storage failures, low remaining validity, and missing metrics

Mixins are written in [Jsonnet](https://jsonnet.org/) and are typically installed with [jsonnet-bundler](https://github.com/jsonnet-bundler/jsonnet-bundler).

## Metrics

| Metric | Type | Labels |
| --- | --- | --- |
| `latr_tokens_total` | gauge | — |
| `latr_rotations_total` | counter | `status` (`success`\|`failure`), `label` |
| `latr_token_validity_remaining_seconds` | gauge | `label` |
| `latr_rotation_duration_seconds_*` | histogram | `label` |
| `latr_vault_storage_errors_total` | counter | `path` |

latr exports these via OpenTelemetry. Names above assume Prometheus-compatible export (histogram `_bucket` / `_sum` / `_count`).

## Prerequisites

```bash
# Jsonnet
brew install jsonnet          # or: go install github.com/google/go-jsonnet/cmd/jsonnet@latest

# jsonnet-bundler (if vendoring into another repo)
brew install jsonnet-bundler  # or: go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest

# Optional: format + Prometheus rule validation
go install github.com/google/go-jsonnet/cmd/jsonnetfmt@latest
# promtool from the Prometheus distribution
```

## Generate files

From this directory:

```bash
make build
```

This produces:

- `prometheus_alerts.yaml` — load into Prometheus / Prometheus Operator
- `dashboards_out/latr.json` — import into Grafana (or serve via sidecar / provisioning)

```bash
make clean    # remove generated outputs
make lint     # jsonnetfmt check + promtool (if installed)
```

## Use from another config repo

```bash
jb init
jb install github.com/linode-obs/latr/latr-mixin@main
```

Create `latr-mixin.libsonnet`:

```jsonnet
local latr = import 'github.com/linode-obs/latr/latr-mixin/mixin.libsonnet';

latr {
  _config+:: {
    // Match how latr metrics appear in your Prometheus
    latrSelector: 'job="latr"',
    // dashboardNamePrefix: 'Platform / ',
    // tokenValidityLowSeconds: 14 * 24 * 3600,
    // alertWindow: '30m',
  },
}
```

Generate:

```bash
mkdir -p files/dashboards
jsonnet -J vendor -S -e 'std.manifestYamlDoc((import "latr-mixin.libsonnet").prometheusAlerts)' > files/alerts.yml
jsonnet -J vendor -m files/dashboards -e '(import "latr-mixin.libsonnet").grafanaDashboards'
```

### kube-prometheus

```jsonnet
local kp = (import 'kube-prometheus/main.libsonnet') +
           (import 'github.com/linode-obs/latr/latr-mixin/mixin.libsonnet') + {
  _config+:: {
    latrSelector: 'namespace="security",job="latr"',
  },
};
```

## Configuration

| Key | Default | Description |
| --- | --- | --- |
| `latrSelector` | `''` | Label selector inserted into metric `{}` (e.g. `job="latr"`) |
| `dashboardNamePrefix` | `''` | Prefix for the Grafana dashboard title |
| `dashboardTags` | `['latr-mixin', ...]` | Grafana tags |
| `grafanaDashboardUid` | `latr-token-rotator` | Stable dashboard UID |
| `dashboardRefresh` | `1m` | Grafana refresh interval |
| `dashboardPeriod` | `now-1h` | Default time range start |
| `tokenValidityLowSeconds` | `604800` (7d) | Threshold for `LatrTokenValidityLow` |
| `alertWindow` | `15m` | Range for increase-based alerts |
| `alertFor` | `5m` | Pending duration before alerts fire |

## Alerts

| Alert | Severity | Meaning |
| --- | --- | --- |
| `LatrTokenRotationFailed` | warning | Failed rotation attempts in the alert window |
| `LatrVaultStorageErrors` | critical | Vault write failures (Linode/Vault may be out of sync — do not revoke tokens) |
| `LatrTokenValidityLow` | warning | A token is within the validity threshold of needing rotation |
| `LatrDown` | warning | `latr_tokens_total` is absent for 15m |

## Dashboard

The dashboard includes:

1. **Overview** — configured tokens, success/failure counts, Vault errors, min time until rotation
2. **Rotations** — rate and increase by status and token label
3. **Token validity** — remaining seconds (graph) and days (table)
4. **Duration & Vault errors** — p50/p95/avg rotation duration and Vault error rate

Template variables: Prometheus datasource, token label (multi-select).

## Layout

```
latr-mixin/
├── mixin.libsonnet          # entrypoint
├── config.libsonnet         # overridable _config
├── alerts/
│   └── alerts.libsonnet
├── dashboards/
│   ├── dashboards.libsonnet
│   ├── latr.libsonnet       # selector injection + metadata
│   └── latr.json            # dashboard definition (placeholder queries)
├── alerts.jsonnet
├── dashboards.jsonnet
├── Makefile
└── README.md
```
