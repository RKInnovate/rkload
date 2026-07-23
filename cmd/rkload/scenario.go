// scenario.go wires config-declared scenarios (schema v2) into the run
// flow: it maps a config.Scenario onto the loader's chain-execution
// options, runs it, and reports one summary per step. This mirrors the
// endpoint path (runOneEndpoint) — the config→loader mapping lives in cmd
// so the loader stays free of a config-package import.
package main

import (
	"fmt"
	"io"
	"time"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/loader"
	"github.com/RKInnovate/rkload/internal/report"
)

// scenarioJob is the work item for one scenario. Its steps occupy the
// flat row indices firstIdx .. firstIdx+len(steps)-1, continuing the
// counter after the endpoint rows so results route to the right TUI row.
type scenarioJob struct {
	scen     config.Scenario
	origin   string
	firstIdx int
}

// flattenScenarios walks every bundle's scenarios in order, giving each
// scenario's steps a contiguous run of flat row indices starting at
// startIdx (which continues after the endpoint rows).
func flattenScenarios(bundles []configBundle, startIdx int) []scenarioJob {
	var jobs []scenarioJob
	idx := startIdx
	for _, b := range bundles {
		for _, s := range b.cfg.Scenarios {
			jobs = append(jobs, scenarioJob{scen: s, origin: b.path, firstIdx: idx})
			idx += len(s.Steps)
		}
	}
	return jobs
}

// toLoaderScenario maps a config.Scenario onto the loader's execution
// options, mirroring how runOneEndpoint maps config.Endpoint onto
// loader.Options. (Auth is wired separately.)
func toLoaderScenario(s config.Scenario) loader.ScenarioOptions {
	timeout, _ := s.ParsedTimeout() // Validate already proved this parses
	steps := make([]loader.Step, len(s.Steps))
	for i, st := range s.Steps {
		steps[i] = loader.Step{
			Name:    st.Name,
			Method:  st.Method,
			URL:     st.URL,
			Headers: st.Headers,
			Body:    st.Body,
			Auth:    toLoaderAuth(st.Auth),
			Extract: toLoaderExtracts(st.Extract),
			Assert:  toLoaderAsserts(st.Assert),
		}
	}
	return loader.ScenarioOptions{
		Name:       s.Name,
		VUs:        s.VUs,
		Iterations: s.Iterations,
		Timeout:    timeout,
		Steps:      steps,
		Auth:       toLoaderAuth(s.Auth),
	}
}

// toLoaderAuth maps a config.Auth onto the loader's copy. Returns nil for
// a nil input (no auth configured).
func toLoaderAuth(a *config.Auth) *loader.Auth {
	if a == nil {
		return nil
	}
	return &loader.Auth{
		Type:         a.Type,
		Token:        a.Token,
		Header:       a.Header,
		Username:     a.Username,
		Password:     a.Password,
		ClientID:     a.ClientID,
		ClientSecret: a.ClientSecret,
		TokenURL:     a.TokenURL,
		Scopes:       a.Scopes,
	}
}

func toLoaderExtracts(rules []config.ExtractRule) []loader.ExtractRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]loader.ExtractRule, len(rules))
	for i, r := range rules {
		out[i] = loader.ExtractRule{Var: r.Var, From: r.From, Path: r.Path, Name: r.Name, Pattern: r.Pattern}
	}
	return out
}

func toLoaderAsserts(rules []config.AssertRule) []loader.AssertRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]loader.AssertRule, len(rules))
	for i, r := range rules {
		out[i] = loader.AssertRule{Type: r.Type, Equals: r.Equals, Value: r.Value, Path: r.Path}
	}
	return out
}

// adaptStepResult flattens a StepResult into the report/TUI Result shape.
// An assertion failure is surfaced as an error so it counts toward
// Summary.Errors (and therefore the non-zero exit policy).
func adaptStepResult(sr loader.StepResult) loader.Result {
	err := sr.Err
	if err == nil {
		err = sr.AssertErr
	}
	return loader.Result{Duration: sr.Duration, StatusCode: sr.StatusCode, Err: err}
}

// runOneScenario runs a scenario chain to completion and returns one
// report.Summary per step (index i == step i). When onResult is non-nil
// it fires live for every executed step, tagged with the step's flat row
// index (firstIdx+StepIndex) so a live UI can route it to the right row.
func runOneScenario(sj scenarioJob, onResult func(row int, r loader.Result)) []report.Summary {
	opts := toLoaderScenario(sj.scen)
	if onResult != nil {
		opts.OnResult = func(sr loader.StepResult) {
			onResult(sj.firstIdx+sr.StepIndex, adaptStepResult(sr))
		}
	}
	start := time.Now()
	all := loader.RunScenario(opts)
	elapsed := time.Since(start)

	perStep := make([][]loader.Result, len(sj.scen.Steps))
	for _, sr := range all {
		perStep[sr.StepIndex] = append(perStep[sr.StepIndex], adaptStepResult(sr))
	}
	summaries := make([]report.Summary, len(sj.scen.Steps))
	for i := range perStep {
		summaries[i] = report.Summarize(perStep[i], elapsed)
	}
	return summaries
}

// runScenariosPlain runs every scenario in the plain (non-TUI) path,
// printing one report per step and accumulating request and error totals.
// Returns the (requests, errors) it added so the caller can fold them into
// the Overall footer and the exit-code decision.
func runScenariosPlain(w io.Writer, scenarios []scenarioJob) (requests, errors int) {
	for _, sj := range scenarios {
		fmt.Fprintf(w, "=== scenario %s ===\n", scenarioLabel(sj.scen))
		fmt.Fprintf(w, "VUs: %d | Iterations: %d | Steps: %d | Timeout: %s\n\n",
			sj.scen.VUs, sj.scen.Iterations, len(sj.scen.Steps), sj.scen.Timeout)

		summaries := runOneScenario(sj, nil)
		for i, st := range sj.scen.Steps {
			fmt.Fprintf(w, "--- step %d/%d: %s %s ---\n", i+1, len(sj.scen.Steps), st.Method, stepLabel(st))
			report.Print(w, summaries[i])
			fmt.Fprintln(w)
			requests += summaries[i].Total
			errors += summaries[i].Errors
		}
	}
	return requests, errors
}

func scenarioLabel(s config.Scenario) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("(%d steps)", len(s.Steps))
}

func stepLabel(st config.Step) string {
	if st.Name != "" {
		return st.Name
	}
	return st.URL
}
