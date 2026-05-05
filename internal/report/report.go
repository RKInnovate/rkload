// Package report aggregates load test Results and renders summaries.
//
// Summarize collapses a slice of loader.Result into a Summary; Print
// writes the human-readable view to an io.Writer. Future siblings
// (percentile.go, distribution.go, formats for JSON/Markdown) hang off
// the same Summary type.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/RKInnovate/rkload/internal/loader"
)

// Summary is the aggregated outcome of a load run.
type Summary struct {
	Total       int
	Successful  int
	Errors      int
	Elapsed     time.Duration
	Throughput  float64
	AvgLatency  time.Duration
	StatusCodes map[int]int
}

// Summarize collapses results into a Summary. elapsed is the wall-clock
// duration of the run, used for throughput.
func Summarize(results []loader.Result, elapsed time.Duration) Summary {
	s := Summary{
		Total:       len(results),
		Elapsed:     elapsed,
		StatusCodes: make(map[int]int),
	}

	var sumDur time.Duration
	for _, r := range results {
		if r.Err != nil {
			s.Errors++
			continue
		}
		s.Successful++
		sumDur += r.Duration
		s.StatusCodes[r.StatusCode]++
	}

	if elapsed > 0 {
		s.Throughput = float64(s.Total) / elapsed.Seconds()
	}
	if s.Successful > 0 {
		s.AvgLatency = sumDur / time.Duration(s.Successful)
	}
	return s
}

// Print writes the human-readable summary to w.
func Print(w io.Writer, s Summary) {
	fmt.Fprintf(w, "--- Results ---\n")
	fmt.Fprintf(w, "Total requests:  %d\n", s.Total)
	fmt.Fprintf(w, "Successful:      %d\n", s.Successful)
	fmt.Fprintf(w, "Errors:          %d\n", s.Errors)
	fmt.Fprintf(w, "Total time:      %s\n", s.Elapsed.Round(time.Millisecond))
	fmt.Fprintf(w, "Throughput:      %.2f req/sec\n", s.Throughput)

	if s.Successful > 0 {
		fmt.Fprintf(w, "Avg latency:     %s\n", s.AvgLatency.Round(time.Millisecond))

		fmt.Fprintf(w, "\nStatus codes:\n")
		for code, count := range s.StatusCodes {
			barLen := count * 20 / s.Successful
			fmt.Fprintf(w, "  HTTP %d: %d %s\n", code, count, strings.Repeat("█", barLen))
		}
	}
}
