package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RKInnovate/rkload/internal/loader"

	tea "github.com/charmbracelet/bubbletea"
)

// helper: send a message through Update and unwrap back to Model.
func step(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func sampleEndpoints() []Endpoint {
	return []Endpoint{
		{Method: "GET", Name: "first", URL: "https://example.com/a", Total: 5},
		{Method: "POST", Name: "second", URL: "https://example.com/b", Total: 3},
	}
}

// ---- Model construction --------------------------------------------------

func TestNew_InitialState(t *testing.T) {
	m := New(sampleEndpoints())
	if m.currentIdx != -1 {
		t.Errorf("currentIdx = %d, want -1 (nothing started yet)", m.currentIdx)
	}
	if m.selectedIdx != 0 {
		t.Errorf("selectedIdx = %d, want 0", m.selectedIdx)
	}
	if len(m.states) != 2 {
		t.Fatalf("len(states) = %d, want 2", len(m.states))
	}
	for i, s := range m.states {
		if s.statusCodes == nil {
			t.Errorf("states[%d].statusCodes is nil; should be initialised", i)
		}
		if s.completed != 0 {
			t.Errorf("states[%d].completed = %d, want 0", i, s.completed)
		}
	}
}

// ---- Update: EndpointStart / EndpointEnd ----------------------------------

func TestUpdate_EndpointStartMarksCurrentAndSetsTimer(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})

	if m.currentIdx != 0 {
		t.Errorf("currentIdx = %d, want 0", m.currentIdx)
	}
	if m.startedAt.IsZero() {
		t.Errorf("startedAt should be set on first EndpointStart")
	}
	if m.states[0].startedAt.IsZero() {
		t.Errorf("states[0].startedAt should be set")
	}
}

func TestUpdate_EndpointEndFinalises(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})
	m = step(m, EndpointEnd{Index: 0})

	if !m.states[0].finished {
		t.Errorf("states[0].finished should be true after EndpointEnd")
	}
	if m.states[0].finishedAt.IsZero() {
		t.Errorf("states[0].finishedAt should be set")
	}
}

// ---- Update: result recording --------------------------------------------

func TestUpdate_ResultMsgIncrementsStateAndTracksStatus(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})

	for _, code := range []int{200, 200, 500, 200} {
		m = step(m, Result(0, loader.Result{Duration: 100 * time.Millisecond, StatusCode: code}))
	}

	if m.states[0].completed != 4 {
		t.Errorf("completed = %d, want 4", m.states[0].completed)
	}
	if m.states[0].statusCodes[200] != 3 {
		t.Errorf("200s = %d, want 3", m.states[0].statusCodes[200])
	}
	if m.states[0].statusCodes[500] != 1 {
		t.Errorf("500s = %d, want 1", m.states[0].statusCodes[500])
	}
	if len(m.states[0].latencies) != 4 {
		t.Errorf("latencies len = %d, want 4 (all errors are nil)", len(m.states[0].latencies))
	}
}

func TestUpdate_ResultMsgWithErrorIncrementsErrCount(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})

	m = step(m, Result(0, loader.Result{Err: dummyErr("conn refused")}))

	if m.states[0].errCount != 1 {
		t.Errorf("errCount = %d, want 1", m.states[0].errCount)
	}
	if len(m.states[0].statusCodes) != 0 {
		t.Errorf("errored result should not add a status code, got %v", m.states[0].statusCodes)
	}
	// Latencies must NOT include error results (they'd skew percentiles).
	if len(m.states[0].latencies) != 0 {
		t.Errorf("latencies should be empty when only result is an error")
	}
}

// ---- Update: navigation ---------------------------------------------------

func TestUpdate_ArrowKeysMoveSelection(t *testing.T) {
	m := New(sampleEndpoints())
	if m.selectedIdx != 0 {
		t.Fatalf("starting selectedIdx = %d, want 0", m.selectedIdx)
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedIdx != 1 {
		t.Errorf("after down, selectedIdx = %d, want 1", m.selectedIdx)
	}
	// At last endpoint — down stays put.
	m = step(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedIdx != 1 {
		t.Errorf("at last endpoint, down should not wrap; got selectedIdx = %d", m.selectedIdx)
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.selectedIdx != 0 {
		t.Errorf("after up, selectedIdx = %d, want 0", m.selectedIdx)
	}
	// At first endpoint — up stays put.
	m = step(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.selectedIdx != 0 {
		t.Errorf("at first endpoint, up should not wrap; got selectedIdx = %d", m.selectedIdx)
	}
}

func TestUpdate_EnterEntersDrillMode_EscExits(t *testing.T) {
	m := New(sampleEndpoints())
	if m.drillMode {
		t.Fatal("drillMode should start false")
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.drillMode {
		t.Errorf("enter should enter drillMode")
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.drillMode {
		t.Errorf("esc should exit drillMode")
	}
}

// ---- Update: done ---------------------------------------------------------

func TestUpdate_DoneSetsTerminalStateAndQuits(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})
	next, cmd := m.Update(Done{})
	final := next.(Model)
	if !final.done() {
		t.Errorf("Done should mark model as done")
	}
	if final.endedAt.IsZero() {
		t.Errorf("Done should set endedAt")
	}
	if cmd == nil {
		t.Errorf("Done should return tea.Quit command (got nil)")
	}
}

// ---- View rendering -------------------------------------------------------

// withSize gives the model a terminal-size message so the windowed
// endpoint table has room for all endpoints. Without this, tests
// would see "N more ↓" hints for everything past the minimum window.
func withSize(m Model) Model {
	return step(m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

func TestView_OverviewMentionsEachEndpoint(t *testing.T) {
	m := withSize(New(sampleEndpoints()))
	m = step(m, EndpointStart{Index: 0})
	out := m.View()
	if !strings.Contains(out, "first") {
		t.Errorf("view should mention endpoint 'first'; got:\n%s", out)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("view should mention endpoint 'second'; got:\n%s", out)
	}
}

func TestView_OverviewShowsAggregateStatusCounts(t *testing.T) {
	m := withSize(New(sampleEndpoints()))
	m = step(m, EndpointStart{Index: 0})
	for i := 0; i < 3; i++ {
		m = step(m, Result(0, loader.Result{Duration: 10 * time.Millisecond, StatusCode: 200}))
	}
	out := m.View()
	if !strings.Contains(out, "200") {
		t.Errorf("overview should mention 200 status code; got:\n%s", out)
	}
	if !strings.Contains(out, "Latency") {
		t.Errorf("overview should have Latency line")
	}
	if !strings.Contains(out, "Throughput") {
		t.Errorf("overview should have Throughput line")
	}
}

// TestView_LargeListShowsScrollHint — when the terminal is too
// short for every endpoint, the windowed table should slice and
// include a "more" hint. Pins the new height-aware rendering.
func TestView_LargeListShowsScrollHint(t *testing.T) {
	endpoints := make([]Endpoint, 30)
	for i := range endpoints {
		endpoints[i] = Endpoint{Method: "GET", Name: fmt.Sprintf("ep%02d", i), Total: 10}
	}
	m := New(endpoints)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 20})
	out := m.View()
	if !strings.Contains(out, "more") {
		t.Errorf("expected a 'more' scroll hint when terminal is short; got:\n%s", out)
	}
}

// TestView_WindowFollowsSelection — moving the selection past the
// visible window should slide the window so the selected row stays
// on screen.
func TestView_WindowFollowsSelection(t *testing.T) {
	endpoints := make([]Endpoint, 30)
	for i := range endpoints {
		endpoints[i] = Endpoint{Method: "GET", Name: fmt.Sprintf("ep%02d", i), Total: 10}
	}
	m := New(endpoints)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 20})
	// Move selection to the last endpoint.
	for i := 0; i < 30; i++ {
		m = step(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	out := m.View()
	if !strings.Contains(out, "ep29") {
		t.Errorf("view should show the selected endpoint ep29 after scrolling; got:\n%s", out)
	}
}

func TestView_DrilldownShowsSelectedEndpoint(t *testing.T) {
	m := withSize(New(sampleEndpoints()))
	m.selectedIdx = 1
	m.drillMode = true
	m = step(m, EndpointStart{Index: 1})
	m = step(m, Result(1, loader.Result{Duration: 50 * time.Millisecond, StatusCode: 201}))
	out := m.View()
	if !strings.Contains(out, "second") {
		t.Errorf("drilldown should show selected endpoint name; got:\n%s", out)
	}
	if !strings.Contains(out, "example.com/b") {
		t.Errorf("drilldown should show the URL; got:\n%s", out)
	}
	if !strings.Contains(out, "201") {
		t.Errorf("drilldown should show status code; got:\n%s", out)
	}
}

// ---- Sparkline / percentile helpers --------------------------------------

func TestRenderSparkline_EmptyReturnsEmpty(t *testing.T) {
	if s := renderSparkline(nil); s != "" {
		t.Errorf("empty input should yield empty string, got %q", s)
	}
}

func TestRenderSparkline_AllZeroes(t *testing.T) {
	s := renderSparkline([]float64{0, 0, 0, 0})
	if len([]rune(s)) != 4 {
		t.Errorf("rune count = %d, want 4", len([]rune(s)))
	}
}

func TestRenderSparkline_LevelsScaleToMax(t *testing.T) {
	// Two samples; the larger should produce the top rune, the
	// smaller should produce a lower rune.
	s := renderSparkline([]float64{1, 10})
	runes := []rune(s)
	if len(runes) != 2 {
		t.Fatalf("rune count = %d, want 2", len(runes))
	}
	if runes[1] != sparkRunes[len(sparkRunes)-1] {
		t.Errorf("max sample did not render as top rune; got %q", string(runes[1]))
	}
	if runes[0] == runes[1] {
		t.Errorf("smaller sample should not match max sample's rune")
	}
}

func TestPercentile_NearestRank(t *testing.T) {
	durs := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	}
	cases := []struct {
		p    int
		want time.Duration
	}{
		{50, 3 * time.Millisecond}, // ceil(0.5*5)=3 → index 2
		{95, 5 * time.Millisecond}, // ceil(0.95*5)=5 → index 4
		{99, 5 * time.Millisecond},
	}
	for _, c := range cases {
		got := percentile(append([]time.Duration(nil), durs...), c.p)
		if got != c.want {
			t.Errorf("percentile(p=%d) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestPercentile_EmptyReturnsZero(t *testing.T) {
	if got := percentile(nil, 95); got != 0 {
		t.Errorf("empty input should return 0, got %v", got)
	}
}

// ---- formatDuration -------------------------------------------------------

func TestFormatDuration_Buckets(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Nanosecond, "500ns"},
		{50 * time.Microsecond, "50µs"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.50s"},
		{61 * time.Second, "1m01s"},
	}
	for _, c := range cases {
		got := formatDuration(c.in)
		if got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- Sparkline advance ----------------------------------------------------

func TestAdvanceSparkline_AddsSampleBasedOnDelta(t *testing.T) {
	m := New(sampleEndpoints())
	m = step(m, EndpointStart{Index: 0})

	// 10 completed in 1 second → 10 r/s
	for i := 0; i < 10; i++ {
		m = step(m, Result(0, loader.Result{Duration: 10 * time.Millisecond, StatusCode: 200}))
	}
	t0 := m.lastTickAt
	m = step(m, tickMsg{At: t0.Add(time.Second)})
	if len(m.throughputHistory) != 1 {
		t.Fatalf("after first sampled tick, history len = %d, want 1", len(m.throughputHistory))
	}
	if m.throughputHistory[0] != 10 {
		t.Errorf("rps = %v, want 10", m.throughputHistory[0])
	}
}

// dummyErr is a tiny error type used by tests that need a non-nil
// error without pulling in network dependencies.
type dummyErr string

func (e dummyErr) Error() string { return string(e) }
