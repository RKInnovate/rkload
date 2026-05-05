// Package main is the entry point for the rkload CLI.
//
// rkload is a concurrent HTTP load testing tool. See README.md for usage.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

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

	if *help {
		flag.Usage()
		return
	}

	if *url == "" {
		fmt.Fprintln(os.Stderr, "Error: -url is required")
		fmt.Fprintln(os.Stderr, "Example: rkload -url https://api.example.com/health -c 50 -n 1000")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Load testing: %s\n", *url)
	fmt.Printf("Workers: %d | Requests: %d | Method: %s\n\n", *concurrency, *requests, *method)

	opts := loader.Options{
		URL:         *url,
		Method:      *method,
		Concurrency: *concurrency,
		Requests:    *requests,
		Timeout:     30 * time.Second,
	}

	start := time.Now()
	results := loader.Run(opts)
	elapsed := time.Since(start)

	summary := report.Summarize(results, elapsed)
	report.Print(os.Stdout, summary)

	// Non-zero exit if any request failed — load-bearing for CI usage.
	if summary.Errors > 0 {
		os.Exit(1)
	}
}
