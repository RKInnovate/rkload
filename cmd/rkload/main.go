// Package main is the entry point for the rkload CLI.
//
// rkload is a concurrent HTTP load testing tool. See README.md for usage.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Build-time variables, populated by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// result holds the outcome of a single HTTP request.
type result struct {
	duration   time.Duration
	statusCode int
	err        error
}

func main() {
	url := flag.String("url", "", "Target URL to load test")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	requests := flag.Int("n", 100, "Total number of requests")
	method := flag.String("method", "GET", "HTTP method (GET, POST, etc.)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rkload %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if *help || *url == "" {
		fmt.Fprintln(os.Stderr, "Example: rkload -url https://api.example.com/health -c 50 -n 1000")
		flag.Usage()
		return
	}

	fmt.Printf("Load testing: %s\n", *url)
	fmt.Printf("Workers: %d | Requests: %d | Method: %s\n\n", *concurrency, *requests, *method)

	// Buffered channel as the work queue. Each token represents one request.
	jobs := make(chan struct{}, *requests)
	for i := 0; i < *requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	results := make(chan result, *requests)

	var wg sync.WaitGroup
	start := time.Now()

	// Spawn the worker pool.
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 30 * time.Second}

			for range jobs {
				req, err := http.NewRequest(*method, *url, nil)
				if err != nil {
					results <- result{err: err}
					continue
				}

				reqStart := time.Now()
				resp, err := client.Do(req)
				duration := time.Since(reqStart)

				if err != nil {
					results <- result{duration: duration, err: err}
					continue
				}
				resp.Body.Close()
				results <- result{duration: duration, statusCode: resp.StatusCode}
			}
		}()
	}

	// Close results channel once all workers have finished.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Aggregate.
	var (
		totalDuration time.Duration
		errors        int
		durations     = make([]time.Duration, 0, *requests)
		statusCodes   = make(map[int]int)
	)

	for r := range results {
		if r.err != nil {
			errors++
			continue
		}
		totalDuration += r.duration
		durations = append(durations, r.duration)
		statusCodes[r.statusCode]++
	}

	elapsed := time.Since(start)
	successful := len(durations)
	total := successful + errors

	fmt.Printf("--- Results ---\n")
	fmt.Printf("Total requests:  %d\n", total)
	fmt.Printf("Successful:      %d\n", successful)
	fmt.Printf("Errors:          %d\n", errors)
	fmt.Printf("Total time:      %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Throughput:      %.2f req/sec\n", float64(total)/elapsed.Seconds())

	if successful > 0 {
		avg := totalDuration / time.Duration(successful)
		fmt.Printf("Avg latency:     %s\n", avg.Round(time.Millisecond))

		fmt.Printf("\nStatus codes:\n")
		for code, count := range statusCodes {
			barLen := count * 20 / successful
			bar := ""
			for i := 0; i < barLen; i++ {
				bar += "█"
			}
			fmt.Printf("  HTTP %d: %d %s\n", code, count, bar)
		}
	}

	// Non-zero exit if any request failed — useful in CI.
	if errors > 0 {
		os.Exit(1)
	}
}
