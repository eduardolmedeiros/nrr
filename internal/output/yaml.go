package output

import (
	"io"
	"os"

	"github.com/nrr-project/nrr/internal/recommender"
	"gopkg.in/yaml.v3"
)

// YAMLFormatter renders recommendations as a YAML document.
type YAMLFormatter struct {
	w io.Writer
}

type yamlRecommendation struct {
	Namespace string `yaml:"namespace"`
	Job       string `yaml:"job"`
	Group     string `yaml:"group"`
	Task      string `yaml:"task"`

	Current struct {
		CPUMHz   int `yaml:"cpu_mhz"`
		MemoryMB int `yaml:"memory_mb"`
	} `yaml:"current"`

	Recommended struct {
		CPUMHz   int `yaml:"cpu_mhz"`
		MemoryMB int `yaml:"memory_mb"`
	} `yaml:"recommended"`

	Diff struct {
		CPUMHz   int `yaml:"cpu_mhz"`
		MemoryMB int `yaml:"memory_mb"`
	} `yaml:"diff"`

	Samples struct {
		CPU    int `yaml:"cpu"`
		Memory int `yaml:"memory"`
	} `yaml:"samples"`
}

func (f *YAMLFormatter) Format(recs []recommender.Recommendation) error {
	w := f.w
	if w == nil {
		w = os.Stdout
	}

	out := make([]yamlRecommendation, 0, len(recs))
	for _, r := range recs {
		y := yamlRecommendation{
			Namespace: r.Task.Namespace,
			Job:       r.Task.Job,
			Group:     r.Task.Group,
			Task:      r.Task.Task,
		}
		y.Current.CPUMHz = r.CurrentCPUMHz
		y.Current.MemoryMB = r.CurrentMemoryMB
		y.Recommended.CPUMHz = r.RecommendedCPUMHz
		y.Recommended.MemoryMB = r.RecommendedMemoryMB
		y.Diff.CPUMHz = r.CPUDiffMHz
		y.Diff.MemoryMB = r.MemDiffMB
		y.Samples.CPU = r.CPUSamples
		y.Samples.Memory = r.MemSamples
		out = append(out, y)
	}

	return yaml.NewEncoder(w).Encode(out)
}
