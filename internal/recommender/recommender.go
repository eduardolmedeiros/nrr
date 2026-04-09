// Package recommender computes right-sized resource recommendations for Nomad tasks.
package recommender

import (
	"context"
	"sort"
	"time"

	"github.com/nrr-project/nrr/internal/nomad"
)

// MetricsBackend is the interface that metrics adapters (nomad-native, cadvisor)
// must implement. Each method returns a slice of observed values over the window.
//
// For CPU: values are in MHz.
// For Memory: values are in MB.
//
// The full TaskSpec is passed so adapters can choose their preferred label
// strategy — e.g. cAdvisor can fall back to AllocIDs when job-name labels
// are not present on container metrics.
type MetricsBackend interface {
	QueryCPU(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error)
	QueryMemory(ctx context.Context, task nomad.TaskSpec, window time.Duration) ([]float64, error)
}

// Config controls the recommendation strategy.
type Config struct {
	// CPUPercentile is the quantile (0.0–1.0) of observed CPU samples used
	// for the recommendation. Default: 0.99 (P99).
	CPUPercentile float64

	// MemPercentile is the quantile (0.0–1.0) of observed memory samples used
	// for the recommendation. Default: 0.99 (P99).
	MemPercentile float64

	// CPUBuffer is the fractional buffer added on top of the percentile value.
	// E.g., 0.15 adds 15%. Default: 0.15.
	CPUBuffer float64

	// MemBuffer is the fractional buffer added on top of the percentile value.
	// E.g., 0.15 adds 15%. Default: 0.15.
	MemBuffer float64

	// MinCPUMHz is the floor for CPU recommendations (MHz). Default: 10.
	MinCPUMHz int

	// MinMemoryMB is the floor for memory recommendations (MB). Default: 64.
	MinMemoryMB int
}

// Recommendation is the result for a single Nomad task.
type Recommendation struct {
	Task nomad.TaskSpec

	// Current declared resources from the job spec.
	CurrentCPUMHz   int
	CurrentMemoryMB int

	// Recommended values.
	RecommendedCPUMHz   int
	RecommendedMemoryMB int

	// Diff: positive = we recommend more, negative = we recommend less.
	CPUDiffMHz   int
	MemDiffMB    int

	// How many data points were available.
	CPUSamples int
	MemSamples int
}

// Recommend computes a recommendation for a single task given its observed
// CPU and memory samples.
func Recommend(task nomad.TaskSpec, cpuSamples, memSamples []float64, cfg Config) Recommendation {
	rec := Recommendation{
		Task:            task,
		CurrentCPUMHz:   task.CPUMHz,
		CurrentMemoryMB: task.MemoryMB,
		CPUSamples:      len(cpuSamples),
		MemSamples:      len(memSamples),
	}

	// CPU recommendation
	if len(cpuSamples) > 0 {
		p := percentile(cpuSamples, cfg.CPUPercentile)
		recommended := int(p * (1 + cfg.CPUBuffer))
		if recommended < cfg.MinCPUMHz {
			recommended = cfg.MinCPUMHz
		}
		rec.RecommendedCPUMHz = recommended
	} else {
		// No data: keep current
		rec.RecommendedCPUMHz = task.CPUMHz
	}

	// Memory recommendation
	if len(memSamples) > 0 {
		p := percentile(memSamples, cfg.MemPercentile)
		recommended := int(p * (1 + cfg.MemBuffer))
		if recommended < cfg.MinMemoryMB {
			recommended = cfg.MinMemoryMB
		}
		rec.RecommendedMemoryMB = recommended
	} else {
		// No data: keep current
		rec.RecommendedMemoryMB = task.MemoryMB
	}

	rec.CPUDiffMHz = rec.RecommendedCPUMHz - rec.CurrentCPUMHz
	rec.MemDiffMB = rec.RecommendedMemoryMB - rec.CurrentMemoryMB

	return rec
}

// Percentile returns the p-th quantile (0.0–1.0) of the given sample slice.
// The input slice is not modified.
func Percentile(samples []float64, p float64) float64 {
	return percentile(samples, p)
}

// percentile returns the p-th quantile (0.0–1.0) of the given sample slice.
// The slice is sorted in-place.
func percentile(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)

	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}

	idx := p * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	// Linear interpolation
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
