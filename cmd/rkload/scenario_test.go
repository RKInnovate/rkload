package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/loader"
)

// chainTestServer serves login → call → logout; /me is 401 unless the
// token minted by /login is injected into the Authorization header.
func chainTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
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

func chainScenario(base string) config.Scenario {
	return config.Scenario{
		Name: "login-call-logout", VUs: 2, Iterations: 4, Timeout: "5s",
		Steps: []config.Step{
			{
				Name: "login", Method: "POST", URL: base + "/login",
				Extract: []config.ExtractRule{{Var: "token", From: "json", Path: "data.accessToken"}},
				Assert:  []config.AssertRule{{Type: "status", Equals: 200}, {Type: "body-contains", Value: "accessToken"}},
			},
			{
				Name: "call", Method: "GET", URL: base + "/me",
				Headers: map[string]string{"Authorization": "Bearer ${token}"},
				Assert:  []config.AssertRule{{Type: "status", Equals: 200}},
			},
			{
				Name: "logout", Method: "POST", URL: base + "/logout",
				Headers: map[string]string{"Authorization": "Bearer ${token}"},
			},
		},
	}
}

func TestRunOneScenario_FullChain(t *testing.T) {
	srv := chainTestServer()
	defer srv.Close()

	sj := scenarioJob{scen: chainScenario(srv.URL)}
	summaries := runOneScenario(sj, nil)

	if len(summaries) != 3 {
		t.Fatalf("summaries = %d, want 3 (one per step)", len(summaries))
	}
	for i, s := range summaries {
		if s.Total != 4 || s.Errors != 0 || s.Successful != 4 {
			t.Errorf("step %d summary wrong: total=%d ok=%d err=%d", i, s.Total, s.Successful, s.Errors)
		}
	}
}

func TestRunOneScenario_AssertFailureIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sj := scenarioJob{scen: config.Scenario{
		VUs: 1, Iterations: 1, Timeout: "5s",
		Steps: []config.Step{{Method: "GET", URL: srv.URL, Assert: []config.AssertRule{{Type: "status", Equals: 200}}}},
	}}
	summaries := runOneScenario(sj, nil)
	if summaries[0].Errors == 0 {
		t.Errorf("assertion failure should count as an error, got %+v", summaries[0])
	}
}

func TestToLoaderScenario_MapsExtractAssert(t *testing.T) {
	opts := toLoaderScenario(config.Scenario{
		Name: "s", VUs: 3, Iterations: 9, Timeout: "2s",
		Steps: []config.Step{{
			Method: "GET", URL: "https://x/",
			Extract: []config.ExtractRule{{Var: "t", From: "json", Path: "a.b"}},
			Assert:  []config.AssertRule{{Type: "json-equals", Path: "a.b", Value: "c"}},
		}},
	})
	if opts.VUs != 3 || opts.Iterations != 9 || len(opts.Steps) != 1 {
		t.Fatalf("scenario fields not mapped: %+v", opts)
	}
	st := opts.Steps[0]
	if len(st.Extract) != 1 || st.Extract[0].Var != "t" || st.Extract[0].Path != "a.b" {
		t.Errorf("extract not mapped: %+v", st.Extract)
	}
	if len(st.Assert) != 1 || st.Assert[0].Type != "json-equals" || st.Assert[0].Value != "c" {
		t.Errorf("assert not mapped: %+v", st.Assert)
	}
}

func TestToLoaderAuth_MapsFieldsAndNil(t *testing.T) {
	if toLoaderAuth(nil) != nil {
		t.Error("nil config auth should map to nil loader auth")
	}
	a := toLoaderAuth(&config.Auth{Type: "basic", Username: "u", Password: "p", TokenURL: "https://t/"})
	if a == nil || a.Type != "basic" || a.Username != "u" || a.Password != "p" || a.TokenURL != "https://t/" {
		t.Errorf("auth fields not mapped: %+v", a)
	}
}

func TestAdaptStepResult_AssertErrIsError(t *testing.T) {
	r := adaptStepResult(loader.StepResult{StatusCode: 200, AssertErr: errors.New("boom")})
	if r.Err == nil {
		t.Error("AssertErr should surface as Result.Err")
	}
	ok := adaptStepResult(loader.StepResult{StatusCode: 200})
	if ok.Err != nil {
		t.Errorf("clean step should have nil Err, got %v", ok.Err)
	}
}

func TestFlattenScenarios_ContiguousIndices(t *testing.T) {
	cfg := &config.Config{
		Version: 2,
		GET:     []config.Endpoint{{URL: "https://x/a"}, {URL: "https://x/b"}},
		Scenarios: []config.Scenario{
			{Name: "s1", Steps: []config.Step{{URL: "https://x/1"}, {URL: "https://x/2"}}},
			{Name: "s2", Steps: []config.Step{{URL: "https://x/3"}}},
		},
	}
	bundles := []configBundle{{cfg: cfg, path: "c.json"}}
	jobs := flattenJobs(bundles)
	scen := flattenScenarios(bundles, len(jobs))

	if len(jobs) != 2 {
		t.Fatalf("endpoint jobs = %d, want 2", len(jobs))
	}
	if scen[0].firstIdx != 2 {
		t.Errorf("scenario 1 firstIdx = %d, want 2 (after 2 endpoints)", scen[0].firstIdx)
	}
	if scen[1].firstIdx != 4 {
		t.Errorf("scenario 2 firstIdx = %d, want 4 (after 2 endpoints + 2 steps)", scen[1].firstIdx)
	}
}

func TestRunScenariosPlain_PrintsAndTotals(t *testing.T) {
	srv := chainTestServer()
	defer srv.Close()

	var buf bytes.Buffer
	reqs, errs := runScenariosPlain(&buf, []scenarioJob{{scen: chainScenario(srv.URL)}})

	if errs != 0 {
		t.Errorf("errors = %d, want 0", errs)
	}
	if reqs != 12 { // 3 steps x 4 iterations
		t.Errorf("requests = %d, want 12", reqs)
	}
	out := buf.String()
	for _, want := range []string{"scenario login-call-logout", "step 1/3: POST login", "step 2/3: GET call"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}
