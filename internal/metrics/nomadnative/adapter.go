// Package nomadnative implements a MetricsBackend using Nomad's built-in
// Prometheus metrics (nomad_client_allocs_*).
//
// Required Nomad configuration (nomad.hcl):
//
//	telemetry {
//	  collection_interval        = "1s"
//	  disable_hostname           = true
//	  prometheus_metrics         = true
//	  publish_allocation_metrics = true
//	  publish_node_metrics       = true
//	}
//
// Metrics used:
//   - nomad_client_allocs_cpu_total_ticks  (gauge, MHz consumed)
//   - nomad_client_allocs_memory_rss       (gauge, bytes)
//
// Labels on these metrics: job, task_group, task, alloc_id, namespace.
// We aggregate across all alloc_ids for a given (job, task_group, task) tuple
// to handle Nomad's ephemeral allocation IDs correctly.
package nomadnative

import (
	"context"
	"fmt"
	"time"

	"github.com/nrr-project/nrr/internal/metrics"
	"github.com/nrr-project/nrr/internal/nomad"
)

const bytesPerMB = 1024 * 1024

// Adapter queries Nomad-native Prometheus metrics.
type Adapter struct {
	client *metrics.PromClient
}

// New creates a new Nomad-native metrics adapter.
func New(address string) (*Adapter, error) {
	client, err := metrics.NewPromClient(address)
	if err != nil {
		return nil, fmt.Errorf("nomadnative: %w", err)
	}
	return &Adapter{client: client}, nil
}

// SetDebug enables PromQL query logging to stderr.
func (a *Adapter) SetDebug(v bool) { a.client.Debug = v }

// QueryCPU returns CPU usage samples in MHz for the given task over the window.
func (a *Adapter) QueryCPU(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`max by (job, task_group, task) (nomad_client_allocs_cpu_total_ticks{job=%q,task_group=%q,task=%q,namespace=%q})`,
		task.Job, task.Group, task.Task, task.Namespace,
	)

	samples, err := a.client.QueryRange(ctx, query, window)
	if err != nil {
		return nil, fmt.Errorf("nomadnative CPU query for %s/%s/%s: %w", task.Job, task.Group, task.Task, err)
	}
	return samples, nil
}

// QueryMemory returns memory usage samples in MB for the given task over the window.
func (a *Adapter) QueryMemory(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`max by (job, task_group, task) (nomad_client_allocs_memory_rss{job=%q,task_group=%q,task=%q,namespace=%q})`,
		task.Job, task.Group, task.Task, task.Namespace,
	)

	samples, err := a.client.QueryRange(ctx, query, window)
	if err != nil {
		return nil, fmt.Errorf("nomadnative memory query for %s/%s/%s: %w", task.Job, task.Group, task.Task, err)
	}

	for i := range samples {
		samples[i] = samples[i] / bytesPerMB
	}
	return samples, nil
}
