package loader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// chainServer serves a login → call → logout flow. /me returns 401 unless
// the Authorization header carries the token minted by /login, so a passing
// run proves the token was extracted and injected.
func chainServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"accessToken": "TOK", "tokenType": "Bearer"},
		})
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOK" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {})
	return httptest.NewServer(mux)
}

func loginCallLogout(base string) []Step {
	return []Step{
		{
			Name: "login", Method: "POST", URL: base + "/login",
			Extract: []ExtractRule{{Var: "token", From: "json", Path: "data.accessToken"}},
			Assert: []AssertRule{
				{Type: "status", Equals: 200},
				{Type: "body-contains", Value: "accessToken"},
			},
		},
		{
			Name: "call", Method: "GET", URL: base + "/me",
			Headers: map[string]string{"Authorization": "Bearer ${token}"},
			Assert:  []AssertRule{{Type: "status", Equals: 200}},
		},
		{
			Name: "logout", Method: "POST", URL: base + "/logout",
			Headers: map[string]string{"Authorization": "Bearer ${token}"},
		},
	}
}

func TestRunScenario_FullChain(t *testing.T) {
	srv := chainServer()
	defer srv.Close()

	results := RunScenario(ScenarioOptions{
		VUs: 2, Iterations: 4, Timeout: 5 * time.Second,
		Steps: loginCallLogout(srv.URL),
	})

	if len(results) != 4*3 {
		t.Fatalf("results = %d, want 12 (4 iters x 3 steps)", len(results))
	}
	for _, r := range results {
		if r.Err != nil || r.AssertErr != nil {
			t.Errorf("step %q failed: err=%v assert=%v", r.StepName, r.Err, r.AssertErr)
		}
	}
}

func TestRunScenario_BoundsVUs(t *testing.T) {
	var inFlight, maxSeen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxSeen)
			if n <= old || atomic.CompareAndSwapInt32(&maxSeen, old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
	}))
	defer srv.Close()

	const vus = 3
	RunScenario(ScenarioOptions{
		VUs: vus, Iterations: 30, Timeout: 5 * time.Second,
		Steps: []Step{{Method: "GET", URL: srv.URL}},
	})
	if int(maxSeen) > vus {
		t.Errorf("max concurrent = %d, exceeds VUs = %d", maxSeen, vus)
	}
}

func TestRunScenario_AssertFailureAbortsChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	results := RunScenario(ScenarioOptions{
		VUs: 1, Iterations: 1, Timeout: 5 * time.Second,
		Steps: []Step{
			{Name: "a", Method: "GET", URL: srv.URL, Assert: []AssertRule{{Type: "status", Equals: 200}}},
			{Name: "b", Method: "GET", URL: srv.URL},
		},
	})
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (step b must be skipped)", len(results))
	}
	if results[0].AssertErr == nil {
		t.Errorf("step a should carry an assertion failure, got %+v", results[0])
	}
}

func TestRunScenario_TransportErrorAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close() // now connections are refused

	results := RunScenario(ScenarioOptions{
		VUs: 1, Iterations: 1, Timeout: 2 * time.Second,
		Steps: []Step{
			{Name: "a", Method: "GET", URL: dead},
			{Name: "b", Method: "GET", URL: dead},
		},
	})
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (step b skipped after transport error)", len(results))
	}
	if results[0].Err == nil {
		t.Errorf("step a should carry a transport error, got %+v", results[0])
	}
}

func TestRunScenario_OnResultOrderMatchesSlice(t *testing.T) {
	srv := chainServer()
	defer srv.Close()

	var mu sync.Mutex
	var seen []StepResult
	results := RunScenario(ScenarioOptions{
		VUs: 2, Iterations: 3, Timeout: 5 * time.Second,
		Steps: loginCallLogout(srv.URL),
		OnResult: func(sr StepResult) {
			mu.Lock()
			seen = append(seen, sr)
			mu.Unlock()
		},
	})
	if len(seen) != len(results) {
		t.Fatalf("OnResult fired %d times, slice has %d", len(seen), len(results))
	}
	for i := range results {
		if seen[i].StepName != results[i].StepName || seen[i].StatusCode != results[i].StatusCode {
			t.Errorf("OnResult order diverges from slice at %d", i)
		}
	}
}

func TestStepNeedsBody(t *testing.T) {
	if stepNeedsBody(Step{Extract: []ExtractRule{{From: "status"}}, Assert: []AssertRule{{Type: "status"}}}) {
		t.Error("status-only step should not need the body")
	}
	if !stepNeedsBody(Step{Extract: []ExtractRule{{From: "json"}}}) {
		t.Error("json extract should need the body")
	}
	if !stepNeedsBody(Step{Assert: []AssertRule{{Type: "body-contains"}}}) {
		t.Error("body-contains assert should need the body")
	}
}
