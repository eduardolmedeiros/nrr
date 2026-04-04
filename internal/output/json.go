package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/nrr-project/nrr/internal/recommender"
)

// JSONFormatter renders recommendations as a JSON array.
type JSONFormatter struct {
	w io.Writer
}

type jsonRecommendation struct {
	Namespace string `json:"namespace"`
	Job       string `json:"job"`
	Group     string `json:"group"`
	Task      string `json:"task"`

	Current struct {
		CPUMHz   int `json:"cpu_mhz"`
		MemoryMB int `json:"memory_mb"`
	} `json:"current"`

	Recommended struct {
		CPUMHz   int `json:"cpu_mhz"`
		MemoryMB int `json:"memory_mb"`
	} `json:"recommended"`

	Diff struct {
		CPUMHz   int `json:"cpu_mhz"`
		MemoryMB int `json:"memory_mb"`
	} `json:"diff"`

	Samples struct {
		CPU    int `json:"cpu"`
		Memory int `json:"memory"`
	} `json:"samples"`
}

func (f *JSONFormatter) Format(recs []recommender.Recommendation) error {
	w := f.w
	if w == nil {
		w = os.Stdout
	}

	out := make([]jsonRecommendation, 0, len(recs))
	for _, r := range recs {
		j := jsonRecommendation{
			Namespace: r.Task.Namespace,
			Job:       r.Task.Job,
			Group:     r.Task.Group,
			Task:      r.Task.Task,
		}
		j.Current.CPUMHz = r.CurrentCPUMHz
		j.Current.MemoryMB = r.CurrentMemoryMB
		j.Recommended.CPUMHz = r.RecommendedCPUMHz
		j.Recommended.MemoryMB = r.RecommendedMemoryMB
		j.Diff.CPUMHz = r.CPUDiffMHz
		j.Diff.MemoryMB = r.MemDiffMB
		j.Samples.CPU = r.CPUSamples
		j.Samples.Memory = r.MemSamples
		out = append(out, j)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
