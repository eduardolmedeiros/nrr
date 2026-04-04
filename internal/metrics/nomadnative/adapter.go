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
)

const (
	// defaultStep is the resolution used for range queries.
	// 5m balances precision vs query cost.
	defaultStep = 5 * time.Minute

	// bytesPerMB for unit conversion.
	bytesPerMB = 1024 * 1024
)

// Adapter queries Nomad-native Prometheus metrics.
type Adapter struct {
	client *metrics.PromClient
}

// New creates a new Nomad-native metrics adapter.
// address should point at Prometheus or VictoriaMetrics (they share the same API).
func New(address string) (*Adapter, error) {
	client, err := metrics.NewPromClient(address)
	if err != nil {
		return nil, fmt.Errorf("nomadnative: %w", err)
	}
	return &Adapter{client: client}, nil
}

// QueryCPU returns CPU usage samples in MHz for the given task over the window.
//
// We use a range query with irate() to convert the cumulative tick counter to a
// rate, then collect the samples for local percentile computation in the
// recommender. Aggregating with `max by (job, task_group, task)` collapses all
// alloc_ids so we get a single time-series even as allocations restart.
func (a *Adapter) QueryCPU(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	// nomad_client_allocs_cpu_total_ticks is reported as a gauge in MHz.
	// We use max_over_time across alloc_ids (ephemeral) but within job/group/task.
	query := fmt.Sprintf(
		`max by (job, task_group, task) (nomad_client_allocs_cpu_total_ticks{job=%q,task_group=%q,task=%q,namespace=%q})`,
		job, group, task, namespace,
	)

	samples, err := a.client.QueryRange(ctx, query, window, defaultStep)
	if err != nil {
		return nil, fmt.Errorf("nomadnative CPU query for %s/%s/%s: %w", job, group, task, err)
	}

	return samples, nil
}

// QueryMemory returns memory usage samples in MB for the given task over the window.
//
// nomad_client_allocs_memory_rss is in bytes; we convert to MB.
// Same alloc_id aggregation strategy as QueryCPU.
func (a *Adapter) QueryMemory(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	query := fmt.Sprintf(
		`max by (job, task_group, task) (nomad_client_allocs_memory_rss{job=%q,task_group=%q,task=%q,namespace=%q})`,
		job, group, task, namespace,
	)

	samples, err := a.client.QueryRange(ctx, query, window, defaultStep)
	if err != nil {
		return nil, fmt.Errorf("nomadnative memory query for %s/%s/%s: %w", job, group, task, err)
	}

	// Convert bytes → MB
	for i := range samples {
		samples[i] = samples[i] / bytesPerMB
	}

	return samples, nil
}
