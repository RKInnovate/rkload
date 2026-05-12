package tui

import (
	"sort"
	"time"
)

// sparklineWidth is how many samples the throughput history holds.
// Each tick adds one sample; with sampleInterval=250ms, 40 cells
// covers 10 seconds of recent activity — long enough to spot a
// trend, short enough to fit any reasonable terminal width.
const sparklineWidth = 40

// sparkRunes maps a 0–8 normalised level to a Unicode block
// character. Index 0 is "no signal" (space-equivalent low bar)
// and index 8 is a full block. Standard sparkline alphabet.
var sparkRunes = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// renderSparkline turns a slice of non-negative numbers into a
// Unicode sparkline. Empty input returns an empty string. The
// scale is relative: the max sample becomes the tallest bar; all
// others scale from there. That way a long-running smooth load
// still produces a useful visual instead of a flat row of full
// blocks.
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
		// All zeros — render as flat ground row.
		return string(sparkRunes[0]) + repeatedRune(sparkRunes[0], len(samples)-1)
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

func repeatedRune(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]rune, n)
	for i := range buf {
		buf[i] = r
	}
	return string(buf)
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
