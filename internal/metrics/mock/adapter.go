// Package mock implements a MetricsBackend that generates synthetic CPU and
// memory samples. It is useful for development, demos, and CI environments
// where no real Prometheus instance is available.
//
// Usage:
//
//	./nrr --metrics-source mock
//
// The mock generates realistic-looking data with configurable variance so you
// can see how different usage patterns affect recommendations:
//
//   - "spiky"  jobs: low average usage but occasional bursts
//   - "stable" jobs: consistent usage close to the declared spec
//   - "hungry" jobs: usage that regularly exceeds the declared spec
//
// The scenario is chosen deterministically based on the job name so results
// are reproducible across runs.
package mock

import (
	"context"
	"math"
	"math/rand"
	"time"
)

const (
	// samplesPerDay controls data density. 288 = one sample every 5 minutes.
	samplesPerDay = 288
)

// scenario describes a usage pattern for a task.
type scenario struct {
	name string
	// cpuFraction is the fraction of declared CPU the task typically uses.
	cpuFraction float64
	// memFraction is the fraction of declared memory the task typically uses.
	memFraction float64
	// cpuSpike is an occasional multiplier applied to simulate bursts (0 = none).
	cpuSpike float64
	// spikeChance is the probability (0–1) of a spike on any given sample.
	spikeChance float64
	// noise is the ±fraction of random noise added to each sample.
	noise float64
}

var scenarios = []scenario{
	{
		name:        "underutilised",
		cpuFraction: 0.12,
		memFraction: 0.20,
		cpuSpike:    1.5,
		spikeChance: 0.02,
		noise:       0.08,
	},
	{
		name:        "well-sized",
		cpuFraction: 0.60,
		memFraction: 0.65,
		cpuSpike:    1.3,
		spikeChance: 0.05,
		noise:       0.10,
	},
	{
		name:        "spiky",
		cpuFraction: 0.20,
		memFraction: 0.45,
		cpuSpike:    3.5,
		spikeChance: 0.08,
		noise:       0.15,
	},
	{
		name:        "hungry",
		cpuFraction: 0.92,
		memFraction: 0.95,
		cpuSpike:    1.2,
		spikeChance: 0.15,
		noise:       0.05,
	},
}

// Adapter is a mock MetricsBackend. It derives fake samples from each task's
// declared resource spec so that recommendations are meaningfully different
// from the current values.
type Adapter struct{}

// New creates a new mock metrics adapter.
func New() *Adapter {
	return &Adapter{}
}

// QueryCPU returns fake CPU usage samples in MHz for the given task.
// Samples are generated from the task's declared CPUMHz (passed via job name
// encoding) — callers should use a declared value of at least 10 MHz.
//
// Since the mock adapter doesn't have access to the declared spec directly,
// it uses a reasonable stand-in base value (500 MHz) scaled by the scenario.
// The cmd layer passes the full TaskSpec to the real adapters; for the mock,
// the samples are proportional to a typical Nomad task size.
func (a *Adapter) QueryCPU(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	sc := pickScenario(job + group + task)
	baseMHz := float64(baseValue(job+group+task, 100, 2000)) // 100–2000 MHz base

	numSamples := int(window.Hours() / 24 * samplesPerDay)
	if numSamples < 10 {
		numSamples = 10
	}

	rng := seededRng(job + group + task + "cpu")
	samples := make([]float64, numSamples)

	for i := range samples {
		v := baseMHz * sc.cpuFraction
		// Add sinusoidal daily pattern (higher during "business hours")
		hour := float64(i%samplesPerDay) / float64(samplesPerDay) * 24
		v *= 1 + 0.2*math.Sin((hour-6)*math.Pi/12)
		// Random noise
		v *= 1 + (rng.Float64()*2-1)*sc.noise
		// Occasional spike
		if rng.Float64() < sc.spikeChance {
			v *= sc.cpuSpike
		}
		if v < 1 {
			v = 1
		}
		samples[i] = v
	}

	return samples, nil
}

// QueryMemory returns fake memory usage samples in MB for the given task.
func (a *Adapter) QueryMemory(ctx context.Context, job, group, task, namespace string, window time.Duration) ([]float64, error) {
	sc := pickScenario(job + group + task)
	baseMB := float64(baseValue(job+group+task, 64, 4096)) // 64–4096 MB base

	numSamples := int(window.Hours() / 24 * samplesPerDay)
	if numSamples < 10 {
		numSamples = 10
	}

	rng := seededRng(job + group + task + "mem")
	samples := make([]float64, numSamples)

	// Memory typically grows slowly over time (gradual leak pattern or just
	// steady state), so we add a gentle upward drift.
	for i := range samples {
		drift := 1 + 0.05*float64(i)/float64(numSamples) // up to +5% drift
		v := baseMB * sc.memFraction * drift
		// Noise — memory is usually less spiky than CPU
		v *= 1 + (rng.Float64()*2-1)*sc.noise*0.5
		if v < 1 {
			v = 1
		}
		samples[i] = v
	}

	return samples, nil
}

// pickScenario deterministically selects a usage scenario based on a string key.
func pickScenario(key string) scenario {
	h := hash(key)
	return scenarios[h%uint64(len(scenarios))]
}

// baseValue returns a deterministic value in [min, max] derived from the key.
func baseValue(key string, min, max int) int {
	h := hash(key + "base")
	return min + int(h%uint64(max-min))
}

// seededRng returns a deterministic random source for a given key so that
// repeated runs produce identical output.
func seededRng(key string) *rand.Rand {
	// hash returns uint64; mask to 63 bits to keep seed positive.
	seed := int64(hash(key) & 0x7fffffffffffffff)
	//nolint:gosec // deterministic mock data, security not a concern
	return rand.New(rand.NewSource(seed))
}

// hash is a simple djb2-style hash for turning a string into a uint64.
func hash(s string) uint64 {
	var h uint64 = 5381
	for _, c := range s {
		h = h*33 + uint64(c)
	}
	return h
}
