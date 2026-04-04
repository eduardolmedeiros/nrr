# How NRR Generates Recommendations

NRR (Nomad Resource Recommender) queries historical metrics from Prometheus or
VictoriaMetrics, applies a percentile-based strategy, and compares the result
against the resource values currently declared in each Nomad job specification.

---

## 1. Task Discovery

NRR connects to the Nomad API and walks the job hierarchy:

```
Job → Task Group → Task
```

For each task it records the **currently declared** resources from the job spec:

| Field        | Nomad HCL key   | Unit |
|--------------|-----------------|------|
| CPU          | `cpu`           | MHz  |
| Memory       | `memory`        | MB   |

These are the values shown in the **"now"** columns of the output and used to
compute the diff.

---

## 2. Metrics Sources

NRR supports two metric schemas, selectable with `--metrics-source`.

### `nomad-native` (default)

Uses metrics published by Nomad's own telemetry system.

**Required Nomad configuration:**

```hcl
telemetry {
  collection_interval        = "1s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
}
```

| Resource | Prometheus metric                        | Unit  | Notes                                      |
|----------|------------------------------------------|-------|--------------------------------------------|
| CPU      | `nomad_client_allocs_cpu_total_ticks`    | MHz   | Gauge — instantaneous CPU consumed         |
| Memory   | `nomad_client_allocs_memory_rss`         | bytes | Gauge — RSS memory, converted to MB        |

**Label dimensions:** `job`, `task_group`, `task`, `alloc_id`, `namespace`

Because Nomad creates a new `alloc_id` each time a task is restarted or
rescheduled, NRR aggregates across all allocation IDs using:

```promql
max by (job, task_group, task) (nomad_client_allocs_cpu_total_ticks{...})
```

This ensures the full history is captured even across restarts.

---

### `cadvisor`

Uses container metrics from [cAdvisor](https://github.com/google/cadvisor).
**Only works for tasks using the Docker task driver.** Tasks running under
`exec`, `raw_exec`, `java`, or other non-Docker drivers will produce no data
with this source.

When Nomad launches a Docker container it attaches metadata as Docker labels,
which Prometheus exposes as metric labels:

| Docker label                                  | Prometheus label                                                    |
|-----------------------------------------------|---------------------------------------------------------------------|
| `com.hashicorp.nomad.job_name`                | `container_label_com_hashicorp_nomad_job_name`                      |
| `com.hashicorp.nomad.task_group_name`         | `container_label_com_hashicorp_nomad_task_group_name`               |
| `com.hashicorp.nomad.task_name`               | `container_label_com_hashicorp_nomad_task_name`                     |

| Resource | Prometheus metric                        | Unit    | Notes                                                                 |
|----------|------------------------------------------|---------|-----------------------------------------------------------------------|
| CPU      | `container_cpu_usage_seconds_total`      | seconds | Counter — `irate(...[5m])` gives cores; converted to MHz via `--cadvisor-mhz-per-core` |
| Memory   | `container_memory_working_set_bytes`     | bytes   | Gauge — working set excludes inactive file cache; what the OOM killer uses |

**CPU unit conversion:** cAdvisor reports CPU in fractional cores. NRR
multiplies by `--cadvisor-mhz-per-core` (default: `1000`) to convert to MHz,
matching Nomad's unit. Set this flag to your actual CPU clock speed for
accurate results (e.g. `--cadvisor-mhz-per-core 2400` for a 2.4 GHz node).

---

## 3. Query Window

Controlled by `--window` (default: `7d`). NRR issues a range query against
the configured backend, collecting one data point every **5 minutes** over
the full window. A 7-day window yields roughly **2,016 samples** per task.

Longer windows smooth out weekly traffic patterns (e.g. lower weekend load)
but require longer metric retention in Prometheus.

Recommended values:

| Scenario                          | Window  |
|-----------------------------------|---------|
| Stable, predictable workloads     | `3d`    |
| Normal services                   | `7d`    |
| Weekly batch or cron jobs         | `14d`   |
| Highly variable / seasonal load   | `30d`   |

---

## 4. Recommendation Strategy

### CPU

```
recommended_cpu = percentile(samples, P) × (1 + buffer)
```

| Parameter          | Flag                 | Default |
|--------------------|----------------------|---------|
| Percentile (P)     | `--cpu-percentile`   | `99`    |
| Buffer             | `--cpu-buffer`       | `15%`   |
| Minimum            | `--min-cpu`          | `10 MHz`|

**Why P99?** Setting the CPU request at the 99th percentile means the task
has sufficient CPU in 99% of observed intervals. The 15% buffer absorbs
short bursts not captured in the scrape interval and gives headroom for
gradual growth. Nomad does not enforce a hard CPU limit by default, so the
request is primarily a scheduling signal.

### Memory

```
recommended_memory = percentile(samples, P) × (1 + buffer)
```

| Parameter          | Flag                 | Default  |
|--------------------|----------------------|----------|
| Percentile (P)     | `--mem-percentile`   | `99`     |
| Buffer             | `--mem-buffer`       | `15%`    |
| Minimum            | `--min-memory`       | `64 MB`  |

**Why P99 for memory too?** Unlike CPU, memory is not compressible — exceeding
the declared limit triggers an OOM kill. Using P99 ensures the task survives
its highest observed usage 99% of the time. The buffer provides safety margin
for growth. If your tasks have predictable memory growth patterns you may want
to raise `--mem-percentile` to `100` (i.e. max observed).

### Floors

Both CPU and memory recommendations are clamped to their respective minimums
(`--min-cpu`, `--min-memory`) regardless of how low the percentile value is.
This prevents accidentally setting unrealistically small values on tasks with
very low but non-zero usage.

---

## 5. Output Columns

| Column       | Description                                                        |
|--------------|--------------------------------------------------------------------|
| `now`        | Currently declared value in the job spec                           |
| `rec`        | Recommended value from NRR                                         |
| `Δ (diff)`   | `rec − now` — negative means savings, positive means needs more    |
| `%`          | Percentage change: `(rec − now) / now × 100`                      |

**Colour coding (table output):**

- 🟢 Green — recommendation is lower than current; resources can be reclaimed
- 🟡 Yellow — recommendation is higher than current; task may be under-provisioned
- Dim — no change recommended

---

## 6. What NRR Does NOT Do

- NRR is **read-only**. It never modifies job specifications.
- NRR does not account for future traffic growth — it recommends based purely
  on historical data within the configured window.
- NRR does not consider Nomad scheduling constraints or bin-packing efficiency.
- Recommendations assume metric data is representative. Tasks that rarely run
  or have very few samples may produce unreliable results (check the
  `SAMPLES` column).
