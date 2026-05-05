// Package main is the entry point for the rkload CLI.
//
// rkload is a concurrent HTTP load testing tool. See README.md for usage.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/loader"
	"github.com/RKInnovate/rkload/internal/report"
)

// Build-time variables, populated by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	url := flag.String("url", "", "Target URL to load test (single-endpoint mode)")
	configPath := flag.String("config", "", "Path to a JSON config file (multi-endpoint mode; mutually exclusive with -url)")
	concurrency := flag.Int("c", 10, "Number of concurrent workers (single-endpoint mode)")
	requests := flag.Int("n", 100, "Total number of requests (single-endpoint mode)")
	method := flag.String("method", "GET", "HTTP method (single-endpoint mode)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rkload %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if *help {
		flag.Usage()
		return
	}

	if *configPath != "" && *url != "" {
		fmt.Fprintln(os.Stderr, "Error: -config and -url are mutually exclusive")
		os.Exit(1)
	}

	if *configPath != "" {
		os.Exit(runFromConfig(*configPath))
	}

	if *url == "" {
		fmt.Fprintln(os.Stderr, "Error: one of -url or -config is required")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  rkload -url https://api.example.com/health -c 50 -n 1000")
		fmt.Fprintln(os.Stderr, "  rkload -config rkload.config.json")
		flag.Usage()
		os.Exit(1)
	}

	os.Exit(runSingle(loader.Options{
		URL:         *url,
		Method:      *method,
		Concurrency: *concurrency,
		Requests:    *requests,
		Timeout:     30 * time.Second,
	}))
}

// runSingle executes the legacy single-endpoint flow driven by -url.
func runSingle(opts loader.Options) int {
	fmt.Printf("Load testing: %s\n", opts.URL)
	fmt.Printf("Workers: %d | Requests: %d | Method: %s\n\n", opts.Concurrency, opts.Requests, opts.Method)

	start := time.Now()
	results := loader.Run(opts)
	elapsed := time.Since(start)

	summary := report.Summarize(results, elapsed)
	report.Print(os.Stdout, summary)

	if summary.Errors > 0 {
		return 1
	}
	return 0
}

// runFromConfig loads a JSON config and runs every (method, endpoint)
// pair sequentially so per-endpoint reports stay clean. Returns
// non-zero if any endpoint had any failed requests, or if the config
// itself is invalid — load-bearing for CI usage.
func runFromConfig(path string) int {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Printf("Loaded config: %s (schema v%d)\n\n", path, cfg.Version)

	var totalRequests, totalErrors, endpointCount int
	for _, group := range cfg.Groups() {
		for _, ep := range group.Endpoints {
			label := ep.Name
			if label == "" {
				label = ep.URL
			}
			fmt.Printf("=== %s %s ===\n", group.Method, label)
			fmt.Printf("URL:     %s\n", ep.URL)
			fmt.Printf("Workers: %d | Requests: %d | Timeout: %s\n\n",
				ep.Concurrency, ep.Requests, ep.Timeout)

			timeout, _ := ep.ParsedTimeout() // Validate already proved this parses
			opts := loader.Options{
				URL:         ep.URL,
				Method:      group.Method,
				Concurrency: ep.Concurrency,
				Requests:    ep.Requests,
				Timeout:     timeout,
				Headers:     ep.Headers,
				Body:        ep.Body,
			}

			start := time.Now()
			results := loader.Run(opts)
			elapsed := time.Since(start)

			summary := report.Summarize(results, elapsed)
			report.Print(os.Stdout, summary)
			fmt.Println()

			totalRequests += summary.Total
			totalErrors += summary.Errors
			endpointCount++
		}
	}

	fmt.Printf("=== Overall ===\n")
	fmt.Printf("Endpoints tested: %d\n", endpointCount)
	fmt.Printf("Total requests:   %d\n", totalRequests)
	fmt.Printf("Total errors:     %d\n", totalErrors)

	if totalErrors > 0 {
		return 1
	}
	return 0
}
