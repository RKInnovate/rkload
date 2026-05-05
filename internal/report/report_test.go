package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RKInnovate/rkload/internal/loader"
)

func TestSummarize_MixedSuccessAndError(t *testing.T) {
	results := []loader.Result{
		{Duration: 100 * time.Millisecond, StatusCode: 200},
		{Duration: 200 * time.Millisecond, StatusCode: 200},
		{Duration: 300 * time.Millisecond, StatusCode: 500},
		{Err: errors.New("boom")},
	}
	s := Summarize(results, 2*time.Second)

	if s.Total != 4 {
		t.Errorf("Total = %d, want 4", s.Total)
	}
	if s.Successful != 3 {
		t.Errorf("Successful = %d, want 3", s.Successful)
	}
	if s.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Errors)
	}
	if want := 200 * time.Millisecond; s.AvgLatency != want {
		t.Errorf("AvgLatency = %v, want %v", s.AvgLatency, want)
	}
	if got, want := s.Throughput, 2.0; got != want {
		t.Errorf("Throughput = %v, want %v", got, want)
	}
	if s.StatusCodes[200] != 2 || s.StatusCodes[500] != 1 {
		t.Errorf("StatusCodes = %v, want {200: 2, 500: 1}", s.StatusCodes)
	}
}

func TestSummarize_AllErrors(t *testing.T) {
	results := []loader.Result{
		{Err: errors.New("boom")},
		{Err: errors.New("boom")},
	}
	s := Summarize(results, time.Second)

	if s.Errors != 2 || s.Successful != 0 {
		t.Errorf("Errors=%d Successful=%d, want 2/0", s.Errors, s.Successful)
	}
	if s.AvgLatency != 0 {
		t.Errorf("AvgLatency = %v, want 0 (no successes)", s.AvgLatency)
	}
}

func TestSummarize_EmptyElapsedNoThroughput(t *testing.T) {
	s := Summarize([]loader.Result{{Duration: time.Millisecond, StatusCode: 200}}, 0)
	if s.Throughput != 0 {
		t.Errorf("Throughput = %v, want 0 when elapsed is 0", s.Throughput)
	}
}

func TestPrint_RendersExpectedFields(t *testing.T) {
	s := Summary{
		Total:       100,
		Successful:  95,
		Errors:      5,
		Elapsed:     2500 * time.Millisecond,
		Throughput:  40.0,
		AvgLatency:  120 * time.Millisecond,
		StatusCodes: map[int]int{200: 90, 500: 5},
	}
	var buf bytes.Buffer
	Print(&buf, s)
	out := buf.String()

	for _, want := range []string{
		"Total requests:  100",
		"Successful:      95",
		"Errors:          5",
		"Throughput:      40.00 req/sec",
		"Avg latency:     120ms",
		"HTTP 200:",
		"HTTP 500:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestPrint_OmitsLatencyAndStatusWhenAllErrors(t *testing.T) {
	s := Summary{Total: 5, Successful: 0, Errors: 5, Elapsed: time.Second, Throughput: 5.0}
	var buf bytes.Buffer
	Print(&buf, s)
	out := buf.String()

	if strings.Contains(out, "Avg latency") {
		t.Errorf("output should not mention Avg latency when no successes:\n%s", out)
	}
	if strings.Contains(out, "Status codes") {
		t.Errorf("output should not mention Status codes when no successes:\n%s", out)
	}
}
