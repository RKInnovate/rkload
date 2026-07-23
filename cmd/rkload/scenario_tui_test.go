package main

import (
	"testing"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/report"

	"github.com/charmbracelet/lipgloss"
)

func TestBuildTUIRows_EndpointsThenSteps(t *testing.T) {
	jobs := []endpointJob{
		{method: "GET", ep: config.Endpoint{URL: "https://x/a", Requests: 100}, idx: 0},
		{method: "POST", ep: config.Endpoint{Name: "create", URL: "https://x/b", Requests: 50}, idx: 1},
	}
	scen := []scenarioJob{{
		scen: config.Scenario{Name: "flow", Iterations: 20, Steps: []config.Step{
			{Name: "login", Method: "POST", URL: "https://x/login"},
			{Name: "call", Method: "GET", URL: "https://x/me"},
		}},
		firstIdx: 2,
	}}

	endpoints, rows := buildTUIRows(jobs, scen)
	if len(endpoints) != 4 || len(rows) != 4 {
		t.Fatalf("endpoints=%d rows=%d, want 4/4", len(endpoints), len(rows))
	}
	// Endpoint rows keep their Requests as the TUI Total.
	if endpoints[0].Total != 100 {
		t.Errorf("endpoint row Total = %d, want 100", endpoints[0].Total)
	}
	// Step rows use the scenario's Iterations as Total and a scenario/step label.
	if endpoints[2].Total != 20 || endpoints[2].Name != "flow/login" {
		t.Errorf("step row 0 = %+v, want Total 20 / flow/login", endpoints[2])
	}
	if rows[3].idx != 3 || rows[3].method != "GET" || rows[3].label != "flow/call" {
		t.Errorf("step row 1 = %+v, want idx 3 / GET / flow/call", rows[3])
	}
}

func TestHasUnexpectedResults_Rows(t *testing.T) {
	rows := []row{{idx: 0}, {idx: 1}}
	done := []bool{true, true}

	clean := []report.Summary{{StatusCodes: map[int]int{200: 5}}, {StatusCodes: map[int]int{204: 3}}}
	if hasUnexpectedResults(rows, clean, done) {
		t.Error("all-2xx rows should not be unexpected")
	}

	bad := []report.Summary{{StatusCodes: map[int]int{200: 5}}, {StatusCodes: map[int]int{500: 1}}}
	if !hasUnexpectedResults(rows, bad, done) {
		t.Error("a 5xx row should be unexpected")
	}

	errored := []report.Summary{{Errors: 1}, {StatusCodes: map[int]int{200: 1}}}
	if !hasUnexpectedResults(rows, errored, done) {
		t.Error("a transport error should be unexpected")
	}
}

func TestRowColorFromIndex_Rows(t *testing.T) {
	green, red, dim := lipgloss.Color("42"), lipgloss.Color("203"), lipgloss.Color("241")
	rows := []row{{idx: 0}, {idx: 1}}
	done := []bool{true, true}
	summaries := []report.Summary{
		{StatusCodes: map[int]int{200: 5}},
		{StatusCodes: map[int]int{500: 1}},
	}
	if got := rowColorFromIndex(0, rows, summaries, done, green, red, dim); got != green {
		t.Errorf("clean row color = %v, want green", got)
	}
	if got := rowColorFromIndex(1, rows, summaries, done, green, red, dim); got != red {
		t.Errorf("failed row color = %v, want red", got)
	}
	if got := rowColorFromIndex(9, rows, summaries, done, green, red, dim); got != dim {
		t.Errorf("out-of-range row color = %v, want dim", got)
	}
}
