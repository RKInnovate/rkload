package loader

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_AllSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results := Run(Options{
		URL:         srv.URL,
		Method:      "GET",
		Concurrency: 5,
		Requests:    20,
		Timeout:     5 * time.Second,
	})

	if len(results) != 20 {
		t.Fatalf("got %d results, want 20", len(results))
	}
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result %d: unexpected error: %v", i, r.Err)
		}
		if r.StatusCode != http.StatusOK {
			t.Errorf("result %d: status = %d, want 200", i, r.StatusCode)
		}
		if r.Duration <= 0 {
			t.Errorf("result %d: duration = %v, want > 0", i, r.Duration)
		}
	}
}

// TestRun_BoundsConcurrency verifies the worker pool never exceeds
// Options.Concurrency in-flight requests. Without this guarantee the
// "C concurrent workers" contract is meaningless.
func TestRun_BoundsConcurrency(t *testing.T) {
	const c = 4
	var inflight, maxInflight int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := atomic.AddInt64(&inflight, 1)
		defer atomic.AddInt64(&inflight, -1)
		for {
			m := atomic.LoadInt64(&maxInflight)
			if cur <= m || atomic.CompareAndSwapInt64(&maxInflight, m, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	Run(Options{
		URL:         srv.URL,
		Method:      "GET",
		Concurrency: c,
		Requests:    20,
		Timeout:     5 * time.Second,
	})

	if got := atomic.LoadInt64(&maxInflight); got > c {
		t.Errorf("max in-flight = %d, want <= %d", got, c)
	}
}

func TestRun_ConnectionErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // immediately close so the URL is unreachable

	results := Run(Options{
		URL:         srv.URL,
		Method:      "GET",
		Concurrency: 2,
		Requests:    5,
		Timeout:     500 * time.Millisecond,
	})

	if len(results) != 5 {
		t.Fatalf("got %d results, want 5", len(results))
	}
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("result %d: expected error, got nil", i)
		}
	}
}

// TestRun_SendsHeadersAndBody verifies the loader actually applies
// Options.Headers and Options.Body to outgoing requests — the contract
// the v0.3.0 config loader will rely on.
func TestRun_SendsHeadersAndBody(t *testing.T) {
	const wantBody = `{"hello":"world"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test"); got != "rk" {
			t.Errorf("X-Test header = %q, want %q", got, "rk")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", body, wantBody)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results := Run(Options{
		URL:         srv.URL,
		Method:      "POST",
		Concurrency: 2,
		Requests:    4,
		Timeout:     5 * time.Second,
		Headers: map[string]string{
			"X-Test":       "rk",
			"Content-Type": "application/json",
		},
		Body: wantBody,
	})

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result %d: %v", i, r.Err)
		}
	}
}

// Reusing the same Body string across many workers must not cause
// "stream consumed" failures — each request gets its own io.Reader.
func TestRun_BodyReusedAcrossWorkers(t *testing.T) {
	var bodyCount, mu = 0, sync.Mutex{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		if len(body) > 0 {
			bodyCount++
		}
		mu.Unlock()
	}))
	defer srv.Close()

	const n = 30
	Run(Options{
		URL: srv.URL, Method: "POST",
		Concurrency: 5, Requests: n, Timeout: 5 * time.Second,
		Body: "payload",
	})

	if bodyCount != n {
		t.Errorf("server saw body on %d/%d requests", bodyCount, n)
	}
}

// TestRun_NewRequestError covers the http.NewRequest failure path
// (e.g. invalid method) which is recorded with a zero duration and
// no status code.
func TestRun_NewRequestError(t *testing.T) {
	results := Run(Options{
		URL:         "http://localhost",
		Method:      "INVALID METHOD WITH SPACE",
		Concurrency: 1,
		Requests:    1,
		Timeout:     time.Second,
	})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Error("expected NewRequest error for invalid method, got nil")
	}
	if results[0].StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0 for pre-flight error", results[0].StatusCode)
	}
}
