package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/nrr-project/nrr/internal/recommender"
)

// ANSI colour codes. Set TableFormatter.NoColor = true to suppress them
// (useful when piping output to a file).
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// Box-drawing characters (rounded corners).
const (
	bTopL = "╭"
	bTopR = "╮"
	bBotL = "╰"
	bBotR = "╯"
	bTopT = "┬"
	bBotT = "┴"
	bLT   = "├"
	bRT   = "┤"
	bX    = "┼"
	bH    = "─"
	bV    = "│"
)

// TableFormatter renders recommendations as a pretty terminal table.
type TableFormatter struct {
	w       io.Writer
	NoColor bool
}

type col struct {
	header string
	width  int // visual width in terminal columns (runes, not bytes)
}

// vlen returns the visual width of s in terminal columns.
// Uses rune count, which is correct for the characters we use
// (box-drawing chars, arrows, ASCII — none are double-width).
func vlen(s string) int {
	return utf8.RuneCountInString(s)
}

func (f *TableFormatter) Format(recs []recommender.Recommendation) error {
	w := f.w
	if w == nil {
		w = os.Stdout
	}

	// ---------- build display rows ----------
	type row struct {
		namespace string
		job       string
		group     string
		task      string
		cpuCell   string // plain text (no ANSI) — used for visual-width math
		memCell   string
		cpuColor  string // ANSI prefix applied when rendering
		memColor  string
	}

	rows := make([]row, 0, len(recs))
	for _, r := range recs {
		cpuPlain, cpuAnsi := resourceCell(r.CurrentCPUMHz, r.RecommendedCPUMHz, r.CPUDiffMHz)
		memPlain, memAnsi := resourceCell(r.CurrentMemoryMB, r.RecommendedMemoryMB, r.MemDiffMB)
		rows = append(rows, row{
			namespace: r.Task.Namespace,
			job:       r.Task.Job,
			group:     r.Task.Group,
			task:      r.Task.Task,
			cpuCell:   cpuPlain,
			memCell:   memPlain,
			cpuColor:  cpuAnsi,
			memColor:  memAnsi,
		})
	}

	// ---------- compute column widths (all in runes / visual columns) ----------
	cols := []col{
		{"NAMESPACE", 9},
		{"JOB", 3},
		{"GROUP", 5},
		{"TASK", 4},
		{"CPU (MHz)", 9},
		{"MEMORY (MB)", 11},
	}
	for _, r := range rows {
		cols[0].width = maxInt(cols[0].width, vlen(r.namespace))
		cols[1].width = maxInt(cols[1].width, vlen(r.job))
		cols[2].width = maxInt(cols[2].width, vlen(r.group))
		cols[3].width = maxInt(cols[3].width, vlen(r.task))
		cols[4].width = maxInt(cols[4].width, vlen(r.cpuCell))
		cols[5].width = maxInt(cols[5].width, vlen(r.memCell))
	}
	cols[4].width = maxInt(cols[4].width, 22)
	cols[5].width = maxInt(cols[5].width, 22)

	cc := f.c // bound colour helper

	// ---------- border builder ----------
	hRule := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(left)
		for i, c := range cols {
			sb.WriteString(strings.Repeat(bH, c.width+2))
			if i < len(cols)-1 {
				sb.WriteString(mid)
			}
		}
		sb.WriteString(right)
		return sb.String()
	}

	// pad renders s left-aligned in a visual field of `width` columns,
	// with 1 space of padding on each side. Uses vlen for correct Unicode width.
	pad := func(s string, width int) string {
		return " " + s + strings.Repeat(" ", width-vlen(s)+1)
	}

	// ---------- render ----------
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %sNRR%s — Nomad Resource Recommender\n\n",
		cc(ansiBold+ansiCyan), cc(ansiReset))

	fmt.Fprintln(w, hRule(bTopL, bTopT, bTopR))

	// Header row
	var hdr strings.Builder
	for _, c := range cols {
		hdr.WriteString(bV)
		hdr.WriteString(cc(ansiBold))
		hdr.WriteString(pad(c.header, c.width))
		hdr.WriteString(cc(ansiReset))
	}
	hdr.WriteString(bV)
	fmt.Fprintln(w, hdr.String())

	fmt.Fprintln(w, hRule(bLT, bX, bRT))

	// Data rows
	for _, r := range rows {
		var sb strings.Builder
		sb.WriteString(bV)
		sb.WriteString(pad(r.namespace, cols[0].width))
		sb.WriteString(bV)
		sb.WriteString(pad(r.job, cols[1].width))
		sb.WriteString(bV)
		sb.WriteString(pad(r.group, cols[2].width))
		sb.WriteString(bV)
		sb.WriteString(pad(r.task, cols[3].width))
		sb.WriteString(bV)
		// Resource cells: colour wraps the content but must not affect width math.
		sb.WriteString(" ")
		sb.WriteString(cc(r.cpuColor))
		sb.WriteString(r.cpuCell)
		sb.WriteString(cc(ansiReset))
		sb.WriteString(strings.Repeat(" ", cols[4].width-vlen(r.cpuCell)+1))
		sb.WriteString(bV)
		sb.WriteString(" ")
		sb.WriteString(cc(r.memColor))
		sb.WriteString(r.memCell)
		sb.WriteString(cc(ansiReset))
		sb.WriteString(strings.Repeat(" ", cols[5].width-vlen(r.memCell)+1))
		sb.WriteString(bV)
		fmt.Fprintln(w, sb.String())
	}

	fmt.Fprintln(w, hRule(bBotL, bBotT, bBotR))

	// Summary line
	savings, overage := 0, 0
	for _, r := range recs {
		if r.CPUDiffMHz < 0 || r.MemDiffMB < 0 {
			savings++
		}
		if r.CPUDiffMHz > 0 || r.MemDiffMB > 0 {
			overage++
		}
	}
	fmt.Fprintf(w, "\n  %d task(s)  ·  %s%d can be downsized%s  ·  %s%d need more resources%s\n\n",
		len(recs),
		cc(ansiGreen), savings, cc(ansiReset),
		cc(ansiYellow), overage, cc(ansiReset),
	)

	return nil
}

// c returns the ANSI escape code unless NoColor is set.
func (f *TableFormatter) c(code string) string {
	if f.NoColor {
		return ""
	}
	return code
}

// resourceCell returns the plain display string and the ANSI colour code for
// one resource column. Format: "current → recommended  ±pct%"
// The returned plain string contains only printable runes (no ANSI codes),
// so vlen() on it gives the correct visual width.
func resourceCell(current, recommended, diff int) (plain, color string) {
	pct := 0.0
	if current > 0 {
		pct = float64(recommended-current) / float64(current) * 100
	}

	sign := ""
	if diff > 0 {
		sign = "+"
	}
	plain = fmt.Sprintf("%d → %d  %s%.0f%%", current, recommended, sign, pct)

	switch {
	case diff < 0:
		color = ansiGreen  // saving resources — good
	case diff > 0:
		color = ansiYellow // needs more — warn
	default:
		color = ansiDim
	}
	return plain, color
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
