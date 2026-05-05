package report

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDistribute_Empty(t *testing.T) {
	if got := distribute(nil, 10); got != nil {
		t.Errorf("distribute(nil) = %v, want nil", got)
	}
}

func TestDistribute_NonPositiveBuckets(t *testing.T) {
	if got := distribute([]time.Duration{1, 2, 3}, 0); got != nil {
		t.Errorf("distribute(_, 0) = %v, want nil", got)
	}
	if got := distribute([]time.Duration{1, 2, 3}, -1); got != nil {
		t.Errorf("distribute(_, -1) = %v, want nil", got)
	}
}

// All identical inputs should collapse to a single bucket; producing
// 10 buckets of which 9 are empty would be visually misleading.
func TestDistribute_AllSameValue(t *testing.T) {
	durs := []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond}
	got := distribute(durs, 10)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (collapsed)", len(got))
	}
	if got[0].Count != 3 || got[0].Start != got[0].End {
		t.Errorf("bucket = %+v, want {start=end, count=3}", got[0])
	}
}

// Range smaller than the bucket count (in nanoseconds) should still
// produce a usable result rather than dividing by zero.
func TestDistribute_TinyRange(t *testing.T) {
	durs := []time.Duration{1, 2, 3} // 1ns..3ns, range=2ns, n=10 → width=0
	got := distribute(durs, 10)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (collapsed via width=0 fallback)", len(got))
	}
	if got[0].Count != 3 {
		t.Errorf("count = %d, want 3", got[0].Count)
	}
}

// 1..100ms across 10 buckets exercises the full code path: integer
// width division, last-bucket stretch to absorb the max, and total
// count preservation.
func TestDistribute_KnownDistribution(t *testing.T) {
	durs := make([]time.Duration, 100)
	for i := range durs {
		durs[i] = time.Duration(i+1) * time.Millisecond
	}

	buckets := distribute(durs, 10)
	if len(buckets) != 10 {
		t.Fatalf("len = %d, want 10", len(buckets))
	}

	var total int
	for _, b := range buckets {
		total += b.Count
	}
	if total != 100 {
		t.Errorf("total counted = %d, want 100 (no values lost)", total)
	}

	if buckets[0].Start != 1*time.Millisecond {
		t.Errorf("first bucket Start = %v, want 1ms", buckets[0].Start)
	}
	if buckets[9].End != 100*time.Millisecond {
		t.Errorf("last bucket End = %v, want 100ms (max-absorbing)", buckets[9].End)
	}
}

func TestPrintDistribution_RendersBucketsAndBars(t *testing.T) {
	buckets := []Bucket{
		{Start: 0, End: 100 * time.Millisecond, Count: 10},
		{Start: 100 * time.Millisecond, End: 200 * time.Millisecond, Count: 5},
		{Start: 200 * time.Millisecond, End: 300 * time.Millisecond, Count: 0},
	}
	var buf bytes.Buffer
	printDistribution(&buf, buckets)
	out := buf.String()

	if !strings.Contains(out, "Latency distribution:") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("expected at least one bar character:\n%s", out)
	}
	// The fullest bucket (count 10) should produce the longest bar; the
	// half-full bucket (count 5) should be shorter; the empty bucket
	// should have no bar but still appear.
	if !strings.Contains(out, "10 ") || !strings.Contains(out, "5 ") || !strings.Contains(out, "0 ") {
		t.Errorf("expected counts 10, 5, 0 to all appear:\n%s", out)
	}
}

func TestPrintDistribution_SkipsWhenAllZero(t *testing.T) {
	var buf bytes.Buffer
	printDistribution(&buf, []Bucket{{Count: 0}, {Count: 0}})
	if buf.Len() != 0 {
		t.Errorf("expected no output when every bucket is empty, got:\n%s", buf.String())
	}
}

func TestPrintDistribution_SkipsWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	printDistribution(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil input, got:\n%s", buf.String())
	}
}
