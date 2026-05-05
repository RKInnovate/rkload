package report

import (
	"math"
	"sort"
	"time"
)

// LatencyStats summarises a slice of successful request durations.
// All fields are zero when the input is empty.
type LatencyStats struct {
	Avg    time.Duration
	Min    time.Duration
	Max    time.Duration
	P50    time.Duration
	P95    time.Duration
	P99    time.Duration
	StdDev time.Duration
}

// computeLatencyStats sorts a copy of durations and returns the stats.
// The input slice is not mutated.
func computeLatencyStats(durations []time.Duration) LatencyStats {
	n := len(durations)
	if n == 0 {
		return LatencyStats{}
	}

	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	avg := sum / time.Duration(n)

	var variance float64
	for _, d := range sorted {
		diff := float64(d - avg)
		variance += diff * diff
	}
	variance /= float64(n)

	return LatencyStats{
		Avg:    avg,
		Min:    sorted[0],
		Max:    sorted[n-1],
		P50:    percentile(sorted, 50),
		P95:    percentile(sorted, 95),
		P99:    percentile(sorted, 99),
		StdDev: time.Duration(math.Sqrt(variance)),
	}
}

// percentile returns the nearest-rank percentile p (0–100) from a
// pre-sorted ascending slice.
func percentile(sorted []time.Duration, p int) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(p)*float64(n)/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
