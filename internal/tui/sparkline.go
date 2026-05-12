package tui

import (
	"math"
	"sort"
	"strings"
	"time"
)

// sparklineWidth is how many samples the throughput history holds.
// Each tick adds one sample; with sampleInterval=250ms, 80 cells
// covers ~20 seconds of recent activity at a useful width for
// modern terminals. The chart renderer trims to the actual width
// available, so this is an upper bound not a guarantee.
const sparklineWidth = 80

// sparkRunes maps a 0–8 normalised level to a Unicode block
// character. Index 0 is "no signal" (space-equivalent low bar)
// and index 8 is a full block. Standard sparkline alphabet.
var sparkRunes = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// renderLineChart draws a multi-row bar chart of the samples,
// scaled so the largest sample touches the top of the chart and
// the smallest sample lives near the bottom. Each sample becomes
// one column; partial-cell heights at the top of each bar use
// the standard 1/8-block characters so the chart looks smooth.
//
// width and height are in terminal cells. Empty input produces
// `height` blank lines so callers can rely on a consistent
// vertical footprint while the chart is "warming up".
//
// If len(samples) < width the chart is right-aligned (most recent
// sample at the far right edge) — same convention as a tail-style
// activity graph.
func renderLineChart(samples []float64, width, height int) string {
	if height <= 0 {
		return ""
	}
	if width <= 0 || len(samples) == 0 {
		return strings.Repeat("\n", height-1)
	}
	if len(samples) > width {
		samples = samples[len(samples)-width:]
	}

	var maxV float64
	for _, v := range samples {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		// All zeros: render a flat bottom row, blanks above.
		var b strings.Builder
		for r := 0; r < height-1; r++ {
			b.WriteString(strings.Repeat(" ", width))
			b.WriteByte('\n')
		}
		// Bottom row: leading spaces (right-align), then samples-worth of empties.
		b.WriteString(strings.Repeat(" ", width))
		return b.String()
	}

	// Each row represents 8 sub-cells of bar height (the 8 1/8-block
	// characters). So a chart of `height` rows has height*8 vertical
	// steps in total.
	steps := height * 8
	cols := make([]int, len(samples)) // step-height per sample
	for i, v := range samples {
		h := int(math.Round((v / maxV) * float64(steps)))
		if h > steps {
			h = steps
		}
		cols[i] = h
	}

	// Right-align: pad with len(samples) - width blanks on the left.
	leftPad := width - len(samples)
	if leftPad < 0 {
		leftPad = 0
	}

	partials := []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var b strings.Builder
	for r := 0; r < height; r++ {
		// Row r (0 = top, height-1 = bottom). A column with stepHeight h
		// fills the bottom `h` sub-cells; in row r we draw the slice
		// from sub-cell (height-1-r)*8 to (height-r)*8.
		rowFloor := (height - 1 - r) * 8
		rowCeil := (height - r) * 8
		// Left padding
		for p := 0; p < leftPad; p++ {
			b.WriteByte(' ')
		}
		for _, h := range cols {
			switch {
			case h >= rowCeil:
				b.WriteRune('█')
			case h <= rowFloor:
				b.WriteByte(' ')
			default:
				b.WriteRune(partials[h-rowFloor])
			}
		}
		if r < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderSparkline is a single-row variant of renderLineChart, kept
// because tests + the (compact) drill-down view still want a
// one-line sparkline.
func renderSparkline(samples []float64) string {
	if len(samples) == 0 {
		return ""
	}
	var maxV float64
	for _, v := range samples {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		return strings.Repeat(string(sparkRunes[0]), len(samples))
	}
	out := make([]rune, len(samples))
	for i, v := range samples {
		level := int((v / maxV) * float64(len(sparkRunes)-1))
		if level < 0 {
			level = 0
		}
		if level >= len(sparkRunes) {
			level = len(sparkRunes) - 1
		}
		out[i] = sparkRunes[level]
	}
	return string(out)
}

// percentile returns the nearest-rank percentile p (0–100) for
// durations. We re-implement here (rather than importing
// internal/report) because the TUI keeps a private latency slice
// per endpoint and doesn't need the full LatencyStats struct.
//
// Mutates the input by sorting it — callers that need to preserve
// order should pass a copy.
func percentile(durations []time.Duration, p int) time.Duration {
	n := len(durations)
	if n == 0 {
		return 0
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	idx := (p*n + 99) / 100 // ceil(p * n / 100)
	if idx > 0 {
		idx--
	}
	if idx >= n {
		idx = n - 1
	}
	return durations[idx]
}
