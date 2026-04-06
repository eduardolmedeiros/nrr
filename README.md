# NRR — Nomad Resource Recommender

NRR analyses historical CPU and memory usage from Prometheus (or VictoriaMetrics) and recommends right-sized resource specs for your HashiCorp Nomad jobs.

Inspired by [KRR](https://github.com/robusta-dev/krr) for Kubernetes.

```
  NRR — Nomad Resource Recommender

╭───────────┬─────────────────┬─────────┬──────────────────┬────────────────────────┬────────────────────────╮
│ NAMESPACE │ JOB             │ GROUP   │ TASK             │ CPU (MHz)              │ MEMORY (MB)            │
├───────────┼─────────────────┼─────────┼──────────────────┼────────────────────────┼────────────────────────┤
│ default   │ api-gateway     │ web     │ nginx            │ 2000 → 240  -88%       │ 1024 → 205  -80%       │
│ default   │ api-gateway     │ web     │ envoy            │ 1000 → 318  -68%       │  512 → 179  -65%       │
│ default   │ backend-api     │ app     │ server           │  500 → 345  -31%       │  256 → 191  -25%       │
│ data      │ ml-inference    │ serving │ model-server     │ 1000 → 1127  +13%      │ 2048 → 2254  +10%      │
╰───────────┴─────────────────┴─────────┴──────────────────┴────────────────────────┴────────────────────────╯

  8 task(s)  ·  6 can be downsized  ·  2 need more resources
```

---

## Features

- **Read-only** — never modifies your job specs, only shows recommendations
- **Two metric sources** — Nomad's native telemetry or cAdvisor (Docker driver)
- **Prometheus & VictoriaMetrics** — any Prometheus-compatible backend works
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

# Filter to a single job, JSON output
./nrr --job my-api --output json

# Pipe-friendly (no colour codes)
./nrr --no-color | tee recommendations.txt

# Export to CSV
./nrr --output csv > recommendations.csv
```

---

## All Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--nomad-address` | `http://localhost:4646` | Nomad API address |
| `--prometheus-address` | `http://localhost:9090` | Prometheus or VictoriaMetrics address |
| `--metrics-source` | `nomad-native` | `nomad-native` \| `cadvisor` \| `mock` |
| `--namespace` | `default` | Nomad namespace to scan (`*` for all) |
| `--job` | _(all jobs)_ | Filter to a specific job name |
| `--window` | `7d` | Historical data window (`1d`, `3d`, `7d`, `14d`, …) |
| `--cpu-percentile` | `99` | CPU percentile to use for recommendations |
| `--mem-percentile` | `99` | Memory percentile to use for recommendations |
| `--cpu-buffer` | `15` | Buffer % added on top of the CPU recommendation |
| `--mem-buffer` | `15` | Buffer % added on top of the memory recommendation |
| `--min-cpu` | `10` | Minimum recommended CPU (MHz) |
| `--min-memory` | `64` | Minimum recommended memory (MB) |
| `-o`, `--output` | `table` | Output format: `table` \| `json` \| `yaml` \| `csv` |
| `--no-color` | `false` | Disable ANSI colours (for piping / log files) |

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
│   └── root.go                    # CLI flags and orchestration
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
