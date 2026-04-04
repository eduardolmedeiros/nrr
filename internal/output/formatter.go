// Package output provides the Formatter interface and a factory for selecting
// the right output format at runtime.
package output

import (
	"fmt"
	"io"

	"github.com/nrr-project/nrr/internal/recommender"
)


// Formatter serialises a slice of recommendations to an io.Writer.
type Formatter interface {
	Format(recommendations []recommender.Recommendation) error
}

// New returns the Formatter for the given format string.
// Supported: table, json, yaml, csv.
// noColor disables ANSI escape codes in table output (ignored for other formats).
func New(format string, w io.Writer, noColor bool) (Formatter, error) {
	switch format {
	case "table", "":
		return &TableFormatter{w: w, NoColor: noColor}, nil
	case "json":
		return &JSONFormatter{w: w}, nil
	case "yaml":
		return &YAMLFormatter{w: w}, nil
	case "csv":
		return &CSVFormatter{w: w}, nil
	default:
		return nil, fmt.Errorf("unsupported format %q — choose: table, json, yaml, csv", format)
	}
}

