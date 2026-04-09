package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nrr-project/nrr/internal/metrics/cadvisor"
	"github.com/nrr-project/nrr/internal/metrics/mock"
	"github.com/nrr-project/nrr/internal/metrics/nomadnative"
	"github.com/nrr-project/nrr/internal/nomad"
	"github.com/nrr-project/nrr/internal/output"
	"github.com/nrr-project/nrr/internal/recommender"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configFile string

var rootCmd = &cobra.Command{
	Use:     "nrr",
	Version: Version,
	Short:   "NRR — Nomad Resource Recommender",
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
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&configFile, "config", "",
		"Config file (default: .nrr/config.yaml, then $XDG_CONFIG_HOME/nrr/config.yaml)")

	// Connection flags
	rootCmd.PersistentFlags().String("nomad-address", "", "Nomad API address (env: NOMAD_ADDR)")
	rootCmd.PersistentFlags().String("nomad-token", "", "Nomad ACL token (env: NOMAD_TOKEN)")
	rootCmd.PersistentFlags().String("nomad-ca-cert", "", "Path to CA certificate for Nomad TLS (env: NOMAD_CACERT)")
	rootCmd.PersistentFlags().String("nomad-client-cert", "", "Path to client certificate for Nomad mTLS (env: NOMAD_CLIENT_CERT)")
	rootCmd.PersistentFlags().String("nomad-client-key", "", "Path to client key for Nomad mTLS (env: NOMAD_CLIENT_KEY)")
	rootCmd.PersistentFlags().Bool("nomad-tls-insecure", false, "Skip TLS certificate verification (not recommended in production)")
	rootCmd.PersistentFlags().String("prometheus-address", "http://localhost:9090",
		"Prometheus (or VictoriaMetrics) address")

	// Metrics source
	rootCmd.Flags().String("metrics-source", "nomad-native", "Metrics source: nomad-native | cadvisor | mock")

	// Scope filters
	rootCmd.Flags().String("namespace", "default", "Nomad namespace to scan (use '*' for all namespaces)")
	rootCmd.Flags().String("job", "", "Filter by job name (empty = all jobs)")

	// Analysis window
	rootCmd.Flags().String("window", "7d", "Historical data window (e.g. 1d, 3d, 7d, 14d)")

	// Recommendation strategy
	rootCmd.Flags().Float64("cpu-percentile", 99, "CPU usage percentile to use for recommendations (0–100)")
	rootCmd.Flags().Float64("mem-percentile", 99, "Memory usage percentile to use for recommendations (0–100)")
	rootCmd.Flags().Float64("cpu-buffer", 15, "Buffer % to add on top of the CPU recommendation")
	rootCmd.Flags().Float64("mem-buffer", 15, "Buffer % to add on top of the memory recommendation")
	rootCmd.Flags().Int("min-cpu", 10, "Minimum recommended CPU (MHz)")
	rootCmd.Flags().Int("min-memory", 64, "Minimum recommended memory (MB)")

	// Output
	rootCmd.Flags().StringP("output", "o", "table", "Output format: table | json | yaml | csv")
	rootCmd.Flags().Bool("no-color", false, "Disable ANSI colours in table output (useful when piping to a file)")
	rootCmd.Flags().Bool("debug", false, "Print the PromQL queries sent to Prometheus before executing them")

	// Bind all flags to viper so config file + env vars feed them automatically.
	_ = viper.BindPFlag("nomad.address", rootCmd.PersistentFlags().Lookup("nomad-address"))
	_ = viper.BindPFlag("nomad.token", rootCmd.PersistentFlags().Lookup("nomad-token"))
	_ = viper.BindPFlag("nomad.tls.ca_cert", rootCmd.PersistentFlags().Lookup("nomad-ca-cert"))
	_ = viper.BindPFlag("nomad.tls.client_cert", rootCmd.PersistentFlags().Lookup("nomad-client-cert"))
	_ = viper.BindPFlag("nomad.tls.client_key", rootCmd.PersistentFlags().Lookup("nomad-client-key"))
	_ = viper.BindPFlag("nomad.tls.insecure", rootCmd.PersistentFlags().Lookup("nomad-tls-insecure"))
	_ = viper.BindPFlag("prometheus.address", rootCmd.PersistentFlags().Lookup("prometheus-address"))
	_ = viper.BindPFlag("metrics_source", rootCmd.Flags().Lookup("metrics-source"))
	_ = viper.BindPFlag("namespace", rootCmd.Flags().Lookup("namespace"))
	_ = viper.BindPFlag("job", rootCmd.Flags().Lookup("job"))
	_ = viper.BindPFlag("window", rootCmd.Flags().Lookup("window"))
	_ = viper.BindPFlag("strategy.cpu_percentile", rootCmd.Flags().Lookup("cpu-percentile"))
	_ = viper.BindPFlag("strategy.mem_percentile", rootCmd.Flags().Lookup("mem-percentile"))
	_ = viper.BindPFlag("strategy.cpu_buffer", rootCmd.Flags().Lookup("cpu-buffer"))
	_ = viper.BindPFlag("strategy.mem_buffer", rootCmd.Flags().Lookup("mem-buffer"))
	_ = viper.BindPFlag("strategy.min_cpu", rootCmd.Flags().Lookup("min-cpu"))
	_ = viper.BindPFlag("strategy.min_memory", rootCmd.Flags().Lookup("min-memory"))
	_ = viper.BindPFlag("output.format", rootCmd.Flags().Lookup("output"))
	_ = viper.BindPFlag("output.no_color", rootCmd.Flags().Lookup("no-color"))
	_ = viper.BindPFlag("output.debug", rootCmd.Flags().Lookup("debug"))

	// Bind standard Nomad env vars.
	_ = viper.BindEnv("nomad.address", "NOMAD_ADDR")
	_ = viper.BindEnv("nomad.token", "NOMAD_TOKEN")
	_ = viper.BindEnv("nomad.tls.ca_cert", "NOMAD_CACERT")
	_ = viper.BindEnv("nomad.tls.client_cert", "NOMAD_CLIENT_CERT")
	_ = viper.BindEnv("nomad.tls.client_key", "NOMAD_CLIENT_KEY")
}

// initConfig loads the config file (if any). Called by cobra before RunE.
func initConfig() {
	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		// 1. Project-level: .nrr/config.yaml in the current directory.
		viper.AddConfigPath(".nrr")
		// 2. User-level: $XDG_CONFIG_HOME/nrr/config.yaml (or ~/Library/Application Support/nrr on macOS).
		if xdgConfig, err := os.UserConfigDir(); err == nil {
			viper.AddConfigPath(filepath.Join(xdgConfig, "nrr"))
		}
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintf(os.Stderr, "Using config file: %s\n", viper.ConfigFileUsed())
	}
}

func runRecommend(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	metricsSource := viper.GetString("metrics_source")
	windowStr := viper.GetString("window")
	namespace := viper.GetString("namespace")

	window, err := parseDuration(windowStr)
	if err != nil {
		return fmt.Errorf("invalid window %q: %w", windowStr, err)
	}

	validSources := map[string]bool{"nomad-native": true, "cadvisor": true, "mock": true}
	if !validSources[metricsSource] {
		return fmt.Errorf("unknown metrics-source %q: must be nomad-native, cadvisor, or mock", metricsSource)
	}

	// 1. Discover Nomad tasks.
	var tasks []nomad.TaskSpec
	if metricsSource == "mock" {
		tasks = mockTasks()
		fmt.Fprintf(os.Stderr, "Mock mode: using %d synthetic tasks (no Nomad API needed).\n", len(tasks))
	} else {
		nomadClient, err := nomad.NewClient(
			viper.GetString("nomad.address"),
			viper.GetString("nomad.token"),
			nomad.TLSConfig{
				CACert:     viper.GetString("nomad.tls.ca_cert"),
				ClientCert: viper.GetString("nomad.tls.client_cert"),
				ClientKey:  viper.GetString("nomad.tls.client_key"),
				Insecure:   viper.GetBool("nomad.tls.insecure"),
			},
		)
		if err != nil {
			return fmt.Errorf("connecting to Nomad: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Discovering tasks in namespace %q...\n", namespace)
		tasks, err = nomadClient.DiscoverTasks(ctx, namespace, viper.GetString("job"))
		if err != nil {
			return fmt.Errorf("discovering tasks: %w", err)
		}
	}
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "No tasks found — check your namespace and job filters.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Found %d task(s). Querying metrics (window: %s)...\n", len(tasks), windowStr)

	// 2. Build the metrics backend.
	prometheusAddress := viper.GetString("prometheus.address")
	debug := viper.GetBool("output.debug")
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

	// 3. Recommendation config.
	cfg := recommender.Config{
		CPUPercentile: viper.GetFloat64("strategy.cpu_percentile") / 100.0,
		MemPercentile: viper.GetFloat64("strategy.mem_percentile") / 100.0,
		CPUBuffer:     viper.GetFloat64("strategy.cpu_buffer") / 100.0,
		MemBuffer:     viper.GetFloat64("strategy.mem_buffer") / 100.0,
		MinCPUMHz:     viper.GetInt("strategy.min_cpu"),
		MinMemoryMB:   viper.GetInt("strategy.min_memory"),
	}

	// 4. Generate recommendations.
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

	// 5. Format and print output.
	formatter, err := output.New(viper.GetString("output.format"), os.Stdout, viper.GetBool("output.no_color"))
	if err != nil {
		return fmt.Errorf("output format %q: %w", viper.GetString("output.format"), err)
	}

	return formatter.Format(recommendations)
}

// mockTasks returns a set of synthetic Nomad tasks that cover the four
// usage scenarios defined in the mock adapter (underutilised, well-sized,
// spiky, hungry). These let you see all recommendation flavours in one run.
func mockTasks() []nomad.TaskSpec {
	fakeAllocs := func(n int, prefix string) []string {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("%s-mock-alloc-%04d", prefix, i+1)
		}
		return ids
	}

	return []nomad.TaskSpec{
		{Namespace: "default", Job: "api-gateway", Group: "web", Task: "nginx", CPUMHz: 2000, MemoryMB: 1024, JobType: "service", AllocIDs: fakeAllocs(2, "api-gateway"), Region: "global", Datacenters: []string{"dc1"}},
		{Namespace: "default", Job: "api-gateway", Group: "web", Task: "envoy", CPUMHz: 1000, MemoryMB: 512, JobType: "service", AllocIDs: fakeAllocs(2, "api-gateway"), Region: "global", Datacenters: []string{"dc1"}},
		{Namespace: "default", Job: "backend-api", Group: "app", Task: "server", CPUMHz: 500, MemoryMB: 256, JobType: "service", AllocIDs: fakeAllocs(1, "backend-api"), Region: "global", Datacenters: []string{"dc1"}},
		{Namespace: "default", Job: "backend-api", Group: "app", Task: "metrics-exporter", CPUMHz: 100, MemoryMB: 64, JobType: "service", AllocIDs: fakeAllocs(1, "backend-api"), Region: "global", Datacenters: []string{"dc1"}},
		{Namespace: "data", Job: "batch-processor", Group: "workers", Task: "processor", CPUMHz: 4000, MemoryMB: 2048, JobType: "batch", AllocIDs: fakeAllocs(3, "batch-processor"), Region: "global", Datacenters: []string{"dc1", "dc2"}},
		{Namespace: "data", Job: "batch-processor", Group: "workers", Task: "scheduler", CPUMHz: 200, MemoryMB: 128, JobType: "batch", AllocIDs: fakeAllocs(3, "batch-processor"), Region: "global", Datacenters: []string{"dc1", "dc2"}},
		{Namespace: "data", Job: "ml-inference", Group: "serving", Task: "model-server", CPUMHz: 1000, MemoryMB: 2048, JobType: "service", AllocIDs: fakeAllocs(2, "ml-inference"), Region: "global", Datacenters: []string{"dc1"}},
		{Namespace: "data", Job: "ml-inference", Group: "serving", Task: "feature-store", CPUMHz: 500, MemoryMB: 512, JobType: "service", AllocIDs: fakeAllocs(2, "ml-inference"), Region: "global", Datacenters: []string{"dc1"}},
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
