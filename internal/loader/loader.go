// Package loader implements the core HTTP load generation engine.
//
// A fixed-size pool of worker goroutines consumes jobs from a buffered
// channel and produces Results onto another channel. Concurrency is bounded
// by the worker count; backpressure is implicit in the buffered job channel.
package loader

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options bounds a single load run.
type Options struct {
	URL         string
	Method      string
	Concurrency int
	Requests    int
	Timeout     time.Duration
	// Headers are sent on every request. nil and empty are equivalent.
	Headers map[string]string
	// Body is the request payload sent verbatim. Empty means no body.
	// A fresh io.Reader is created per request so the same Body string
	// can be safely reused across all workers.
	Body string
}

// Result is the outcome of a single HTTP request.
type Result struct {
	Duration   time.Duration
	StatusCode int
	Err        error
}

// Run drives Options.Concurrency workers against Options.URL until
// Options.Requests have been issued, returning every Result in completion
// order. It blocks until the run is complete.
func Run(opts Options) []Result {
	jobs := make(chan struct{}, opts.Requests)
	for i := 0; i < opts.Requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	results := make(chan Result, opts.Requests)

	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: opts.Timeout}
			for range jobs {
				var body io.Reader
				if opts.Body != "" {
					body = strings.NewReader(opts.Body)
				}
				req, err := http.NewRequest(opts.Method, opts.URL, body)
				if err != nil {
					results <- Result{Err: err}
					continue
				}
				for k, v := range opts.Headers {
					req.Header.Set(k, v)
				}
				start := time.Now()
				resp, err := client.Do(req)
				dur := time.Since(start)
				if err != nil {
					results <- Result{Duration: dur, Err: err}
					continue
				}
				resp.Body.Close()
				results <- Result{Duration: dur, StatusCode: resp.StatusCode}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]Result, 0, opts.Requests)
	for r := range results {
		out = append(out, r)
	}
	return out
}
