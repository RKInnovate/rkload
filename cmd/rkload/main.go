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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RKInnovate/rkload/internal/cache"
	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/importer"
	"github.com/RKInnovate/rkload/internal/loader"
	"github.com/RKInnovate/rkload/internal/report"
	"github.com/RKInnovate/rkload/internal/updater"
)

// Build-time variables, populated by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Subcommand dispatch comes first so `rkload import openapi spec.yaml`
	// (and friends) don't get parsed by the top-level flag set, which
	// would reject the unknown positional argument.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "import":
			os.Exit(importMain(os.Args[2:]))
		case "validate":
			os.Exit(validateMain(os.Args[2:]))
		case "init":
			os.Exit(initMain(os.Args[2:]))
		case "update":
			os.Exit(updateMain(os.Args[2:]))
		}
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

	// Daily update notice — silent on errors, skipped for dev builds,
	// non-tty stdout, or RKLOAD_NO_UPDATE_CHECK=1.
	maybePrintUpdateNotice(os.Stderr, isTerminal(os.Stdout))

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
	fmt.Fprintln(os.Stderr, "  rkload init [FILE] [--force]                  write a starter config (stdout if FILE omitted)")
	fmt.Fprintln(os.Stderr, "  rkload validate <FILE> [--no-cache]           validate a config and record metadata")
	fmt.Fprintln(os.Stderr, "  rkload import {openapi|postman} <FILE> [...]  generate a config from a spec")
	fmt.Fprintln(os.Stderr, "  rkload update [--check|--version V|--force]   update the binary in place")
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
	cfg, status, err := loadAndValidateForRun(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Printf("Loaded config: %s (schema v%d)\n", path, cfg.Version)
	fmt.Printf("Validation:    %s\n\n", status)

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
	serverURL := fs.String("server-url", "", "Override the spec's base URL (servers[] / host+basePath) — wins over --server-index")
	serverIndex := fs.Int("server-index", 0, "Pick servers[N] from an OpenAPI 3 spec (default 0; ignored for Swagger 2)")
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
		ServerURL:          *serverURL,
		ServerIndex:        *serverIndex,
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

// loadAndValidateForRun is the cache-aware load path for `rkload
// -config`. It reads the file bytes, looks up the canonical-hash
// cache entry, and either:
//
//   - skips Validate and returns the parsed config with a "cached …"
//     status if the entry matches the current rkload version, or
//   - runs full Validate, writes a fresh cache entry, and returns a
//     "re-checked …" status on cache miss / version mismatch / any
//     hash-or-lookup hiccup.
//
// A cache write failure is reported in the status line, not as an
// error — the validation succeeded, only the bookkeeping didn't.
func loadAndValidateForRun(path string) (*config.Config, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("config: opening %s: %w", path, err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return nil, "", fmt.Errorf("config: parsing %s: %w", path, err)
	}

	hash, _ := cache.CanonicalHash(data) // Parse succeeded; the same bytes hash cleanly.
	if hash != "" {
		if entry, lookupErr := cache.Lookup(hash); lookupErr == nil && entry != nil && entry.RkloadVersion == version {
			cfg.ApplyDefaults()
			status := fmt.Sprintf("cached %s (rkload %s)",
				entry.ValidatedAt.UTC().Format("2006-01-02 15:04 UTC"),
				entry.RkloadVersion)
			return cfg, status, nil
		}
	}

	// Cache miss, version mismatch, or lookup error — do full validation.
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	cfg.ApplyDefaults()

	if hash == "" {
		// Defensive: no hash, nothing to cache.
		return cfg, "re-checked (not cached)", nil
	}
	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		absPath = path
	}
	var size int64
	if fi, statErr := os.Stat(path); statErr == nil {
		size = fi.Size()
	}
	entry := &cache.Entry{
		Hash:           hash,
		ValidatedAt:    time.Now().UTC(),
		RkloadVersion:  version,
		ConfigPath:     absPath,
		FileSizeBytes:  size,
		SchemaURL:      cfg.Schema,
		SchemaVersion:  cfg.Version,
		EndpointCounts: endpointCounts(cfg),
		Status:         cache.StatusValid,
	}
	if storeErr := cache.Store(entry); storeErr != nil {
		return cfg, fmt.Sprintf("re-checked (cache write failed: %v)", storeErr), nil
	}
	return cfg, "re-checked and cached", nil
}

// maybePrintUpdateNotice runs the once-per-day update check and
// prints a one-line notice to out when a newer release exists.
// Failures are silent — a network blip should never disrupt a
// load test or stand between the user and their command. Skipped
// for:
//
//   - "dev" / "unknown" / *-snapshot builds (local dev — noisy)
//   - RKLOAD_NO_UPDATE_CHECK=1 (explicit opt-out)
//   - non-tty stdout (piped, redirected, CI — the notice would
//     pollute machine-readable output)
//
// The tty bool is injected so tests can exercise the print path
// without faking a terminal.
func maybePrintUpdateNotice(out io.Writer, tty bool) {
	if !tty {
		return
	}
	if version == "dev" || version == "unknown" {
		return
	}
	if os.Getenv("RKLOAD_NO_UPDATE_CHECK") == "1" {
		return
	}

	state, err := updater.LoadState()
	if err != nil || state == nil {
		state = &updater.State{}
	}

	latest := state.LatestVersionSeen
	if updater.ShouldCheck(state, time.Now(), 24*time.Hour) {
		rel, err := updater.Latest(nil)
		if err != nil {
			return // silent
		}
		latest = rel.Tag
		state.LastCheckedAt = time.Now()
		state.LatestVersionSeen = latest
		_ = updater.SaveState(state)
	}
	if latest == "" {
		return
	}
	newer, err := updater.Newer(version, latest)
	if err != nil || !newer {
		return
	}
	fmt.Fprintf(out, "[update available] rkload %s — run `rkload update` to upgrade\n\n", latest)
}

// isTerminal reports whether f refers to a character device (a tty).
// Cheap stdlib-only check — no /x/term dependency.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// updateMain handles `rkload update`. The thin wrapper resolves
// host context (executable path, GOOS, GOARCH) and delegates to
// runUpdate so the core path is testable.
func updateMain(args []string) int {
	fs := flag.NewFlagSet("rkload update", flag.ContinueOnError)
	check := fs.Bool("check", false, "Report whether an update is available; do not install")
	pinned := fs.String("version", "", "Install a specific version (e.g. v0.3.4); allows downgrade")
	force := fs.Bool("force", false, "Install even if the current version is at or above the target")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rkload update [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Checks GitHub Releases for a newer version, downloads the matching")
		fmt.Fprintln(os.Stderr, "archive, verifies its SHA-256 against checksums.txt, and atomically")
		fmt.Fprintln(os.Stderr, "replaces the running binary. Set RKLOAD_NO_UPDATE_CHECK=1 to opt out")
		fmt.Fprintln(os.Stderr, "of the daily background check that runs at startup.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: locating executable:", err)
		return 1
	}
	return runUpdate(os.Stdout, os.Stderr, exePath, runtime.GOOS, runtime.GOARCH, *check, *pinned, *force)
}

// runUpdate is the testable core of `rkload update`. exePath /
// goos / goarch are injected so tests can run against a fake
// executable file and a controlled archive name without touching
// the running process.
func runUpdate(out, errOut io.Writer, exePath, goos, goarch string, check bool, pinned string, force bool) int {
	var target updater.Release
	if pinned != "" {
		target = updater.Release{Tag: pinned}
	} else {
		rel, err := updater.Latest(nil)
		if err != nil {
			fmt.Fprintln(errOut, "Error:", err)
			return 1
		}
		target = rel
	}

	newer := true
	if pinned == "" {
		ok, err := updater.Newer(version, target.Tag)
		if err != nil {
			fmt.Fprintln(errOut, "Error:", err)
			return 1
		}
		newer = ok
	}

	if !newer && !force {
		fmt.Fprintf(out, "rkload %s is already up to date.\n", version)
		return 0
	}

	if check {
		fmt.Fprintf(out, "rkload %s available (current: %s).\nRun `rkload update` to install.\n", target.Tag, version)
		return 0
	}

	archiveName := updater.ArchiveName(target.Tag, goos, goarch)
	fmt.Fprintf(out, "Downloading %s...\n", archiveName)
	archivePath, err := updater.DownloadAndVerify(nil, target, archiveName)
	if err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	defer os.Remove(archivePath)

	fmt.Fprintf(out, "Installing to %s...\n", exePath)
	if err := updater.ReplaceSelf(exePath, archivePath); err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	fmt.Fprintf(out, "Updated rkload to %s.\n", target.Tag)
	return 0
}

// starterConfigTemplate is what `rkload init` emits. The endpoints
// are deliberately representative rather than minimal — one GET with
// defaults left implicit, one POST showing headers/body/timeout/c/n
// — so a user editing the file sees every common knob without having
// to consult the schema. REPLACE_ME placeholders mirror the import
// subcommand convention: greppable, never silently shipped to prod.
const starterConfigTemplate = `{
  "$schema": "https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v1/config.schema.json",
  "version": 1,

  "GET": [
    {
      "name": "health",
      "url": "https://api.example.com/health",
      "c": 10,
      "requests": 100
    }
  ],

  "POST": [
    {
      "name": "login",
      "url": "https://api.example.com/auth/login",
      "headers": {
        "Content-Type": "application/json",
        "Authorization": "Bearer REPLACE_ME"
      },
      "body": "{\"email\":\"user@example.com\",\"password\":\"REPLACE_ME\"}",
      "c": 5,
      "requests": 50,
      "timeout": "10s"
    }
  ]
}
`

// initMain handles `rkload init [path]`. Accepts an optional positional
// output path; without one, the template goes to stdout so the user
// can pipe or redirect.
func initMain(args []string) int {
	fs := flag.NewFlagSet("rkload init", flag.ContinueOnError)
	force := fs.Bool("force", false, "Overwrite the target file if it already exists")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rkload init [path] [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Writes a starter rkload config — one GET and one POST with")
		fmt.Fprintln(os.Stderr, "headers, body, and explicit defaults — so you can begin from a")
		fmt.Fprintln(os.Stderr, "working file instead of the schema. With no path argument the")
		fmt.Fprintln(os.Stderr, "config is printed to stdout. Existing files are not overwritten")
		fmt.Fprintln(os.Stderr, "unless --force is given.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 1
	}
	path := ""
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	return runInit(os.Stdout, os.Stderr, path, *force)
}

// runInit is the testable core of `rkload init`. Empty path means
// "write to out"; a non-empty path triggers the refuse-overwrite
// check (skipped when force is true).
func runInit(out, errOut io.Writer, path string, force bool) int {
	if path == "" {
		if _, err := io.WriteString(out, starterConfigTemplate); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(errOut, "Error: %s already exists (pass --force to overwrite)\n", path)
			return 1
		} else if !os.IsNotExist(err) {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}

	if err := os.WriteFile(path, []byte(starterConfigTemplate), 0o644); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	fmt.Fprintf(out, "Wrote starter config to %s\n", path)
	return 0
}

// validateMain handles `rkload validate <config>`. It parses the
// subcommand flags, then delegates to runValidate so the core logic
// stays testable without going through os.Args / os.Exit.
func validateMain(args []string) int {
	fs := flag.NewFlagSet("rkload validate", flag.ContinueOnError)
	noCache := fs.Bool("no-cache", false, "Skip reading and writing the validation cache")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rkload validate <config> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Validates a JSON config against the schema, prints a summary of")
		fmt.Fprintln(os.Stderr, "the file (hash, size, per-method endpoint counts), and records the")
		fmt.Fprintln(os.Stderr, "result in the validation cache (~/.rkload/cache/ by default; set")
		fmt.Fprintln(os.Stderr, "RKLOAD_CACHE_DIR to override). On a subsequent `rkload -config`")
		fmt.Fprintln(os.Stderr, "run, a cached hash match skips re-validation.")
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
	return runValidate(os.Stdout, os.Stderr, fs.Arg(0), *noCache)
}

// runValidate is the testable core of `rkload validate`. Returns 0 on
// a clean validation, 1 on any failure (file missing, parse error,
// validation error). A cache write failure does not flip the exit
// code — the validation succeeded; the record-keeping didn't.
func runValidate(out, errOut io.Writer, path string, noCache bool) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	cfg, err := config.Parse(data)
	if err != nil {
		fmt.Fprintf(errOut, "config: parsing %s: %v\n", path, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	cfg.ApplyDefaults()

	hash, err := cache.CanonicalHash(data)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	var size int64
	if fi, statErr := os.Stat(path); statErr == nil {
		size = fi.Size()
	}
	counts := endpointCounts(cfg)
	entry := &cache.Entry{
		Hash:           hash,
		ValidatedAt:    time.Now().UTC(),
		RkloadVersion:  version,
		ConfigPath:     absPath,
		FileSizeBytes:  size,
		SchemaURL:      cfg.Schema,
		SchemaVersion:  cfg.Version,
		EndpointCounts: counts,
		Status:         cache.StatusValid,
	}

	var cacheLine string
	switch {
	case noCache:
		cacheLine = "no (-no-cache)"
	default:
		if storeErr := cache.Store(entry); storeErr != nil {
			cacheLine = fmt.Sprintf("no (write failed: %v)", storeErr)
		} else {
			dir, _ := cache.Dir()
			cacheLine = fmt.Sprintf("yes (%s)", filepath.Join(dir, hash+".json"))
		}
	}

	printValidateSummary(out, absPath, hash, size, cfg, counts, cacheLine)
	return 0
}

// endpointCounts returns a map of method → endpoint count with a
// synthesised "total" key, so the cache entry can render summaries
// without re-walking the config.
func endpointCounts(cfg *config.Config) map[string]int {
	counts := map[string]int{}
	total := 0
	for _, g := range cfg.Groups() {
		if n := len(g.Endpoints); n > 0 {
			counts[g.Method] = n
			total += n
		}
	}
	counts["total"] = total
	return counts
}

// methodOrder is the stable display order for endpoint counts and
// matches config.Config.Groups() so output is identical to how the
// run flow walks endpoints.
var methodOrder = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

func formatCounts(counts map[string]int) string {
	var parts []string
	for _, m := range methodOrder {
		if n := counts[m]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", m, n))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func printValidateSummary(w io.Writer, path, hash string, size int64, cfg *config.Config, counts map[string]int, cacheLine string) {
	fmt.Fprintf(w, "Validated: %s\n", path)
	fmt.Fprintf(w, "  Status:    %s\n", cache.StatusValid)
	fmt.Fprintf(w, "  Hash:      %s\n", hash)
	fmt.Fprintf(w, "  Size:      %d bytes\n", size)
	if cfg.Schema != "" {
		fmt.Fprintf(w, "  Schema:    %s\n", cfg.Schema)
	}
	fmt.Fprintf(w, "  Version:   %d\n", cfg.Version)
	fmt.Fprintf(w, "  Endpoints: %s (total: %d)\n", formatCounts(counts), counts["total"])
	fmt.Fprintf(w, "  Cached:    %s\n", cacheLine)
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
