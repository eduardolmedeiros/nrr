// Package cadvisor implements a MetricsBackend using cAdvisor container metrics.
//
// Label strategy:
//
// Nomad attaches container metadata as Docker labels, but the exact label set
// varies by version and configuration. NRR tries two strategies in order:
//
//  1. Filter by alloc_id (container_label_com_hashicorp_nomad_alloc_id).
//     Works whenever Nomad is running tasks via the Docker driver, regardless
//     of which other labels are present. Alloc IDs are fetched from the Nomad
//     API and stored in TaskSpec.AllocIDs.
//
//  2. Filter by job/task_group/task name labels (fallback for older setups
//     where alloc_id is not labelled but name labels are).
//
// For tasks using exec, raw_exec, java, or other non-Docker drivers, this
// adapter will return empty samples — use the nomad-native adapter instead.
//
// Metrics used:
//   - container_cpu_usage_seconds_total  (counter — rate gives cores used)
//   - container_memory_working_set_bytes (gauge — bytes)
package cadvisor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nrr-project/nrr/internal/metrics"
	"github.com/nrr-project/nrr/internal/nomad"
)

const (
	defaultMHzPerCore = 1000.0
	bytesPerMB        = 1024 * 1024
)

// Adapter queries cAdvisor metrics from a Prometheus-compatible backend.
type Adapter struct {
	client     *metrics.PromClient
	mhzPerCore float64
}

// New creates a new cAdvisor metrics adapter.
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

// SetDebug enables PromQL query logging to stderr.
func (a *Adapter) SetDebug(v bool) { a.client.Debug = v }

// QueryCPU returns CPU usage samples in MHz for the given task over the window.
// Uses alloc_id label filtering when AllocIDs are available (preferred),
// falling back to job/task_group/task name labels.
func (a *Adapter) QueryCPU(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`rate(container_cpu_usage_seconds_total{%s}[5m])`,
		labelSelector(task),
	)

	samples, err := a.client.QueryRange(ctx, query, window)
	if err != nil {
		return nil, fmt.Errorf("cadvisor CPU query for %s/%s/%s: %w", task.Job, task.Group, task.Task, err)
	}

	for i := range samples {
		samples[i] = samples[i] * a.mhzPerCore
	}
	return samples, nil
}

// QueryMemory returns memory usage samples in MB for the given task over the window.
func (a *Adapter) QueryMemory(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`container_memory_working_set_bytes{%s}`,
		labelSelector(task),
	)

	samples, err := a.client.QueryRange(ctx, query, window)
	if err != nil {
		return nil, fmt.Errorf("cadvisor memory query for %s/%s/%s: %w", task.Job, task.Group, task.Task, err)
	}

	for i := range samples {
		samples[i] = samples[i] / bytesPerMB
	}
	return samples, nil
}

// labelSelector builds the Prometheus label selector for a task.
// Prefers alloc_id filtering (works when name labels are absent) and falls
// back to name-based labels when no alloc IDs are available.
func labelSelector(task nomad.TaskSpec) string {
	if len(task.AllocIDs) > 0 {
		// Use alloc_id regex match — handles multiple running allocations.
		ids := strings.Join(task.AllocIDs, "|")
		return fmt.Sprintf(
			`container_label_com_hashicorp_nomad_alloc_id=~%q`,
			ids,
		)
	}

	// Fallback: filter by name labels (older Nomad / different cAdvisor config).
	return fmt.Sprintf(
		`container_label_com_hashicorp_nomad_job_name=%q,`+
			`container_label_com_hashicorp_nomad_task_group_name=%q,`+
			`container_label_com_hashicorp_nomad_task_name=%q`,
		task.Job, task.Group, task.Task,
	)
}
