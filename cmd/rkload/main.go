// Package main is the entry point for the rkload CLI.
//
// rkload is a concurrent HTTP load testing tool. See README.md for usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/importer"
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
	// Subcommand dispatch comes first so `rkload import openapi spec.yaml`
	// doesn't get parsed by the top-level flag set (which would reject
	// the unknown positional argument).
	if len(os.Args) > 1 && os.Args[1] == "import" {
		os.Exit(importMain(os.Args[2:]))
	}

	os.Exit(runMain())
}

// runMain hosts the legacy flag-driven entry point: -url, -config,
// -version, -help, etc. Returning an int (instead of calling os.Exit
// directly) keeps the function testable.
func runMain() int {
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
		return 0
	}

	if *help {
		printRootUsage()
		return 0
	}

	if *configPath != "" && *url != "" {
		fmt.Fprintln(os.Stderr, "Error: -config and -url are mutually exclusive")
		return 1
	}

	if *configPath != "" {
		return runFromConfig(*configPath)
	}

	if *url == "" {
		fmt.Fprintln(os.Stderr, "Error: one of -url or -config is required")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  rkload -url https://api.example.com/health -c 50 -n 1000")
		fmt.Fprintln(os.Stderr, "  rkload -config rkload.config.json")
		fmt.Fprintln(os.Stderr, "  rkload import openapi spec.yaml -o rkload.config.json")
		flag.Usage()
		return 1
	}

	return runSingle(loader.Options{
		URL:         *url,
		Method:      *method,
		Concurrency: *concurrency,
		Requests:    *requests,
		Timeout:     30 * time.Second,
	})
}

func printRootUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  rkload -url <URL> [-c N] [-n N] [-method M]   single-endpoint mode")
	fmt.Fprintln(os.Stderr, "  rkload -config <FILE>                         multi-endpoint mode (JSON config)")
	fmt.Fprintln(os.Stderr, "  rkload import {openapi|postman} <FILE> [...]  generate a config from a spec")
	fmt.Fprintln(os.Stderr, "  rkload -version                               print version and exit")
	fmt.Fprintln(os.Stderr, "")
	flag.Usage()
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

// importMain dispatches to per-format handlers under `rkload import`.
// Format handlers each own a flag.NewFlagSet so their flags don't
// pollute the root command's flag space.
func importMain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: rkload import {openapi|postman} <file> [flags]")
		return 1
	}

	switch args[0] {
	case "openapi":
		return importOpenAPI(args[1:])
	case "postman":
		return importPostman(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, "Usage: rkload import {openapi|postman} <file> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  openapi   Generate config from OpenAPI 3.x or Swagger 2.0 (JSON or YAML)")
		fmt.Fprintln(os.Stderr, "  postman   Generate config from a Postman Collection v2.1")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown import format %q. Supported: openapi, postman\n", args[0])
		return 1
	}
}

// importOpenAPI parses an OpenAPI 3.x or Swagger 2.0 spec and writes
// the equivalent rkload Config. Output goes to -o or stdout.
func importOpenAPI(args []string) int {
	fs := flag.NewFlagSet("rkload import openapi", flag.ContinueOnError)
	output := fs.String("o", "", "Output file (default: stdout)")
	concurrency := fs.Int("c", 0, "Default concurrency for generated endpoints (0 = config default)")
	requests := fs.Int("n", 0, "Default request count for generated endpoints (0 = config default)")
	timeout := fs.String("timeout", "", "Default timeout for generated endpoints (empty = config default)")
	tagFilter := fs.String("tag", "", "Include only operations whose tags contain this value")
	pathPrefix := fs.String("path-prefix", "", "Include only paths starting with this prefix (e.g. /api/v1/)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rkload import openapi <spec> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 1
	}
	specPath := fs.Arg(0)

	in, err := os.Open(specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer in.Close()

	cfg, err := importer.OpenAPI(in, importer.OpenAPIOptions{
		DefaultConcurrency: *concurrency,
		DefaultRequests:    *requests,
		DefaultTimeout:     *timeout,
		TagFilter:          *tagFilter,
		PathPrefix:         *pathPrefix,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return writeConfigJSON(cfg, *output)
}

// writeConfigJSON encodes cfg as pretty-printed JSON to outPath (empty
// = stdout). Stdout writes don't get a trailing close, but file writes
// flush on close — so a fatal mid-encode error doesn't leave a half-
// written file behind under normal usage.
func writeConfigJSON(cfg *config.Config, outPath string) int {
	var w io.Writer = os.Stdout
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// importPostman parses a Postman Collection v2.1 and writes the
// equivalent rkload Config. Folder structure is flattened.
func importPostman(args []string) int {
	fs := flag.NewFlagSet("rkload import postman", flag.ContinueOnError)
	output := fs.String("o", "", "Output file (default: stdout)")
	concurrency := fs.Int("c", 0, "Default concurrency for generated endpoints (0 = config default)")
	requests := fs.Int("n", 0, "Default request count for generated endpoints (0 = config default)")
	timeout := fs.String("timeout", "", "Default timeout for generated endpoints (empty = config default)")
	pathPrefix := fs.String("path-prefix", "", "Include only endpoints whose URL contains this substring")
	varsRaw := newRepeatableFlag(fs, "var", "Override a Postman {{var}} (repeatable, e.g. --var baseUrl=https://prod.x)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rkload import postman <collection> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 1
	}
	colPath := fs.Arg(0)

	in, err := os.Open(colPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer in.Close()

	vars, err := parseVarFlags(*varsRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg, err := importer.Postman(in, importer.PostmanOptions{
		DefaultConcurrency: *concurrency,
		DefaultRequests:    *requests,
		DefaultTimeout:     *timeout,
		PathPrefix:         *pathPrefix,
		Vars:               vars,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return writeConfigJSON(cfg, *output)
}

// repeatableFlag is a tiny implementation of a -var key=value flag
// that may appear multiple times. flag.StringSlice doesn't exist in
// the stdlib, so we plug a custom flag.Value that appends.
type repeatableFlag struct{ values *[]string }

func (r *repeatableFlag) String() string {
	if r.values == nil {
		return ""
	}
	return strings.Join(*r.values, ",")
}

func (r *repeatableFlag) Set(s string) error {
	*r.values = append(*r.values, s)
	return nil
}

func newRepeatableFlag(fs *flag.FlagSet, name, usage string) *[]string {
	values := &[]string{}
	fs.Var(&repeatableFlag{values: values}, name, usage)
	return values
}

// parseVarFlags turns ["k1=v1", "k2=v2"] into map[string]string.
// Errors clearly on malformed entries instead of silently dropping
// them.
func parseVarFlags(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("--var %q: expected key=value form", kv)
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out, nil
}
