// Package tui renders a live dashboard for an rkload run.
//
// The TUI is a bubbletea program that consumes Result messages
// produced by loader.Run via its OnResult callback and renders:
//
//   - per-endpoint progress bars, counter, throughput, live p95
//   - aggregate status code distribution updating live
//   - throughput sparkline over the recent past
//   - rolling p50/p95/p99 latency tickers
//
// It is intentionally optional: when stdout is not a TTY (CI,
// pipe, redirect), the cmd layer skips the TUI and uses the
// existing plain-text reporter. When it IS a TTY, the TUI runs
// during the load test and the plain-text aggregate report still
// prints after the TUI exits, so `rkload ... | tee log.txt` still
// captures meaningful, grep-able text.
package tui

import (
	"time"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/loader"
)

// Endpoint captures the static information about one endpoint
// the TUI is rendering. It is built once at start of the run and
// never mutates.
type Endpoint struct {
	Method string
	Name   string
	URL    string
	Total  int // total requests the loader will issue
}

// EndpointFromConfig converts a (method, config.Endpoint) pair
// into the TUI's display form, picking the name fallback in
// the same order the runtime report uses.
func EndpointFromConfig(method string, ep config.Endpoint) Endpoint {
	name := ep.Name
	if name == "" {
		name = ep.URL
	}
	return Endpoint{
		Method: method,
		Name:   name,
		URL:    ep.URL,
		Total:  ep.Requests,
	}
}

// endpointState is mutable state per endpoint, updated as
// results stream in.
type endpointState struct {
	completed   int
	statusCodes map[int]int
	errCount    int
	latencies   []time.Duration
	startedAt   time.Time
	finishedAt  time.Time
	finished    bool
}

func newEndpointState() endpointState {
	return endpointState{
		statusCodes: make(map[int]int),
		latencies:   make([]time.Duration, 0, 256),
	}
}

func (s *endpointState) record(r loader.Result) {
	s.completed++
	if r.Err != nil {
		s.errCount++
	} else {
		s.statusCodes[r.StatusCode]++
		s.latencies = append(s.latencies, r.Duration)
	}
}

func (s *endpointState) throughput(now time.Time) float64 {
	if s.startedAt.IsZero() {
		return 0
	}
	end := now
	if s.finished {
		end = s.finishedAt
	}
	elapsed := end.Sub(s.startedAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(s.completed) / elapsed
}

// Model is the bubbletea Model for the rkload TUI.
type Model struct {
	endpoints   []Endpoint
	states      []endpointState
	currentIdx  int // -1 = nothing started yet; len(endpoints) = all done
	selectedIdx int // for keyboard navigation; defaults to currentIdx

	// Aggregate, rebuilt from per-endpoint states each render.
	startedAt time.Time
	endedAt   time.Time

	// Throughput sparkline: ring buffer of req/sec samples taken
	// every sampleInterval. The Update tick advances it.
	throughputHistory []float64
	lastTickAt        time.Time
	lastTickCompleted int // completed count at last tick, for delta-based req/sec

	// Terminal dimensions (refreshed on WindowSizeMsg).
	width, height int

	// UI state.
	drillMode bool
	quitting  bool
}

// New constructs a fresh Model for the given endpoints. currentIdx
// starts at -1 so View can show a "waiting to start" state until
// the first EndpointStart message arrives.
func New(endpoints []Endpoint) Model {
	states := make([]endpointState, len(endpoints))
	for i := range states {
		states[i] = newEndpointState()
	}
	return Model{
		endpoints:         endpoints,
		states:            states,
		currentIdx:        -1,
		selectedIdx:       0,
		throughputHistory: make([]float64, 0, sparklineWidth),
	}
}

// done returns true when every endpoint has been marked finished.
func (m *Model) done() bool {
	if len(m.endpoints) == 0 {
		return true
	}
	return m.currentIdx >= len(m.endpoints)
}

// totalRequests reports the sum of Total across all endpoints —
// the denominator for the overall progress bar.
func (m *Model) totalRequests() int {
	t := 0
	for _, e := range m.endpoints {
		t += e.Total
	}
	return t
}

// completedRequests reports the running total across all endpoints.
func (m *Model) completedRequests() int {
	c := 0
	for _, s := range m.states {
		c += s.completed
	}
	return c
}
