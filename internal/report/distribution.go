package report

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// defaultBuckets is the histogram bucket count when callers don't
// specify one. Ten is a reasonable default for terminal output: dense
// enough to show shape, sparse enough to scan.
const defaultBuckets = 10

// Bucket is one entry in a latency distribution histogram. The
// half-open interval is [Start, End), except for the final bucket
// which extends to End inclusive so the global maximum is counted.
type Bucket struct {
	Start time.Duration
	End   time.Duration
	Count int
}

// distribute splits durations into n linear buckets between the slice's
// min and max. Returns nil for empty input or non-positive n. When all
// inputs are equal, returns a single bucket holding the full count
// (rather than n empty buckets, which would be useless).
func distribute(durations []time.Duration, n int) []Bucket {
	if len(durations) == 0 || n <= 0 {
		return nil
	}

	lo, hi := durations[0], durations[0]
	for _, d := range durations {
		if d < lo {
			lo = d
		}
		if d > hi {
			hi = d
		}
	}

	if lo == hi {
		return []Bucket{{Start: lo, End: hi, Count: len(durations)}}
	}

	width := (hi - lo) / time.Duration(n)
	if width == 0 {
		// Range is smaller than n nanoseconds — fall back to a single
		// bucket so we don't divide by zero in placement.
		return []Bucket{{Start: lo, End: hi, Count: len(durations)}}
	}

	buckets := make([]Bucket, n)
	for i := range buckets {
		buckets[i].Start = lo + time.Duration(i)*width
		buckets[i].End = lo + time.Duration(i+1)*width
	}
	// Stretch the final bucket to the true max so the global maximum
	// always lands in the last bucket regardless of integer-division
	// drift in `width`.
	buckets[n-1].End = hi

	for _, d := range durations {
		idx := int((d - lo) / width)
		if idx >= n {
			idx = n - 1
		}
		buckets[idx].Count++
	}
	return buckets
}

// printDistribution renders an ASCII histogram. Bars are scaled so the
// fullest bucket is 30 cells wide.
func printDistribution(w io.Writer, buckets []Bucket) {
	if len(buckets) == 0 {
		return
	}

	var maxCount int
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}
	if maxCount == 0 {
		return
	}

	fmt.Fprintf(w, "\nLatency distribution:\n")
	for _, b := range buckets {
		barLen := b.Count * 30 / maxCount
		fmt.Fprintf(w, "  %8s - %-8s: %5d %s\n",
			b.Start.Round(time.Millisecond),
			b.End.Round(time.Millisecond),
			b.Count,
			strings.Repeat("█", barLen))
	}
}
