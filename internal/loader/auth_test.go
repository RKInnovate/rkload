package loader

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestApplyAuth_Bearer(t *testing.T) {
	req := newReq(t)
	if err := applyAuth(req, Auth{Type: "bearer", Token: "abc"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer abc")
	}
}

func TestApplyAuth_BearerInterpolatesEnv(t *testing.T) {
	t.Setenv("API_TOKEN", "secret-xyz")
	req := newReq(t)
	if err := applyAuth(req, Auth{Type: "bearer", Token: "${API_TOKEN}"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret-xyz" {
		t.Errorf("Authorization = %q, want env-resolved token", got)
	}
}

func TestApplyAuth_APIKeyDefaultAndCustomHeader(t *testing.T) {
	req := newReq(t)
	_ = applyAuth(req, Auth{Type: "apikey", Token: "k1"}, nil)
	if got := req.Header.Get("Authorization"); got != "k1" {
		t.Errorf("default apikey header = %q, want raw key on Authorization", got)
	}
	req2 := newReq(t)
	_ = applyAuth(req2, Auth{Type: "apikey", Header: "X-API-Key", Token: "k2"}, nil)
	if got := req2.Header.Get("X-API-Key"); got != "k2" {
		t.Errorf("custom apikey header = %q, want k2", got)
	}
}

func TestApplyAuth_Basic(t *testing.T) {
	req := newReq(t)
	_ = applyAuth(req, Auth{Type: "basic", Username: "user", Password: "pass"}, nil)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("basic auth = %q, want %q", got, want)
	}
}

func TestApplyAuth_OAuth2NotImplemented(t *testing.T) {
	err := applyAuth(newReq(t), Auth{Type: "oauth2", ClientID: "c", TokenURL: "https://t/"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("want oauth2 not-implemented error, got %v", err)
	}
}

func TestApplyAuth_UnknownType(t *testing.T) {
	if err := applyAuth(newReq(t), Auth{Type: "hawk"}, nil); err == nil {
		t.Error("want unknown-auth-type error")
	}
}

// TestRunScenario_AppliesScenarioAuth proves scenario-level auth reaches
// the server (the handler 401s without the expected bearer token).
func TestRunScenario_AppliesScenarioAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOK" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	results := RunScenario(ScenarioOptions{
		VUs: 1, Iterations: 2, Timeout: 5 * time.Second,
		Auth:  &Auth{Type: "bearer", Token: "TOK"},
		Steps: []Step{{Method: "GET", URL: srv.URL, Assert: []AssertRule{{Type: "status", Equals: 200}}}},
	})
	for _, r := range results {
		if r.Err != nil || r.AssertErr != nil {
			t.Errorf("scenario auth not applied: %+v", r)
		}
	}
}

// TestRunScenario_StepAuthOverridesScenario proves a step's own auth wins
// over the scenario-level auth.
func TestRunScenario_StepAuthOverridesScenario(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer STEP" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	results := RunScenario(ScenarioOptions{
		VUs: 1, Iterations: 1, Timeout: 5 * time.Second,
		Auth: &Auth{Type: "bearer", Token: "SCENARIO"},
		Steps: []Step{{
			Method: "GET", URL: srv.URL,
			Auth:   &Auth{Type: "bearer", Token: "STEP"},
			Assert: []AssertRule{{Type: "status", Equals: 200}},
		}},
	})
	if results[0].AssertErr != nil {
		t.Errorf("step auth should override scenario auth, got %+v", results[0])
	}
}
