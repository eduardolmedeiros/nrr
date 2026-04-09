# NRR — Nomad Resource Recommender

Analyses historical CPU and memory usage from Prometheus and recommends right-sized resource specs for your Nomad jobs. Inspired by [KRR](https://github.com/robusta-dev/krr) for Kubernetes.

```
╭───┬────────┬─────────┬───────────┬─────────────────┬─────────┬─────────┬──────────────────┬────────┬─────────┬───────────────┬─────────┬──────────────╮
│ # │ REGION │ DC      │ NAMESPACE │ JOB             │ TYPE    │ GROUP   │ TASK             │ ALLOCS │ CPU NOW │ CPU REC       │ MEM NOW │ MEM REC      │
├───┼────────┼─────────┼───────────┼─────────────────┼─────────┼─────────┼──────────────────┼────────┼─────────┼───────────────┼─────────┼──────────────┤
│ 1 │ global │ dc1     │ default   │ api-gateway     │ service │ web     │ nginx            │      2 │    2000 │ 386 (-81%)    │    1024 │ 134 (-87%)   │
│ 2 │ global │ dc1     │ default   │ api-gateway     │ service │ web     │ envoy            │      2 │    1000 │ 1247 (+25%)   │     512 │ 2120 (+314%) │
│ 3 │ global │ dc1     │ default   │ backend-api     │ service │ app     │ server           │      1 │     500 │ 350 (-30%)    │     256 │ 192 (-25%)   │
│ 4 │ global │ dc1,dc2 │ data      │ batch-processor │ batch   │ workers │ processor        │      3 │    4000 │ 980 (-76%)    │    2048 │ 2793 (+36%)  │
╰───┴────────┴─────────┴───────────┴─────────────────┴─────────┴─────────┴──────────────────┴────────┴─────────┴───────────────┴─────────┴──────────────╯

  8 task(s)  ·  ↓ 3 can be downsized  ·  ↑ 5 need more resources  ·  ✓ 0 well-sized
```

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

Add the following to your Nomad configuration and reload:

```hcl
telemetry {
  collection_interval        = "1s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
}
```

Then configure Prometheus to scrape `:4646/v1/metrics?format=prometheus`. Use `honor_labels: true` in the scrape config.

For cAdvisor support (Docker driver only), deploy cAdvisor on each Nomad client node and scrape it with Prometheus.

---

## Quick Start

```bash
# Try without a cluster
./nrr --metrics-source mock

# Real cluster
./nrr \
  --nomad-address http://nomad.example.com:4646 \
  --prometheus-address http://prometheus.example.com:9090

# TLS cluster — or set NOMAD_ADDR / NOMAD_CACERT / NOMAD_CLIENT_CERT / NOMAD_CLIENT_KEY
./nrr \
  --nomad-address https://nomad.example.com:4646 \
  --nomad-ca-cert /path/to/nomad-agent-ca.pem \
  --nomad-client-cert /path/to/client.pem \
  --nomad-client-key /path/to/client-key.pem

# Scan all namespaces, export to CSV
./nrr --namespace '*' --output csv > recommendations.csv

# Debug: show PromQL queries + per-task calculation breakdown
./nrr --debug
```

---

## Configuration

Precedence: `CLI flags > env vars > config file > defaults`

NRR looks for a config file in order:
1. `--config /path/to/config.yaml`
2. `.nrr/config.yaml` (project-level)
3. `$XDG_CONFIG_HOME/nrr/config.yaml` (user-level, e.g. `~/.config/nrr/config.yaml`)

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

output:
  format: table
```

---

## All Flags

| Flag | Config key | Env var | Default | Description |
|------|-----------|---------|---------|-------------|
| `--config` | — | — | _(see above)_ | Explicit path to config file |
| `--nomad-address` | `nomad.address` | `NOMAD_ADDR` | `http://localhost:4646` | Nomad API address |
| `--nomad-token` | `nomad.token` | `NOMAD_TOKEN` | — | Nomad ACL token |
| `--nomad-ca-cert` | `nomad.tls.ca_cert` | `NOMAD_CACERT` | — | CA certificate for TLS |
| `--nomad-client-cert` | `nomad.tls.client_cert` | `NOMAD_CLIENT_CERT` | — | Client certificate for mTLS |
| `--nomad-client-key` | `nomad.tls.client_key` | `NOMAD_CLIENT_KEY` | — | Client key for mTLS |
| `--nomad-tls-insecure` | `nomad.tls.insecure` | — | `false` | Skip TLS verification |
| `--prometheus-address` | `prometheus.address` | — | `http://localhost:9090` | Prometheus / VictoriaMetrics address |
| `--metrics-source` | `metrics_source` | — | `nomad-native` | `nomad-native` \| `cadvisor` \| `mock` |
| `--namespace` | `namespace` | — | `default` | Nomad namespace (`*` for all) |
| `--job` | `job` | — | _(all)_ | Filter to a specific job |
| `--window` | `window` | — | `7d` | Historical data window |
| `--cpu-percentile` | `strategy.cpu_percentile` | — | `99` | CPU percentile (0–100) |
| `--mem-percentile` | `strategy.mem_percentile` | — | `99` | Memory percentile (0–100) |
| `--cpu-buffer` | `strategy.cpu_buffer` | — | `15` | Buffer % on top of CPU recommendation |
| `--mem-buffer` | `strategy.mem_buffer` | — | `15` | Buffer % on top of memory recommendation |
| `--min-cpu` | `strategy.min_cpu` | — | `10` | Minimum CPU (MHz) |
| `--min-memory` | `strategy.min_memory` | — | `64` | Minimum memory (MB) |
| `-o`, `--output` | `output.format` | — | `table` | `table` \| `json` \| `yaml` \| `csv` |
| `--no-color` | `output.no_color` | — | `false` | Disable ANSI colours |
| `--debug` | `output.debug` | — | `false` | Show PromQL queries and calculation breakdown |

See [docs/recommendations.md](docs/recommendations.md) for details on the recommendation strategy.

---

## License

MIT
