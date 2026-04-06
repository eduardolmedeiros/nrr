package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Probe Prometheus to check which metrics and labels are available",
	Long: `diagnose queries Prometheus (or VictoriaMetrics) to check whether the
expected metrics exist and what label names are actually present.

Run this first when --metrics-source cadvisor or nomad-native returns no data.

Example:
  nrr diagnose --prometheus-address http://prometheus:9090`,
	RunE: runDiagnose,
}

func init() {
	rootCmd.AddCommand(diagnoseCmd)
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := promapi.NewClient(promapi.Config{
		Address: prometheusAddress,
		Client:  &http.Client{Timeout: 15 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("connecting to Prometheus: %w", err)
	}
	api := promv1.NewAPI(client)

	fmt.Fprintf(os.Stderr, "Probing %s …\n\n", prometheusAddress)

	checkNomadNative(ctx, api)
	fmt.Println()
	checkCAdvisor(ctx, api)

	return nil
}

// checkNomadNative verifies that Nomad's built-in telemetry metrics are present.
func checkNomadNative(ctx context.Context, api promv1.API) {
	fmt.Println("── Nomad native metrics ─────────────────────────────────────────")

	metrics := []string{
		"nomad_client_allocs_cpu_total_ticks",
		"nomad_client_allocs_memory_rss",
	}

	for _, m := range metrics {
		result, _, err := api.Query(ctx, m, time.Now())
		if err != nil {
			fmt.Printf("  ✗ %-45s  error: %v\n", m, err)
			continue
		}
		vec, ok := result.(model.Vector)
		if !ok || len(vec) == 0 {
			fmt.Printf("  ✗ %-45s  no data\n", m)
			continue
		}
		// Collect distinct job+namespace combos as a quick sample.
		jobs := distinctLabelValues(vec, "job")
		fmt.Printf("  ✓ %-45s  %d series  jobs: %s\n", m, len(vec), joinTrunc(jobs, 5))
	}

	fmt.Println()
	fmt.Println("  If ✗: ensure publish_allocation_metrics = true in Nomad telemetry config.")
	fmt.Println("  If ✓: use --metrics-source nomad-native (works for all task drivers).")
}

// checkCAdvisor verifies that cAdvisor container metrics are present and
// inspects which Nomad-related labels are attached.
func checkCAdvisor(ctx context.Context, api promv1.API) {
	fmt.Println("── cAdvisor metrics ─────────────────────────────────────────────")

	// 1. Check if the metric exists at all (no label filter).
	result, _, err := api.Query(ctx, "container_cpu_usage_seconds_total", time.Now())
	if err != nil {
		fmt.Printf("  ✗ container_cpu_usage_seconds_total  error: %v\n", err)
		fmt.Println("\n  cAdvisor does not appear to be scraped by this Prometheus instance.")
		return
	}
	vec, ok := result.(model.Vector)
	if !ok || len(vec) == 0 {
		fmt.Println("  ✗ container_cpu_usage_seconds_total  no data")
		fmt.Println("\n  cAdvisor does not appear to be scraped by this Prometheus instance.")
		return
	}

	fmt.Printf("  ✓ container_cpu_usage_seconds_total  %d total series\n\n", len(vec))

	// 2. Identify which Nomad-related labels are present.
	nomadLabelCandidates := []string{
		// Current naming (Nomad 1.x)
		"container_label_com_hashicorp_nomad_job_name",
		"container_label_com_hashicorp_nomad_task_group_name",
		"container_label_com_hashicorp_nomad_task_name",
		"container_label_com_hashicorp_nomad_alloc_id",
		// Older naming variants
		"container_label_com_hashicorp_nomad_job",
		"container_label_com_hashicorp_nomad_task_group",
		"container_label_com_hashicorp_nomad_task",
	}

	fmt.Println("  Nomad label scan:")
	foundAny := false
	for _, label := range nomadLabelCandidates {
		vals := distinctLabelValues(vec, model.LabelName(label))
		if len(vals) == 0 {
			fmt.Printf("    – %-65s  (not present)\n", label)
		} else {
			fmt.Printf("    ✓ %-65s  values: %s\n", label, joinTrunc(vals, 4))
			foundAny = true
		}
	}

	fmt.Println()
	if foundAny {
		allocIDLabel := "container_label_com_hashicorp_nomad_alloc_id"
		jobNameLabel := "container_label_com_hashicorp_nomad_job_name"

		hasAllocID := len(distinctLabelValues(vec, model.LabelName(allocIDLabel))) > 0
		hasJobName := len(distinctLabelValues(vec, model.LabelName(jobNameLabel))) > 0

		switch {
		case hasAllocID:
			// Preferred strategy — NRR fetches alloc IDs from Nomad API and
			// uses them to filter cAdvisor metrics by container label.
			fmt.Println("  ✓ alloc_id label found — NRR will use alloc_id filtering (preferred strategy).")
			fmt.Println("    Use --metrics-source cadvisor")
			fmt.Println("    Note: only tasks using the Docker driver will appear.")
			if hasJobName {
				fmt.Println("    Job/task name labels are also present (fallback available).")
			}
		case hasJobName:
			fmt.Println("  ✓ Job/task name labels found — NRR will use name-based filtering (fallback strategy).")
			fmt.Println("    Use --metrics-source cadvisor")
			fmt.Println("    Note: only tasks using the Docker driver will appear.")
		default:
			// Has some Nomad labels but none that NRR can use.
			fmt.Println("  ⚠ Nomad labels found but none that NRR supports.")
			fmt.Println("    NRR supports:")
			fmt.Println("      container_label_com_hashicorp_nomad_alloc_id   (preferred)")
			fmt.Println("      container_label_com_hashicorp_nomad_job_name   (fallback)")
			fmt.Println("    Check the label names marked ✓ above and open a GitHub issue")
			fmt.Println("    so we can add support for your Nomad/cAdvisor version.")
		}
	} else {
		fmt.Println("  ✗ No Nomad labels found on cAdvisor metrics.")
		fmt.Println("    This usually means your tasks are using exec/raw_exec drivers,")
		fmt.Println("    not the Docker driver. Use --metrics-source nomad-native instead.")

		// As a bonus, show what non-Nomad labels are present so the user isn't
		// left completely in the dark.
		fmt.Println()
		fmt.Println("  Other labels present on container_cpu_usage_seconds_total:")
		allLabels := collectAllLabelNames(vec)
		for _, l := range allLabels {
			if strings.HasPrefix(l, "container_label_") {
				vals := distinctLabelValues(vec, model.LabelName(l))
				fmt.Printf("    %s = %s\n", l, joinTrunc(vals, 3))
			}
		}
	}
}

// distinctLabelValues returns the unique non-empty values of a label across a vector.
func distinctLabelValues(vec model.Vector, label model.LabelName) []string {
	seen := map[string]struct{}{}
	for _, s := range vec {
		if v := string(s.Metric[label]); v != "" {
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// collectAllLabelNames returns all label names present across a vector, sorted.
func collectAllLabelNames(vec model.Vector) []string {
	seen := map[string]struct{}{}
	for _, s := range vec {
		for k := range s.Metric {
			seen[string(k)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// joinTrunc joins up to max items; appends "+N more" if truncated.
func joinTrunc(items []string, max int) string {
	if len(items) == 0 {
		return "(none)"
	}
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:max], ", ") + fmt.Sprintf(" (+%d more)", len(items)-max)
}
