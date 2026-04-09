# NRR — Nomad Resource Recommender

NRR analyses historical CPU and memory usage from Prometheus (or VictoriaMetrics) and recommends right-sized resource specs for your HashiCorp Nomad jobs.

Inspired by [KRR](https://github.com/robusta-dev/krr) for Kubernetes.

```
  NRR — Nomad Resource Recommender

╭───┬────────┬─────────┬───────────┬─────────────────┬─────────┬─────────┬──────────────────┬────────┬─────────┬───────────────┬─────────┬──────────────╮
│ # │ REGION │ DC      │ NAMESPACE │ JOB             │ TYPE    │ GROUP   │ TASK             │ ALLOCS │ CPU NOW │ CPU REC       │ MEM NOW │ MEM REC      │
├───┼────────┼─────────┼───────────┼─────────────────┼─────────┼─────────┼──────────────────┼────────┼─────────┼───────────────┼─────────┼──────────────┤
│ 1 │ global │ dc1     │ default   │ api-gateway     │ service │ web     │ nginx            │      2 │    2000 │ 386 (-81%)    │    1024 │ 134 (-87%)   │
│ 2 │ global │ dc1     │ default   │ api-gateway     │ service │ web     │ envoy            │      2 │    1000 │ 1247 (+25%)   │     512 │ 2120 (+314%) │
│ 3 │ global │ dc1     │ default   │ backend-api     │ service │ app     │ server           │      1 │     500 │ 350 (-30%)    │     256 │ 192 (-25%)   │
│ 4 │ global │ dc1     │ default   │ backend-api     │ service │ app     │ metrics-exporter │      1 │     100 │ 1320 (+1220%) │      64 │ 949 (+1383%) │
│ 5 │ global │ dc1,dc2 │ data      │ batch-processor │ batch   │ workers │ processor        │      3 │    4000 │ 980 (-76%)    │    2048 │ 2793 (+36%)  │
│ 6 │ global │ dc1,dc2 │ data      │ batch-processor │ batch   │ workers │ scheduler        │      3 │     200 │ 206 (+3%)     │     128 │ 1009 (+688%) │
│ 7 │ global │ dc1     │ data      │ ml-inference    │ service │ serving │ model-server     │      2 │    1000 │ 1494 (+49%)   │    2048 │ 2540 (+24%)  │
│ 8 │ global │ dc1     │ data      │ ml-inference    │ service │ serving │ feature-store    │      2 │     500 │ 1586 (+217%)  │     512 │ 1662 (+225%) │
╰───┴────────┴─────────┴───────────┴─────────────────┴─────────┴─────────┴──────────────────┴────────┴─────────┴───────────────┴─────────┴──────────────╯

  8 task(s)  ·  ↓ 3 can be downsized  ·  ↑ 5 need more resources  ·  ✓ 0 well-sized
```

---

## Features

- **Read-only** — never modifies your job specs, only shows recommendations
- **Two metric sources** — Nomad's native telemetry or cAdvisor (Docker driver)
- **Prometheus & VictoriaMetrics** — any Prometheus-compatible backend works
- **TLS / mTLS support** — connect to TLS-enabled Nomad clusters with CA and client certificates
- **ACL support** — pass a Nomad token via `--nomad-token` or `NOMAD_TOKEN`
- **All-namespace scanning** — use `--namespace '*'` to scan every namespace at once
- **Flexible output** — table, JSON, YAML, CSV
- **Configurable strategy** — percentile, buffer %, and minimum floors
- **Mock mode** — try it without a live cluster

---

## Installation

```bash
git clone https://github.com/eduardolmedeiros/nrr
cd nrr
go build -o nrr .
```

Requires Go 1.22+.

---

## Prerequisites

### Nomad native metrics (recommended)

Add the following to your Nomad client configuration and reload:

```hcl
telemetry {
  collection_interval        = "1s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
}
```

Then configure Prometheus to scrape the Nomad metrics endpoint (default `:4646/v1/metrics?format=prometheus`).

### cAdvisor metrics

Deploy cAdvisor on each Nomad client node and configure Prometheus to scrape it. Only works for tasks using the **Docker task driver**.

---

## Quick Start

```bash
# Try with synthetic data — no cluster needed
./nrr --metrics-source mock

# Real cluster, default settings (Nomad native metrics, 7-day window)
./nrr \
  --nomad-address http://nomad.example.com:4646 \
  --prometheus-address http://prometheus.example.com:9090

# cAdvisor source, VictoriaMetrics backend
./nrr \
  --metrics-source cadvisor \
  --prometheus-address http://victoria-metrics.example.com:8428

# Scan all namespaces at once
./nrr --namespace '*'

# With a Nomad ACL token
./nrr --nomad-token "$(cat ~/.nomad-token)"
# or via environment variable
export NOMAD_TOKEN="s3cr3t"
./nrr

# TLS-enabled cluster (flags)
./nrr \
  --nomad-address https://nomad.example.com:4646 \
  --nomad-ca-cert /path/to/nomad-agent-ca.pem \
  --nomad-client-cert /path/to/global-cli-nomad.pem \
  --nomad-client-key /path/to/global-cli-nomad-key.pem

# TLS via standard environment variables (same vars as the Nomad CLI)
export NOMAD_ADDR=https://nomad.example.com:4646
export NOMAD_CACERT=/path/to/nomad-agent-ca.pem
export NOMAD_CLIENT_CERT=/path/to/global-cli-nomad.pem
export NOMAD_CLIENT_KEY=/path/to/global-cli-nomad-key.pem
./nrr

# Skip TLS verification (dev/test only)
./nrr --nomad-tls-insecure

# Filter to a single job, JSON output
./nrr --job my-api --output json

# Pipe-friendly (no colour codes)
./nrr --no-color | tee recommendations.txt

# Export to CSV
./nrr --output csv > recommendations.csv

# Debug: print the PromQL queries being sent
./nrr --debug
```

---

## Configuration

NRR supports three layers of configuration, in order of precedence:

```
CLI flags  >  environment variables  >  config file  >  defaults
```

### Config file

NRR looks for a config file in these locations (first match wins):

1. `--config /path/to/config.yaml` (explicit override)
2. `.nrr/config.yaml` in the current directory (project-level)
3. `$XDG_CONFIG_HOME/nrr/config.yaml` — typically `~/.config/nrr/config.yaml` on Linux, `~/Library/Application Support/nrr/config.yaml` on macOS (user-level)

Example `.nrr/config.yaml`:

```yaml
nomad:
  address: https://nomad.example.com:4646
  tls:
    ca_cert: /path/to/nomad-agent-ca.pem
    client_cert: /path/to/global-cli-nomad.pem
    client_key: /path/to/global-cli-nomad-key.pem

prometheus:
  address: http://prometheus.example.com:9090

metrics_source: nomad-native
namespace: default
window: 7d

strategy:
  cpu_percentile: 99
  mem_percentile: 99
  cpu_buffer: 15
  mem_buffer: 15
  min_cpu: 10
  min_memory: 64

output:
  format: table
  no_color: false
  debug: false
```

---

## All Flags

| Flag | Config key | Env var | Default | Description |
|------|-----------|---------|---------|-------------|
| `--config` | — | — | _(see above)_ | Explicit path to config file |
| `--nomad-address` | `nomad.address` | `NOMAD_ADDR` | `http://localhost:4646` | Nomad API address |
| `--nomad-token` | `nomad.token` | `NOMAD_TOKEN` | — | Nomad ACL token (SecretID) |
| `--nomad-ca-cert` | `nomad.tls.ca_cert` | `NOMAD_CACERT` | — | Path to CA certificate for Nomad TLS |
| `--nomad-client-cert` | `nomad.tls.client_cert` | `NOMAD_CLIENT_CERT` | — | Path to client certificate for Nomad mTLS |
| `--nomad-client-key` | `nomad.tls.client_key` | `NOMAD_CLIENT_KEY` | — | Path to client key for Nomad mTLS |
| `--nomad-tls-insecure` | `nomad.tls.insecure` | — | `false` | Skip TLS certificate verification (not recommended in production) |
| `--prometheus-address` | `prometheus.address` | — | `http://localhost:9090` | Prometheus or VictoriaMetrics address |
| `--metrics-source` | `metrics_source` | — | `nomad-native` | `nomad-native` \| `cadvisor` \| `mock` |
| `--namespace` | `namespace` | — | `default` | Nomad namespace to scan (`*` for all namespaces) |
| `--job` | `job` | — | _(all jobs)_ | Filter to a specific job name |
| `--window` | `window` | — | `7d` | Historical data window (`1d`, `3d`, `7d`, `14d`, …) |
| `--cpu-percentile` | `strategy.cpu_percentile` | — | `99` | CPU percentile to use for recommendations |
| `--mem-percentile` | `strategy.mem_percentile` | — | `99` | Memory percentile to use for recommendations |
| `--cpu-buffer` | `strategy.cpu_buffer` | — | `15` | Buffer % added on top of the CPU recommendation |
| `--mem-buffer` | `strategy.mem_buffer` | — | `15` | Buffer % added on top of the memory recommendation |
| `--min-cpu` | `strategy.min_cpu` | — | `10` | Minimum recommended CPU (MHz) |
| `--min-memory` | `strategy.min_memory` | — | `64` | Minimum recommended memory (MB) |
| `-o`, `--output` | `output.format` | — | `table` | Output format: `table` \| `json` \| `yaml` \| `csv` |
| `--no-color` | `output.no_color` | — | `false` | Disable ANSI colours (for piping / log files) |
| `--debug` | `output.debug` | — | `false` | Print PromQL queries before executing them |

---

## How Recommendations Work

See [docs/recommendations.md](docs/recommendations.md) for a full explanation of the metric queries, PromQL used, the P99+buffer strategy, and what each output column means.

**Short version:**

```
recommended_cpu    = P99(cpu_samples)    × (1 + cpu_buffer)
recommended_memory = P99(memory_samples) × (1 + mem_buffer)
```

Both values are clamped to their configured minimums.

---

## Project Structure

```
nrr/
├── main.go
├── cmd/
│   ├── root.go                    # CLI flags and orchestration
│   └── diagnose.go                # `nrr diagnose` — probe Prometheus label availability
├── internal/
│   ├── nomad/
│   │   └── client.go              # Nomad API: discover jobs → groups → tasks
│   ├── metrics/
│   │   ├── promclient.go          # Shared Prometheus-compatible HTTP client
│   │   ├── nomadnative/           # nomad_client_allocs_* metrics adapter
│   │   ├── cadvisor/              # container_* metrics adapter (Docker only)
│   │   └── mock/                  # Deterministic fake data for development
│   ├── recommender/
│   │   └── recommender.go         # Strategy engine + MetricsBackend interface
│   └── output/
│       ├── formatter.go           # Formatter interface + factory
│       ├── table.go               # Pretty terminal table
│       ├── json.go
│       ├── yaml.go
│       └── csv.go
└── docs/
    └── recommendations.md         # Deep-dive on metrics and strategy
```

---

## Roadmap

- [ ] Unit tests for the recommender engine
- [ ] Apply mode — generate updated job HCL snippets
- [ ] GitHub Actions CI

---

## License

MIT
