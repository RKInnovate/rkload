package report

import (
	"testing"
	"time"
)

// TestPercentile_KnownValues uses durations 1ms..100ms so each percentile
// has a memorable expected value under nearest-rank.
func TestComputeLatencyStats_KnownValues(t *testing.T) {
	durs := make([]time.Duration, 100)
	for i := range durs {
		durs[i] = time.Duration(i+1) * time.Millisecond
	}

	stats := computeLatencyStats(durs)

	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"min", stats.Min, 1 * time.Millisecond},
		{"max", stats.Max, 100 * time.Millisecond},
		{"avg", stats.Avg, 50500 * time.Microsecond}, // sum 5050ms / 100 = 50.5ms exactly
		{"p50", stats.P50, 50 * time.Millisecond},    // ceil(50)-1 = 49 → sorted[49]
		{"p95", stats.P95, 95 * time.Millisecond},    // ceil(95)-1 = 94 → sorted[94]
		{"p99", stats.P99, 99 * time.Millisecond},    // ceil(99)-1 = 98 → sorted[98]
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestComputeLatencyStats_Empty(t *testing.T) {
	stats := computeLatencyStats(nil)
	if (stats != LatencyStats{}) {
		t.Errorf("expected zero LatencyStats for empty input, got %+v", stats)
	}
}

func TestComputeLatencyStats_SingleValue(t *testing.T) {
	stats := computeLatencyStats([]time.Duration{100 * time.Millisecond})

	if stats.Min != 100*time.Millisecond || stats.Max != 100*time.Millisecond ||
		stats.P50 != 100*time.Millisecond || stats.P95 != 100*time.Millisecond ||
		stats.P99 != 100*time.Millisecond || stats.Avg != 100*time.Millisecond {
		t.Errorf("single value should produce 100ms across all percentiles, got %+v", stats)
	}
	if stats.StdDev != 0 {
		t.Errorf("StdDev = %v, want 0 for single value", stats.StdDev)
	}
}

// TestComputeLatencyStats_StdDevKnown uses the textbook example
// {2,4,4,4,5,5,7,9} whose population standard deviation is exactly 2.
func TestComputeLatencyStats_StdDevKnown(t *testing.T) {
	durs := []time.Duration{2, 4, 4, 4, 5, 5, 7, 9}
	stats := computeLatencyStats(durs)
	if stats.StdDev != 2 {
		t.Errorf("StdDev = %v, want 2", stats.StdDev)
	}
}

// Percentile computation must operate on a sort-stable copy — callers
// reuse the durations slice and a hidden mutation would be a nasty
// bug to track down.
func TestComputeLatencyStats_DoesNotMutateInput(t *testing.T) {
	orig := []time.Duration{3, 1, 2, 5, 4}
	durs := append([]time.Duration(nil), orig...)
	computeLatencyStats(durs)
	for i := range durs {
		if durs[i] != orig[i] {
			t.Errorf("input mutated at index %d: got %v, want %v", i, durs[i], orig[i])
		}
	}
}

func TestPercentile_OutOfRangeClamped(t *testing.T) {
	sorted := []time.Duration{10, 20, 30}
	if got := percentile(sorted, 0); got != 10 {
		t.Errorf("p0 = %v, want first element 10", got)
	}
	if got := percentile(sorted, 100); got != 30 {
		t.Errorf("p100 = %v, want last element 30", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile of empty slice = %v, want 0", got)
	}
}
