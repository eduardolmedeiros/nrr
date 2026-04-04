package output

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"github.com/nrr-project/nrr/internal/recommender"
)

// CSVFormatter renders recommendations as a CSV file (with headers).
type CSVFormatter struct {
	w io.Writer
}

func (f *CSVFormatter) Format(recs []recommender.Recommendation) error {
	w := f.w
	if w == nil {
		w = os.Stdout
	}

	cw := csv.NewWriter(w)

	// Header row
	if err := cw.Write([]string{
		"namespace", "job", "group", "task",
		"current_cpu_mhz", "recommended_cpu_mhz", "cpu_diff_mhz",
		"current_memory_mb", "recommended_memory_mb", "memory_diff_mb",
		"cpu_samples", "memory_samples",
	}); err != nil {
		return fmt.Errorf("writing CSV header: %w", err)
	}

	for _, r := range recs {
		if err := cw.Write([]string{
			r.Task.Namespace,
			r.Task.Job,
			r.Task.Group,
			r.Task.Task,
			fmt.Sprintf("%d", r.CurrentCPUMHz),
			fmt.Sprintf("%d", r.RecommendedCPUMHz),
			fmt.Sprintf("%d", r.CPUDiffMHz),
			fmt.Sprintf("%d", r.CurrentMemoryMB),
			fmt.Sprintf("%d", r.RecommendedMemoryMB),
			fmt.Sprintf("%d", r.MemDiffMB),
			fmt.Sprintf("%d", r.CPUSamples),
			fmt.Sprintf("%d", r.MemSamples),
		}); err != nil {
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	cw.Flush()
	return cw.Error()
}
