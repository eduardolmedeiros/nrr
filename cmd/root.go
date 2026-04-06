package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nrr-project/nrr/internal/metrics/cadvisor"
	"github.com/nrr-project/nrr/internal/metrics/mock"
	"github.com/nrr-project/nrr/internal/metrics/nomadnative"
	"github.com/nrr-project/nrr/internal/nomad"
	"github.com/nrr-project/nrr/internal/output"
	"github.com/nrr-project/nrr/internal/recommender"
	"github.com/spf13/cobra"
)

var (
	nomadAddress      string
	nomadToken        string
	prometheusAddress string
	metricsSource     string
	windowStr         string
	outputFormat      string
	noColor           bool
	debug             bool
	namespace         string
	jobFilter         string
	cpuPercentile     float64
	memPercentile     float64
	cpuBuffer         float64
	memBuffer         float64
	minCPUMHz         int
	minMemoryMB       int
)

var rootCmd = &cobra.Command{
	Use:   "nrr",
	Short: "NRR — Nomad Resource Recommender",
	Long: `NRR analyses historical CPU and memory usage from Prometheus (or VictoriaMetrics)
and recommends right-sized resource specs for your Nomad jobs.

Metrics sources:
  nomad-native  Uses nomad_client_allocs_* metrics (works with any Nomad task driver)
  cadvisor      Uses container_* metrics from cAdvisor (Docker driver only)

Output formats: table (default), json, yaml, csv`,
	RunE: runRecommend,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Connection flags
	rootCmd.PersistentFlags().StringVar(&nomadAddress, "nomad-address", "http://localhost:4646",
		"Nomad API address")
	rootCmd.PersistentFlags().StringVar(&nomadToken, "nomad-token", "",
		"Nomad ACL token (SecretID). Falls back to NOMAD_TOKEN env var if not set")
	rootCmd.PersistentFlags().StringVar(&prometheusAddress, "prometheus-address", "http://localhost:9090",
		"Prometheus (or VictoriaMetrics) address — VictoriaMetrics is API-compatible, just pass its URL")

	// Metrics source
	rootCmd.Flags().StringVar(&metricsSource, "metrics-source", "nomad-native",
		"Metrics source: nomad-native | cadvisor | mock")

	// Scope filters
	rootCmd.Flags().StringVar(&namespace, "namespace", "default",
		"Nomad namespace to scan (use '*' for all namespaces)")
	rootCmd.Flags().StringVar(&jobFilter, "job", "",
		"Filter by job name (empty = all jobs)")

	// Analysis window
	rootCmd.Flags().StringVar(&windowStr, "window", "7d",
		"Historical data window (e.g. 1d, 3d, 7d, 14d)")

	// Recommendation strategy
	rootCmd.Flags().Float64Var(&cpuPercentile, "cpu-percentile", 99,
		"CPU usage percentile to use for recommendations (0–100)")
	rootCmd.Flags().Float64Var(&memPercentile, "mem-percentile", 99,
		"Memory usage percentile to use for recommendations (0–100)")
	rootCmd.Flags().Float64Var(&cpuBuffer, "cpu-buffer", 15,
		"Buffer % to add on top of the CPU recommendation")
	rootCmd.Flags().Float64Var(&memBuffer, "mem-buffer", 15,
		"Buffer % to add on top of the memory recommendation")
	rootCmd.Flags().IntVar(&minCPUMHz, "min-cpu", 10,
		"Minimum recommended CPU (MHz)")
	rootCmd.Flags().IntVar(&minMemoryMB, "min-memory", 64,
		"Minimum recommended memory (MB)")

	// Output
	rootCmd.Flags().StringVarP(&outputFormat, "output", "o", "table",
		"Output format: table | json | yaml | csv")
	rootCmd.Flags().BoolVar(&noColor, "no-color", false,
		"Disable ANSI colours in table output (useful when piping to a file)")
	rootCmd.Flags().BoolVar(&debug, "debug", false,
		"Print the PromQL queries sent to Prometheus before executing them")
}

func runRecommend(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Parse window duration
	window, err := parseDuration(windowStr)
	if err != nil {
		return fmt.Errorf("invalid --window %q: %w", windowStr, err)
	}

	// Validate flags
	validSources := map[string]bool{"nomad-native": true, "cadvisor": true, "mock": true}
	if !validSources[metricsSource] {
		return fmt.Errorf("unknown --metrics-source %q: must be nomad-native, cadvisor, or mock", metricsSource)
	}

	// 1. Discover Nomad tasks and their current resource specs.
	// In mock mode we skip the real Nomad API and use synthetic tasks instead.
	var tasks []nomad.TaskSpec
	if metricsSource == "mock" {
		tasks = mockTasks()
		fmt.Fprintf(os.Stderr, "Mock mode: using %d synthetic tasks (no Nomad API needed).\n", len(tasks))
	} else {
		nomadClient, err := nomad.NewClient(nomadAddress, nomadToken)
		if err != nil {
			return fmt.Errorf("connecting to Nomad at %s: %w", nomadAddress, err)
		}
		fmt.Fprintf(os.Stderr, "Discovering tasks in namespace %q...\n", namespace)
		tasks, err = nomadClient.DiscoverTasks(ctx, namespace, jobFilter)
		if err != nil {
			return fmt.Errorf("discovering tasks: %w", err)
		}
	}
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "No tasks found — check your --namespace and --job filters.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Found %d task(s). Querying metrics (window: %s)...\n", len(tasks), windowStr)

	// 2. Build the metrics backend
	var backend recommender.MetricsBackend
	switch metricsSource {
	case "nomad-native":
		b, err := nomadnative.New(prometheusAddress)
		if err != nil {
			return fmt.Errorf("initialising nomad-native metrics adapter: %w", err)
		}
		b.SetDebug(debug)
		backend = b
	case "cadvisor":
		b, err := cadvisor.New(prometheusAddress)
		if err != nil {
			return fmt.Errorf("initialising cadvisor metrics adapter: %w", err)
		}
		b.SetDebug(debug)
		backend = b
	case "mock":
		backend = mock.New()
	}

	// 3. Recommendation config
	cfg := recommender.Config{
		CPUPercentile: cpuPercentile / 100.0,
		MemPercentile: memPercentile / 100.0,
		CPUBuffer:     cpuBuffer / 100.0,
		MemBuffer:     memBuffer / 100.0,
		MinCPUMHz:     minCPUMHz,
		MinMemoryMB:   minMemoryMB,
	}

	// 4. Generate recommendations
	var recommendations []recommender.Recommendation
	for _, task := range tasks {
		cpuSamples, err := backend.QueryCPU(ctx, task, window)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: CPU query failed for %s/%s/%s: %v\n",
				task.Job, task.Group, task.Task, err)
		}

		memSamples, err := backend.QueryMemory(ctx, task, window)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: memory query failed for %s/%s/%s: %v\n",
				task.Job, task.Group, task.Task, err)
		}

		if len(cpuSamples) == 0 && len(memSamples) == 0 {
			fmt.Fprintf(os.Stderr, "  WARN: no metrics for %s/%s/%s — skipping\n",
				task.Job, task.Group, task.Task)
			continue
		}

		rec := recommender.Recommend(task, cpuSamples, memSamples, cfg)
		recommendations = append(recommendations, rec)
	}

	if len(recommendations) == 0 {
		fmt.Fprintln(os.Stderr, "No recommendations generated — no metrics data found.")
		return nil
	}

	// 5. Format and print output
	formatter, err := output.New(outputFormat, os.Stdout, noColor)
	if err != nil {
		return fmt.Errorf("output format %q: %w", outputFormat, err)
	}

	return formatter.Format(recommendations)
}

// mockTasks returns a set of synthetic Nomad tasks that cover the four
// usage scenarios defined in the mock adapter (underutilised, well-sized,
// spiky, hungry). These let you see all recommendation flavours in one run.
func mockTasks() []nomad.TaskSpec {
	return []nomad.TaskSpec{
		// Underutilised: declared resources much larger than actual usage
		{Namespace: "default", Job: "api-gateway", Group: "web", Task: "nginx", CPUMHz: 2000, MemoryMB: 1024, JobType: "service"},
		{Namespace: "default", Job: "api-gateway", Group: "web", Task: "envoy", CPUMHz: 1000, MemoryMB: 512, JobType: "service"},

		// Well-sized: declared resources roughly match actual usage
		{Namespace: "default", Job: "backend-api", Group: "app", Task: "server", CPUMHz: 500, MemoryMB: 256, JobType: "service"},
		{Namespace: "default", Job: "backend-api", Group: "app", Task: "metrics-exporter", CPUMHz: 100, MemoryMB: 64, JobType: "service"},

		// Spiky: low average but occasional bursts — tests P99 strategy
		{Namespace: "data", Job: "batch-processor", Group: "workers", Task: "processor", CPUMHz: 4000, MemoryMB: 2048, JobType: "batch"},
		{Namespace: "data", Job: "batch-processor", Group: "workers", Task: "scheduler", CPUMHz: 200, MemoryMB: 128, JobType: "batch"},

		// Hungry: consistently using more than declared — recommendation will be higher
		{Namespace: "data", Job: "ml-inference", Group: "serving", Task: "model-server", CPUMHz: 1000, MemoryMB: 2048, JobType: "service"},
		{Namespace: "data", Job: "ml-inference", Group: "serving", Task: "feature-store", CPUMHz: 500, MemoryMB: 512, JobType: "service"},
	}
}

// parseDuration parses durations like "7d", "24h", "30m".
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days := 0
		if _, err := fmt.Sscanf(s, "%dd", &days); err != nil {
			return 0, fmt.Errorf("invalid day format: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
