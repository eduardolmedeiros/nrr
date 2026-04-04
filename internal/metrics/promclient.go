// Package metrics provides a shared Prometheus-compatible HTTP client and the
// MetricsBackend interface implemented by each metrics adapter.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PromClient is a thin wrapper around the official Prometheus Go client.
// It works with any Prometheus-compatible backend, including VictoriaMetrics.
type PromClient struct {
	api promv1.API
}

// NewPromClient creates a PromClient pointed at the given address.
// This works for Prometheus and VictoriaMetrics (API-compatible).
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

// QueryInstant executes a PromQL instant query and returns the scalar/vector
// result values as a []float64. It is used for range aggregation queries like
// quantile_over_time(...[7d]).
func (c *PromClient) QueryInstant(ctx context.Context, query string) ([]float64, error) {
	result, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("prometheus query %q: %w", query, err)
	}
	for _, w := range warnings {
		fmt.Printf("prometheus warning: %s\n", w)
	}

	var values []float64
	switch v := result.(type) {
	case model.Vector:
		for _, sample := range v {
			values = append(values, float64(sample.Value))
		}
	case *model.Scalar:
		values = append(values, float64(v.Value))
	}

	return values, nil
}

// QueryRange executes a PromQL range query and returns all sample values
// across the time range. Useful when you want raw samples for local percentile
// computation rather than relying on server-side quantile_over_time.
func (c *PromClient) QueryRange(ctx context.Context, query string, window time.Duration, step time.Duration) ([]float64, error) {
	end := time.Now()
	start := end.Add(-window)

	result, warnings, err := c.api.QueryRange(ctx, query, promv1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil, fmt.Errorf("prometheus range query %q: %w", query, err)
	}
	for _, w := range warnings {
		fmt.Printf("prometheus warning: %s\n", w)
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
