// Package cadvisor implements a MetricsBackend using cAdvisor container metrics.
//
// This adapter works when Nomad tasks run via the Docker driver. cAdvisor
// attaches Nomad job metadata as Docker labels, which Prometheus exposes as:
//
//	container_label_com_hashicorp_nomad_job_name
//	container_label_com_hashicorp_nomad_task_group_name
//	container_label_com_hashicorp_nomad_task_name
//	container_label_com_hashicorp_nomad_alloc_id
//
// For tasks using exec, raw_exec, java, or other non-Docker drivers, this
// adapter will return empty samples — use the nomad-native adapter instead.
//
// Metrics used:
//   - container_cpu_usage_seconds_total  (counter — rate gives cores used)
//   - container_memory_working_set_bytes (gauge — bytes)
//
// CPU conversion: cAdvisor reports in CPU cores (fractional). We convert to MHz
// using a configurable MHz-per-core value (default: 1000 MHz/core). For accurate
// results, set --cadvisor-mhz-per-core to match your actual node CPU frequency.
package cadvisor

import (
	"context"
	"fmt"
	"time"

	"github.com/nrr-project/nrr/internal/metrics"
)

const (
	defaultStep      = 5 * time.Minute
	defaultMHzPerCore = 1000.0 // conservative default; tune to your hardware
	bytesPerMB        = 1024 * 1024
)

// Adapter queries cAdvisor metrics from a Prometheus-compatible backend.
type Adapter struct {
	client     *metrics.PromClient
	mhzPerCore float64
}

// New creates a new cAdvisor metrics adapter.
// address should point at Prometheus or VictoriaMetrics.
// mhzPerCore converts CPU cores (cAdvisor unit) to MHz (Nomad unit).
// Pass 0 to use the default (1000 MHz/core).
func New(address string, mhzPerCore ...float64) (*Adapter, error) {
	client, err := metrics.NewPromClient(address)
	if err != nil {
		return nil, fmt.Errorf("cadvisor: %w", err)
	}

	mhz := defaultMHzPerCore
	if len(mhzPerCore) > 0 && mhzPerCore[0] > 0 {
		mhz = mhzPerCore[0]
	}

	return &Adapter{client: client, mhzPerCore: mhz}, nil
}

// QueryCPU returns CPU usage samples in MHz for the given task over the window.
//
// cAdvisor's container_cpu_usage_seconds_total is a monotonically increasing
// counter; irate() over a 5m window gives instantaneous CPU usage in cores.
// We multiply by mhzPerCore to convert to MHz (matching Nomad's CPU unit).
//
// We aggregate across alloc_ids using max by(job, task_group, task) to handle
// allocation restarts correctly.
func (a *Adapter) QueryCPU(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`max by (container_label_com_hashicorp_nomad_job_name, container_label_com_hashicorp_nomad_task_group_name, container_label_com_hashicorp_nomad_task_name) (`+
			`irate(container_cpu_usage_seconds_total{`+
			`container_label_com_hashicorp_nomad_job_name=%q,`+
			`container_label_com_hashicorp_nomad_task_group_name=%q,`+
			`container_label_com_hashicorp_nomad_task_name=%q`+
			`}[5m])`+
			`)`,
		job, group, task,
	)

	samples, err := a.client.QueryRange(ctx, query, window, defaultStep)
	if err != nil {
		return nil, fmt.Errorf("cadvisor CPU query for %s/%s/%s: %w", job, group, task, err)
	}

	// Convert cores → MHz
	for i := range samples {
		samples[i] = samples[i] * a.mhzPerCore
	}

	return samples, nil
}

// QueryMemory returns memory usage samples in MB for the given task over the window.
//
// container_memory_working_set_bytes is a gauge (bytes). We convert to MB.
// working_set_bytes excludes inactive file-backed pages and is the value that
// the OOM killer uses — making it the right signal for right-sizing.
func (a *Adapter) QueryMemory(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`max by (container_label_com_hashicorp_nomad_job_name, container_label_com_hashicorp_nomad_task_group_name, container_label_com_hashicorp_nomad_task_name) (`+
			`container_memory_working_set_bytes{`+
			`container_label_com_hashicorp_nomad_job_name=%q,`+
			`container_label_com_hashicorp_nomad_task_group_name=%q,`+
			`container_label_com_hashicorp_nomad_task_name=%q`+
			`}`+
			`)`,
		job, group, task,
	)

	samples, err := a.client.QueryRange(ctx, query, window, defaultStep)
	if err != nil {
		return nil, fmt.Errorf("cadvisor memory query for %s/%s/%s: %w", job, group, task, err)
	}

	// Convert bytes → MB
	for i := range samples {
		samples[i] = samples[i] / bytesPerMB
	}

	return samples, nil
}
