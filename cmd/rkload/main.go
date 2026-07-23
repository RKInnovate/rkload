// Package main is the entry point for the rkload CLI.
//
// rkload is a concurrent HTTP load testing tool. See README.md for usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	lgtable "github.com/charmbracelet/lipgloss/table"

	"github.com/RKInnovate/rkload/internal/cache"
	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/importer"
	"github.com/RKInnovate/rkload/internal/loader"
	"github.com/RKInnovate/rkload/internal/report"
	"github.com/RKInnovate/rkload/internal/tui"
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

// ConfigSuffix is the filename suffix rkload looks for when -config
// points at a directory. Picking a compound suffix instead of plain
// .json prevents directory-mode runs from accidentally trying to
// load unrelated JSON files (settings, data fixtures, etc.) that
// happen to share the directory.
const ConfigSuffix = ".rkload.json"

// configBundle pairs a parsed Config with the file it came from and
// the validation cache status string we want to show in the final
// report. The TUI / plain renderers consume []configBundle so both
// single-file and directory modes share the same downstream code.
type configBundle struct {
	cfg    *config.Config
	path   string
	status string
}

// endpointJob is the work item the runner goroutine consumes: the
// resolved method + endpoint plus the flat index that matches the
// TUI's endpoint slice. Origin records which config the endpoint
// came from so the final report can group endpoints by source file
// when a directory bundles several configs.
type endpointJob struct {
	method string
	ep     config.Endpoint
	idx    int
	origin string // path of the config file this endpoint came from
}

// runFromConfig loads a JSON config (single file) or every
// *.rkload.json in a directory (recursive iteration is intentionally
// NOT done — keep this predictable), and runs every (method,
// endpoint) pair sequentially. Returns non-zero if any endpoint
// had any failed requests, or if any config was invalid —
// load-bearing for CI usage.
//
// When stdout is a TTY, a live TUI dashboard replaces the per-
// endpoint progress output during the run; the plain-text aggregate
// report still prints afterwards so `rkload ... | tee log.txt` and
// similar workflows capture meaningful, grep-able text.
func runFromConfig(path string) int {
	bundles, err := resolveConfigs(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	jobs := flattenJobs(bundles)
	scenarios := flattenScenarios(bundles, len(jobs))
	if len(jobs) == 0 && len(scenarios) == 0 {
		fmt.Fprintln(os.Stderr, "config: no endpoints or scenarios in the loaded config(s)")
		return 1
	}

	// The live TUI dashboard doesn't render scenario steps yet, so any
	// config that declares scenarios uses the plain path for now.
	if isTerminal(os.Stdout) && len(scenarios) == 0 {
		return runFromConfigTUI(bundles, jobs)
	}
	return runFromConfigPlain(bundles, jobs, scenarios)
}

// resolveConfigs handles both forms of the -config argument. A
// regular file path loads that one file (any extension accepted);
// a directory path scans for *.rkload.json entries (lexically
// sorted, non-recursive). Each file is loaded + validated
// independently — the validation cache fires per-file.
func resolveConfigs(path string) ([]configBundle, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if !info.IsDir() {
		cfg, status, err := loadAndValidateForRun(path)
		if err != nil {
			return nil, err
		}
		return []configBundle{{cfg: cfg, path: path, status: status}}, nil
	}

	files, err := findRkloadConfigs(path)
	if err != nil {
		return nil, fmt.Errorf("config: scanning %s: %w", path, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("config: no *.rkload.json files found in %s "+
			"(directory-mode looks for the compound suffix to avoid loading "+
			"unrelated JSON files)", path)
	}
	bundles := make([]configBundle, 0, len(files))
	for _, f := range files {
		cfg, status, err := loadAndValidateForRun(f)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, configBundle{cfg: cfg, path: f, status: status})
	}
	return bundles, nil
}

// findRkloadConfigs returns the *.rkload.json file paths in dir,
// sorted lexically. Non-recursive — nested directories are ignored
// so users can keep helper files / staging configs under
// subdirectories without affecting a top-level run.
func findRkloadConfigs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ConfigSuffix) {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// flattenJobs walks every bundle's config in stable order and
// produces a flat slice of (method, endpoint, index, origin)
// tuples. The index matches the TUI's endpoint slice so result
// messages route to the right row.
func flattenJobs(bundles []configBundle) []endpointJob {
	var jobs []endpointJob
	idx := 0
	for _, b := range bundles {
		for _, group := range b.cfg.Groups() {
			for _, ep := range group.Endpoints {
				jobs = append(jobs, endpointJob{
					method: group.Method,
					ep:     ep,
					idx:    idx,
					origin: b.path,
				})
				idx++
			}
		}
	}
	return jobs
}

// printBundlesHeader prints the per-file Loaded/Validation header.
// One line each for single-file runs, multiple lines for directory
// runs. Used at the top of plain mode and at the bottom of TUI mode
// (which shows the live dashboard during the run instead).
func printBundlesHeader(bundles []configBundle) {
	for _, b := range bundles {
		fmt.Printf("Loaded config: %s (schema v%d)\n", b.path, b.cfg.Version)
		fmt.Printf("Validation:    %s\n", b.status)
	}
	fmt.Println()
}

// runFromConfigPlain is the legacy text path: per-endpoint progress
// printed inline, then the overall aggregate. Used when stdout is
// not a TTY.
func runFromConfigPlain(bundles []configBundle, jobs []endpointJob, scenarios []scenarioJob) int {
	printBundlesHeader(bundles)

	var totalRequests, totalErrors int
	for _, job := range jobs {
		label := job.ep.Name
		if label == "" {
			label = job.ep.URL
		}
		fmt.Printf("=== %s %s ===\n", job.method, label)
		fmt.Printf("URL:     %s\n", job.ep.URL)
		fmt.Printf("Workers: %d | Requests: %d | Timeout: %s\n\n",
			job.ep.Concurrency, job.ep.Requests, job.ep.Timeout)

		summary := runOneEndpoint(job, nil)
		report.Print(os.Stdout, summary)
		fmt.Println()

		totalRequests += summary.Total
		totalErrors += summary.Errors
	}

	// Scenarios run after the flat endpoints, one report per step.
	sReqs, sErrs := runScenariosPlain(os.Stdout, scenarios)
	totalRequests += sReqs
	totalErrors += sErrs

	fmt.Printf("=== Overall ===\n")
	fmt.Printf("Endpoints tested: %d\n", len(jobs))
	if len(scenarios) > 0 {
		fmt.Printf("Scenarios tested: %d\n", len(scenarios))
	}
	fmt.Printf("Total requests:   %d\n", totalRequests)
	fmt.Printf("Total errors:     %d\n", totalErrors)

	if totalErrors > 0 {
		return 1
	}
	return 0
}

// runFromConfigTUI runs the load test under the live dashboard.
// Endpoints run in PARALLEL — one goroutine per endpoint, each
// invoking loader.Run independently and feeding its own progress
// stream into the TUI. The aggregate report after the TUI exits
// shows the same per-endpoint summaries as before.
//
// Note that per-endpoint concurrency (`c` in the config) stacks
// on top of this: 10 endpoints with c=5 means 50 concurrent
// in-flight requests in total. That's almost always what you
// want for stress testing (everything hitting the target at
// once); when it isn't, drop `c` per endpoint.
//
// If the user quits early, in-flight requests on every endpoint
// still complete (loader.Run is blocking; cancellation is a v1.1
// concern). Exit code is 0 on a clean user quit even if errors
// accumulated — the user told us to stop.
func runFromConfigTUI(bundles []configBundle, jobs []endpointJob) int {
	endpoints := make([]tui.Endpoint, 0, len(jobs))
	for _, job := range jobs {
		endpoints = append(endpoints, tui.EndpointFromConfig(job.method, job.ep))
	}

	program := tui.NewProgram(endpoints, os.Stdout)

	var (
		summaries     = make([]report.Summary, len(jobs))
		summariesDone = make([]bool, len(jobs))
		quitRequested atomic.Bool
		runStart      = time.Now()
		workerDone    = make(chan struct{})
	)

	go func() {
		defer close(workerDone)
		// Fan out: every endpoint runs concurrently in its own
		// goroutine. A WaitGroup gates the Done message until all
		// have finished naturally OR the user has quit and the
		// in-flight batches drain.
		var wg sync.WaitGroup
		for _, job := range jobs {
			if quitRequested.Load() {
				break
			}
			wg.Add(1)
			go func(j endpointJob) {
				defer wg.Done()
				program.Send(tui.EndpointStart{Index: j.idx})
				summary := runOneEndpoint(j, func(r loader.Result) {
					program.Send(tui.Result(j.idx, r))
				})
				summaries[j.idx] = summary
				summariesDone[j.idx] = true
				program.Send(tui.EndpointEnd{Index: j.idx})
			}(job)
		}
		wg.Wait()
		program.Send(tui.Done{})
	}()

	userQuit, runErr := program.Run()
	if userQuit {
		quitRequested.Store(true)
	}
	<-workerDone
	totalElapsed := time.Since(runStart)

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
		return 1
	}

	// Compact post-TUI summary: a single-screen recap rather than
	// reprinting the verbose per-endpoint report we already showed
	// live. Users who want the full breakdown can re-run with
	// stdout piped (which uses the plain-text path automatically).
	printCompactSummary(bundles, jobs, summaries, summariesDone, totalElapsed)

	// If any endpoint returned a non-2xx status or had transport
	// errors, open a pager with the per-endpoint detail. Short
	// content flies through via `less -F`; long content paginates
	// and the user q's out. Skipped silently when no pager is
	// available, when stdout isn't a TTY, or when RKLOAD_NO_PAGER=1.
	if hasUnexpectedResults(jobs, summaries, summariesDone) {
		showUnexpectedInPager(jobs, summaries, summariesDone)
	}

	totalErrors := 0
	for _, job := range jobs {
		if summariesDone[job.idx] {
			totalErrors += summaries[job.idx].Errors
		}
	}
	if userQuit {
		return 0
	}
	if totalErrors > 0 {
		return 1
	}
	return 0
}

// hasUnexpectedResults reports whether any completed endpoint
// produced a non-2xx response or a transport-level error. Used to
// decide whether the post-summary pager view is worth opening.
func hasUnexpectedResults(jobs []endpointJob, summaries []report.Summary, done []bool) bool {
	for _, job := range jobs {
		if !done[job.idx] {
			continue
		}
		s := summaries[job.idx]
		if s.Errors > 0 {
			return true
		}
		for code := range s.StatusCodes {
			if code < 200 || code >= 300 {
				return true
			}
		}
	}
	return false
}

// showUnexpectedInPager renders a detailed report of endpoints
// that produced non-2xx responses or transport errors, then pipes
// it through $PAGER (default `less -FRX`). Falls back to direct
// stdout when no pager is available so users without `less`
// installed still see the content.
//
// The pager flags `-FRX` are the canonical "smart pager" combo
// (also what git uses): -F quits if content fits on one screen,
// -R preserves ANSI colour, -X doesn't clear the screen on exit.
func showUnexpectedInPager(jobs []endpointJob, summaries []report.Summary, done []bool) {
	content := renderUnexpectedReport(jobs, summaries, done)
	if content == "" {
		return
	}

	if os.Getenv("RKLOAD_NO_PAGER") == "1" {
		fmt.Print(content)
		return
	}

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -FRX"
	}

	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Print(content)
		return
	}
	if err := cmd.Start(); err != nil {
		// Pager not installed or unrunnable — fall back to plain
		// stdout. Silent so we don't pollute the user's terminal
		// with a "less not found" message they didn't ask for.
		fmt.Print(content)
		return
	}
	_, _ = io.WriteString(stdin, content)
	_ = stdin.Close()
	_ = cmd.Wait()
}

// renderUnexpectedReport builds the per-endpoint detail block —
// one bordered table per endpoint that had non-2xx responses or
// transport errors. Empty string when no endpoint qualifies.
//
// Each line is `<code> <name>: <count>` so the output is greppable
// and the standard-name column lines up across rows.
func renderUnexpectedReport(jobs []endpointJob, summaries []report.Summary, done []bool) string {
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).
		Render("Endpoints with unexpected status codes or transport errors"))
	b.WriteString("\n\n")
	b.WriteString(dim.Render("Press q to close, ↑↓ to scroll. " +
		"Anything outside 2xx is treated as \"unexpected\" by default."))
	b.WriteString("\n")

	any := false
	for _, job := range jobs {
		if !done[job.idx] {
			continue
		}
		s := summaries[job.idx]

		var unexpected []int
		for code := range s.StatusCodes {
			if code < 200 || code >= 300 {
				unexpected = append(unexpected, code)
			}
		}
		if len(unexpected) == 0 && s.Errors == 0 {
			continue
		}
		sort.Ints(unexpected)
		any = true

		title := fmt.Sprintf("%s %s", job.method, endpointLabel(job.ep))
		b.WriteString("\n")
		b.WriteString(red.Render("✗ ") + lipgloss.NewStyle().Bold(true).Render(title))
		b.WriteString(dim.Render(fmt.Sprintf("    %d total requests, %d errors", s.Total, s.Errors)))
		b.WriteString("\n")

		t := lgtable.New().
			Border(lipgloss.NormalBorder()).
			BorderStyle(border).
			Headers("CODE", "NAME", "COUNT").
			StyleFunc(func(row, col int) lipgloss.Style {
				if row == lgtable.HeaderRow {
					return header.Padding(0, 1).Align(lipgloss.Left)
				}
				base := lipgloss.NewStyle().Padding(0, 1)
				if col == 2 {
					base = base.Align(lipgloss.Right)
				}
				return base.Foreground(classColorAnsi(0).GetForeground()) // overridden below per row
			})

		for _, code := range unexpected {
			t.Row(
				classColorAnsi(code).Render(fmt.Sprintf("%d", code)),
				http.StatusText(code),
				fmt.Sprintf("%d", s.StatusCodes[code]),
			)
		}
		if s.Errors > 0 {
			t.Row(
				red.Render("err"),
				"transport-level (timeout / refused / DNS / TLS / other)",
				fmt.Sprintf("%d", s.Errors),
			)
		}
		b.WriteString(t.Render())
		b.WriteString("\n")
	}

	if !any {
		return ""
	}
	return b.String()
}

// printCompactSummary writes a one-screen recap of a finished TUI
// run as a bordered table — same shape as the live TUI table, just
// frozen and printed to normal stdout so it survives the alt-screen
// dismissal. Each endpoint contributes one row (status, method,
// name, done counter, throughput, p95). Aggregate status code
// counts and average throughput print below the table.
//
// Only invoked on the TUI path (TTY-only), so ANSI styles are
// appropriate; non-TTY runs use the verbose plain-text reporter.
func printCompactSummary(bundles []configBundle, jobs []endpointJob,
	summaries []report.Summary, done []bool, elapsed time.Duration) {

	titleColor := lipgloss.Color("63")
	dimColor := lipgloss.Color("241")
	greenColor := lipgloss.Color("42")
	redColor := lipgloss.Color("203")
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))

	totalRequests, totalErrors, endpointsDone := 0, 0, 0
	allStatus := map[int]int{}
	for _, job := range jobs {
		if !done[job.idx] {
			continue
		}
		s := summaries[job.idx]
		totalRequests += s.Total
		totalErrors += s.Errors
		endpointsDone++
		for code, n := range s.StatusCodes {
			allStatus[code] += n
		}
	}

	// Title line.
	fmt.Println()
	titleLine := lipgloss.NewStyle().Bold(true).Foreground(titleColor).Render("rkload — done") +
		"  " + lipgloss.NewStyle().Foreground(dimColor).Render(fmt.Sprintf(
		"Configs: %d   Endpoints: %d   Requests: %d   Elapsed: %s",
		len(bundles), endpointsDone, totalRequests, formatShortDuration(elapsed)))
	fmt.Println(titleLine)
	if endpointsDone < len(jobs) {
		fmt.Println(lipgloss.NewStyle().Foreground(dimColor).Render(
			fmt.Sprintf("                Skipped: %d (user quit)", len(jobs)-endpointsDone)))
	}
	fmt.Println()

	// Per-endpoint table with the full percentile set so users
	// don't have to re-run to see p50/p99.
	t := lgtable.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(border).
		Headers("STATUS", "METHOD", "ENDPOINT", "DONE", "R/S", "AVG", "P50", "P95", "P99").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == lgtable.HeaderRow {
				return header.Padding(0, 1).Align(lipgloss.Left)
			}
			base := lipgloss.NewStyle().Padding(0, 1)
			// Right-align all numeric columns (everything from DONE onward).
			if col >= 3 {
				base = base.Align(lipgloss.Right)
			}
			if row < 0 {
				return base
			}
			return base.Foreground(rowColorFromIndex(row, jobs, summaries, done, greenColor, redColor, dimColor))
		})

	for _, job := range jobs {
		if !done[job.idx] {
			continue
		}
		s := summaries[job.idx]
		status := "✓ done"
		if summaryHasUnexpected(s) {
			status = "✗ fail"
		}
		t.Row(
			status,
			job.method,
			truncate(endpointLabel(job.ep), 40),
			fmt.Sprintf("%d/%d", s.Successful, s.Total),
			fmt.Sprintf("%.1f r/s", s.Throughput),
			formatShortDuration(s.AvgLatency),
			formatShortDuration(s.P50Latency),
			formatShortDuration(s.P95Latency),
			formatShortDuration(s.P99Latency),
		)
	}
	fmt.Println(t.Render())
	fmt.Println()

	// Aggregate footer.
	if len(allStatus) > 0 {
		var codes []int
		for c := range allStatus {
			codes = append(codes, c)
		}
		sort.Ints(codes)
		var parts []string
		for _, c := range codes {
			parts = append(parts, classColorAnsi(c).Render(fmt.Sprintf("%d:%d", c, allStatus[c])))
		}
		fmt.Println(header.Render("Status   ") + strings.Join(parts, "  "))
	}
	if totalErrors > 0 {
		fmt.Println(lipgloss.NewStyle().Foreground(redColor).Render(
			fmt.Sprintf("Errors   %d", totalErrors)))
	}
	if elapsed > 0 && totalRequests > 0 {
		fmt.Println(header.Render("R/S      ") +
			fmt.Sprintf("%.1f total", float64(totalRequests)/elapsed.Seconds()))
	}
}

// rowColorFromIndex maps a row index in the rendered (filtered)
// table back to a status colour. Used by the lipgloss StyleFunc
// which only gets (row, col) — not the underlying job.
//
// "Failed" = transport errors OR any non-2xx responses. The post-
// summary table now flips a row red when the endpoint only returned
// non-2xx statuses (which previously displayed green-checkmark
// success because the loader saw no transport errors).
func rowColorFromIndex(row int, jobs []endpointJob, summaries []report.Summary,
	done []bool, greenColor, redColor, dimColor lipgloss.Color) lipgloss.Color {
	i := 0
	for _, job := range jobs {
		if !done[job.idx] {
			continue
		}
		if i == row {
			if summaryHasUnexpected(summaries[job.idx]) {
				return redColor
			}
			return greenColor
		}
		i++
	}
	return dimColor
}

// summaryHasUnexpected reports whether a finished endpoint had any
// non-2xx response or transport error — i.e. anything a stress
// tester would consider a failure. Mirrored from
// internal/tui.hasUnexpectedResults (the TUI's per-endpoint
// state has the same shape but different types, so we can't
// share the function directly).
func summaryHasUnexpected(s report.Summary) bool {
	if s.Errors > 0 {
		return true
	}
	for code := range s.StatusCodes {
		if code < 200 || code >= 300 {
			return true
		}
	}
	return false
}

// classColorAnsi mirrors the TUI's classColor for the post-run
// status histogram. Duplicated here so cmd/ doesn't have to import
// internal/tui purely for its colour helper.
func classColorAnsi(code int) lipgloss.Style {
	switch {
	case code >= 200 && code < 300:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	case code >= 300 && code < 400:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	case code >= 400 && code < 500:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case code >= 500:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	}
}

// truncate clamps s to at most n display-width cells, appending an
// ellipsis if it had to chop. Mirrors the helper in internal/tui.
func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func endpointLabel(ep config.Endpoint) string {
	if ep.Name != "" {
		return ep.Name
	}
	return ep.URL
}

// formatShortDuration mirrors the TUI's compact duration style
// (used inside the recap line so the two views look consistent).
func formatShortDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		mins := int(d.Minutes())
		secs := int(d.Seconds()) - mins*60
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
}

// runOneEndpoint runs a single endpoint to completion and returns
// its report.Summary. onResult, when non-nil, is forwarded to the
// loader as Options.OnResult so live progress UIs can subscribe.
// Shared by both the plain and TUI paths so the per-endpoint
// behaviour stays identical.
func runOneEndpoint(job endpointJob, onResult func(loader.Result)) report.Summary {
	timeout, _ := job.ep.ParsedTimeout() // Validate already proved this parses
	opts := loader.Options{
		URL:         job.ep.URL,
		Method:      job.method,
		Concurrency: job.ep.Concurrency,
		Requests:    job.ep.Requests,
		Timeout:     timeout,
		Headers:     job.ep.Headers,
		Body:        job.ep.Body,
		OnResult:    onResult,
	}
	start := time.Now()
	results := loader.Run(opts)
	elapsed := time.Since(start)
	return report.Summarize(results, elapsed)
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
