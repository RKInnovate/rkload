package loader

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxCapturedBody caps how much of a response body is read when a step
// needs it for extraction or assertion, bounding per-VU memory.
const maxCapturedBody = 1 << 20 // 1 MiB

// Step is one request in a scenario chain. It mirrors config.Step (minus
// the config-only auth wiring, added separately) so the loader stays free
// of a config-package import.
type Step struct {
	Name    string
	Method  string
	URL     string
	Headers map[string]string
	Body    string
	Auth    *Auth // overrides the scenario-level auth for this step
	Extract []ExtractRule
	Assert  []AssertRule
}

// ScenarioOptions describes one scenario to run. VUs virtual users each
// run the ordered Steps as a chain, Iterations times in total.
type ScenarioOptions struct {
	Name       string
	VUs        int
	Iterations int
	Timeout    time.Duration
	Steps      []Step
	Auth       *Auth // applied to every step unless the step overrides it
	// OnResult, if set, is invoked once per executed step in drain order
	// (the same contract as Options.OnResult, but per step). The returned
	// slice is exactly the concatenation of all OnResult calls.
	OnResult func(StepResult)
}

// StepResult is the outcome of a single executed step.
type StepResult struct {
	Iteration  int
	StepIndex  int
	StepName   string
	Duration   time.Duration
	StatusCode int
	Err        error  // transport / request-build failure
	AssertErr  error  // assertion failure (distinct, still counts as a failure)
	Body       []byte // populated only when the step needs it; nil otherwise
}

// RunScenario runs a scenario chain with a bounded pool of VUs workers.
// Each virtual user owns its own *http.Client (so keep-alive connections
// are reused across the chain) and a fresh variable map per iteration. A
// step whose Err or AssertErr is set aborts the remaining steps of that
// iteration; downstream steps emit no results.
func RunScenario(opts ScenarioOptions) []StepResult {
	jobs := make(chan int, opts.Iterations)
	for i := 0; i < opts.Iterations; i++ {
		jobs <- i
	}
	close(jobs)

	results := make(chan StepResult, opts.Iterations*max(1, len(opts.Steps)))
	var wg sync.WaitGroup
	for v := 0; v < opts.VUs; v++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: opts.Timeout}
			for iter := range jobs {
				vars := map[string]string{}
				for si := range opts.Steps {
					sr := execStep(client, opts.Steps[si], opts.Auth, si, iter, vars)
					results <- sr
					if sr.Err != nil || sr.AssertErr != nil {
						break // abort the rest of this chain iteration
					}
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	out := make([]StepResult, 0, cap(results))
	for r := range results {
		if opts.OnResult != nil {
			opts.OnResult(r)
		}
		out = append(out, r)
	}
	return out
}

// execStep issues one step's request, resolving ${var} placeholders from
// vars, then runs the step's extract and assert rules. The response body
// is read only when a rule needs it; otherwise it is drained (so the VU's
// connection can be reused) and discarded.
func execStep(client *http.Client, step Step, scenAuth *Auth, stepIndex, iter int, vars map[string]string) StepResult {
	sr := StepResult{Iteration: iter, StepIndex: stepIndex, StepName: step.Name}

	var body io.Reader
	if step.Body != "" {
		body = strings.NewReader(interpolate(step.Body, vars))
	}
	req, err := http.NewRequest(stepMethod(step.Method), interpolate(step.URL, vars), body)
	if err != nil {
		sr.Err = err
		return sr
	}
	// Apply the effective auth (step overrides scenario) before explicit
	// headers, so an explicit per-step header still wins.
	if effAuth := step.Auth; effAuth != nil || scenAuth != nil {
		if effAuth == nil {
			effAuth = scenAuth
		}
		if err := applyAuth(req, *effAuth, vars); err != nil {
			sr.Err = err
			return sr
		}
	}
	for k, v := range step.Headers {
		req.Header.Set(k, interpolate(v, vars))
	}

	start := time.Now()
	resp, err := client.Do(req)
	sr.Duration = time.Since(start)
	if err != nil {
		sr.Err = err
		return sr
	}
	defer resp.Body.Close()
	sr.StatusCode = resp.StatusCode

	var respBody []byte
	if stepNeedsBody(step) {
		respBody, _ = io.ReadAll(io.LimitReader(resp.Body, maxCapturedBody))
		sr.Body = respBody
	} else {
		// Drain so the keep-alive connection can be reused across the chain.
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	if err := runExtracts(step.Extract, vars, resp.StatusCode, resp.Header, respBody); err != nil {
		sr.Err = err // a failed extraction fails the step
		return sr
	}
	if err := runAsserts(step.Assert, resp.StatusCode, respBody); err != nil {
		sr.AssertErr = err
		return sr
	}
	return sr
}

// stepNeedsBody reports whether any of a step's rules require the response
// body (JSON/regex extraction, or a body-oriented assertion). Header- and
// status-only steps skip the read entirely.
func stepNeedsBody(step Step) bool {
	for _, e := range step.Extract {
		if e.From == "json" || e.From == "regex" {
			return true
		}
	}
	for _, a := range step.Assert {
		if a.Type == "body-contains" || a.Type == "json-equals" {
			return true
		}
	}
	return false
}

func stepMethod(m string) string {
	if m == "" {
		return http.MethodGet
	}
	return m
}
