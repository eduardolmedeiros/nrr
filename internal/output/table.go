package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/nrr-project/nrr/internal/recommender"
)

// ANSI colour codes. Set TableFormatter.NoColor = true to suppress them.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiRed    = "\033[31m"
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

// col describes one table column.
type col struct {
	header    string
	width     int  // visual width in terminal columns
	rightAlign bool // numbers are right-aligned, text is left-aligned
}

// vlen returns the visual width of s in terminal columns (rune count).
func vlen(s string) int {
	return utf8.RuneCountInString(s)
}

func (f *TableFormatter) Format(recs []recommender.Recommendation) error {
	w := f.w
	if w == nil {
		w = os.Stdout
	}

	cc := f.c

	// ── build display rows ────────────────────────────────────────────────────
	type row struct {
		num      string
		ns       string
		region   string
		dc       string
		job      string
		jobType  string
		group    string
		task     string
		allocs   string
		cpuNow   string
		cpuRec   string
		cpuColor string
		memNow   string
		memRec   string
		memColor string
	}

	rows := make([]row, 0, len(recs))
	for i, r := range recs {
		cpuNow, cpuRec, cpuColor := resourceCells(r.CurrentCPUMHz, r.RecommendedCPUMHz, r.CPUDiffMHz, r.CPUSamples)
		memNow, memRec, memColor := resourceCells(r.CurrentMemoryMB, r.RecommendedMemoryMB, r.MemDiffMB, r.MemSamples)

		allocs := "-"
		if n := len(r.Task.AllocIDs); n > 0 {
			allocs = fmt.Sprintf("%d", n)
		}

		jobType := r.Task.JobType
		if jobType == "" {
			jobType = "-"
		}

		dc := "-"
		if len(r.Task.Datacenters) > 0 {
			dc = strings.Join(r.Task.Datacenters, ",")
		}
		region := r.Task.Region
		if region == "" {
			region = "-"
		}

		rows = append(rows, row{
			num:      fmt.Sprintf("%d", i+1),
			ns:       r.Task.Namespace,
			region:   region,
			dc:       dc,
			job:      r.Task.Job,
			jobType:  jobType,
			group:    r.Task.Group,
			task:     r.Task.Task,
			allocs:   allocs,
			cpuNow:   cpuNow,
			cpuRec:   cpuRec,
			cpuColor: cpuColor,
			memNow:   memNow,
			memRec:   memRec,
			memColor: memColor,
		})
	}

	// ── compute column widths ─────────────────────────────────────────────────
	cols := []col{
		{"#", 1, true},
		{"REGION", 6, false},
		{"DC", 2, false},
		{"NAMESPACE", 9, false},
		{"JOB", 3, false},
		{"TYPE", 4, false},
		{"GROUP", 5, false},
		{"TASK", 4, false},
		{"ALLOCS", 6, true},
		{"CPU NOW", 7, true},
		{"CPU REC", 7, false},
		{"MEM NOW", 7, true},
		{"MEM REC", 7, false},
	}

	for _, r := range rows {
		cols[0].width = maxInt(cols[0].width, vlen(r.num))
		cols[1].width = maxInt(cols[1].width, vlen(r.region))
		cols[2].width = maxInt(cols[2].width, vlen(r.dc))
		cols[3].width = maxInt(cols[3].width, vlen(r.ns))
		cols[4].width = maxInt(cols[4].width, vlen(r.job))
		cols[5].width = maxInt(cols[5].width, vlen(r.jobType))
		cols[6].width = maxInt(cols[6].width, vlen(r.group))
		cols[7].width = maxInt(cols[7].width, vlen(r.task))
		cols[8].width = maxInt(cols[8].width, vlen(r.allocs))
		cols[9].width = maxInt(cols[9].width, vlen(r.cpuNow))
		cols[10].width = maxInt(cols[10].width, vlen(r.cpuRec))
		cols[11].width = maxInt(cols[11].width, vlen(r.memNow))
		cols[12].width = maxInt(cols[12].width, vlen(r.memRec))
	}
	// minimum widths for resource columns so short values don't look cramped
	cols[9].width = maxInt(cols[9].width, 7)
	cols[10].width = maxInt(cols[10].width, 12)
	cols[11].width = maxInt(cols[11].width, 7)
	cols[12].width = maxInt(cols[12].width, 12)

	// ── border helpers ────────────────────────────────────────────────────────
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

	// padL left-aligns s in a field of `width` visual columns (1-space padding each side).
	padL := func(s string, width int) string {
		return " " + s + strings.Repeat(" ", width-vlen(s)+1)
	}

	// padR right-aligns s in a field of `width` visual columns (1-space padding each side).
	padR := func(s string, width int) string {
		return strings.Repeat(" ", width-vlen(s)+1) + s + " "
	}

	padCell := func(s string, c col) string {
		if c.rightAlign {
			return padR(s, c.width)
		}
		return padL(s, c.width)
	}

	// ── render ────────────────────────────────────────────────────────────────
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %sNRR%s — Nomad Resource Recommender\n\n",
		cc(ansiBold+ansiCyan), cc(ansiReset))

	fmt.Fprintln(w, hRule(bTopL, bTopT, bTopR))

	// Header row
	var hdr strings.Builder
	for _, c := range cols {
		hdr.WriteString(bV)
		hdr.WriteString(cc(ansiBold))
		hdr.WriteString(padCell(c.header, c))
		hdr.WriteString(cc(ansiReset))
	}
	hdr.WriteString(bV)
	fmt.Fprintln(w, hdr.String())

	fmt.Fprintln(w, hRule(bLT, bX, bRT))

	// Data rows
	for _, r := range rows {
		var sb strings.Builder
		sb.WriteString(bV); sb.WriteString(cc(ansiDim)); sb.WriteString(padCell(r.num, cols[0])); sb.WriteString(cc(ansiReset))
		sb.WriteString(bV); sb.WriteString(cc(ansiDim)); sb.WriteString(padCell(r.region, cols[1])); sb.WriteString(cc(ansiReset))
		sb.WriteString(bV); sb.WriteString(cc(ansiDim)); sb.WriteString(padCell(r.dc, cols[2])); sb.WriteString(cc(ansiReset))
		sb.WriteString(bV); sb.WriteString(padCell(r.ns, cols[3]))
		sb.WriteString(bV); sb.WriteString(padCell(r.job, cols[4]))
		sb.WriteString(bV); sb.WriteString(cc(ansiDim)); sb.WriteString(padCell(r.jobType, cols[5])); sb.WriteString(cc(ansiReset))
		sb.WriteString(bV); sb.WriteString(padCell(r.group, cols[6]))
		sb.WriteString(bV); sb.WriteString(padCell(r.task, cols[7]))
		sb.WriteString(bV); sb.WriteString(cc(ansiDim)); sb.WriteString(padCell(r.allocs, cols[8])); sb.WriteString(cc(ansiReset))

		// CPU NOW — plain, right-aligned
		sb.WriteString(bV)
		sb.WriteString(padCell(r.cpuNow, cols[9]))

		// CPU REC — coloured
		sb.WriteString(bV)
		sb.WriteString(" ")
		sb.WriteString(cc(r.cpuColor))
		sb.WriteString(r.cpuRec)
		sb.WriteString(cc(ansiReset))
		sb.WriteString(strings.Repeat(" ", cols[10].width-vlen(r.cpuRec)+1))

		// MEM NOW — plain, right-aligned
		sb.WriteString(bV)
		sb.WriteString(padCell(r.memNow, cols[11]))

		// MEM REC — coloured
		sb.WriteString(bV)
		sb.WriteString(" ")
		sb.WriteString(cc(r.memColor))
		sb.WriteString(r.memRec)
		sb.WriteString(cc(ansiReset))
		sb.WriteString(strings.Repeat(" ", cols[12].width-vlen(r.memRec)+1))

		sb.WriteString(bV)
		fmt.Fprintln(w, sb.String())
	}

	fmt.Fprintln(w, hRule(bBotL, bBotT, bBotR))

	// ── summary line ─────────────────────────────────────────────────────────
	var savings, overage, ok int
	for _, r := range recs {
		switch {
		case r.CPUDiffMHz < 0 || r.MemDiffMB < 0:
			savings++
		case r.CPUDiffMHz > 0 || r.MemDiffMB > 0:
			overage++
		default:
			ok++
		}
	}
	fmt.Fprintf(w,
		"\n  %d task(s)  ·  %s↓ %d can be downsized%s  ·  %s↑ %d need more resources%s  ·  %s✓ %d well-sized%s\n\n",
		len(recs),
		cc(ansiGreen), savings, cc(ansiReset),
		cc(ansiYellow), overage, cc(ansiReset),
		cc(ansiDim), ok, cc(ansiReset),
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

// resourceCells returns the "now" string, the "recommended" string (with % diff),
// and the ANSI colour for the recommended value.
// samples == 0 means no data was available for the recommendation.
func resourceCells(current, recommended, diff, samples int) (now, rec, color string) {
	if current > 0 {
		now = fmt.Sprintf("%d", current)
	} else {
		now = "-"
	}

	if samples == 0 {
		// No metrics data — recommendation is just the current value unchanged.
		rec = fmt.Sprintf("%d", recommended)
		color = ansiDim
		return
	}

	pct := 0.0
	if current > 0 {
		pct = float64(recommended-current) / float64(current) * 100
	}

	sign := ""
	if diff > 0 {
		sign = "+"
	}
	rec = fmt.Sprintf("%d (%s%.0f%%)", recommended, sign, pct)

	switch {
	case diff < 0:
		color = ansiGreen
	case diff > 0:
		color = ansiYellow
	default:
		color = ansiDim
	}
	return
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
