// Package metrics provides a shared Prometheus-compatible HTTP client and the
// MetricsBackend interface implemented by each metrics adapter.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	// targetSteps is the desired number of data points per query window.
	// More steps = finer resolution but more load on Prometheus.
	targetSteps = 200

	minStep = 15 * time.Second
	maxStep = 5 * time.Minute
)

// PromClient is a thin wrapper around the official Prometheus Go client.
// It works with any Prometheus-compatible backend, including VictoriaMetrics.
type PromClient struct {
	api   promv1.API
	Debug bool // when true, prints every PromQL query to stderr before execution
}

// NewPromClient creates a PromClient pointed at the given address.
func NewPromClient(address string) (*PromClient, error) {
	client, err := api.NewClient(api.Config{
		Address: address,
		Client:  &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("creating Prometheus client: %w", err)
	}
	return &PromClient{api: promv1.NewAPI(client)}, nil
}

// stepFor computes an appropriate range query step for the given window:
// window/targetSteps, clamped between minStep and maxStep.
func stepFor(window time.Duration) time.Duration {
	step := window / targetSteps
	if step < minStep {
		step = minStep
	}
	if step > maxStep {
		step = maxStep
	}
	return step
}

// QueryRange executes a PromQL range query over [now-window, now] and returns
// all sample values. The step is derived automatically from the window size.
func (c *PromClient) QueryRange(ctx context.Context, query string, window time.Duration) ([]float64, error) {
	end := time.Now()
	start := end.Add(-window)
	step := stepFor(window)

	if c.Debug {
		fmt.Fprintf(os.Stderr, "\n  [debug] window=%s step=%s\n  [debug] query: %s\n\n", window, step, query)
	}

	result, warnings, err := c.api.QueryRange(ctx, query, promv1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil, fmt.Errorf("prometheus range query: %w", err)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  prometheus warning: %s\n", w)
	}

	var values []float64
	if matrix, ok := result.(model.Matrix); ok {
		for _, stream := range matrix {
			for _, point := range stream.Values {
				values = append(values, float64(point.Value))
			}
		}
	}

	return values, nil
}
