package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Style palette. Kept terse on purpose — readable terminals win
// over flashy ones. Foreground colours only; backgrounds rarely
// look good on every terminal theme.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))  // purple
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")) // light grey
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))            // mid grey
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))             // green
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))            // orange/yellow
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))            // red
	currentStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("219")) // pink — running
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))  // blue — selected
)

// View renders one frame. Pure function of Model state — same
// Model always produces the same string.
func (m Model) View() string {
	if len(m.endpoints) == 0 {
		return dimStyle.Render("(no endpoints to run)") + "\n"
	}
	if m.drillMode && m.selectedIdx >= 0 && m.selectedIdx < len(m.endpoints) {
		return m.renderDrilldown()
	}
	return m.renderOverview()
}

func (m Model) renderOverview() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderEndpointList())
	b.WriteString("\n")
	b.WriteString(m.renderAggregate())
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m Model) renderHeader() string {
	totalReq := m.totalRequests()
	completed := m.completedRequests()
	pct := 0
	if totalReq > 0 {
		pct = completed * 100 / totalReq
	}

	title := titleStyle.Render("rkload — live")
	if m.done() {
		title = titleStyle.Render("rkload — done")
	}
	elapsed := m.runElapsed()
	body := fmt.Sprintf(
		"Endpoints: %d   Requests: %d/%d (%d%%)   Elapsed: %s",
		len(m.endpoints), completed, totalReq, pct, formatDuration(elapsed),
	)
	return title + "  " + dimStyle.Render(body)
}

// renderEndpointList shows every endpoint, one per row, with
// progress bar and per-endpoint counters. Selected endpoint gets
// blue highlight; running endpoint gets pink.
func (m Model) renderEndpointList() string {
	var lines []string
	maxName := 0
	for _, ep := range m.endpoints {
		label := fmt.Sprintf("%s %s", ep.Method, ep.Name)
		if l := lipgloss.Width(label); l > maxName {
			maxName = l
		}
	}
	if maxName > 40 {
		maxName = 40
	}

	for i, ep := range m.endpoints {
		s := &m.states[i]
		label := fmt.Sprintf("%s %s", ep.Method, ep.Name)
		if w := lipgloss.Width(label); w > maxName {
			label = label[:maxName-1] + "…"
		}
		label = padRight(label, maxName)

		bar := renderProgressBar(s.completed, ep.Total, 22)
		counter := fmt.Sprintf("%d/%d", s.completed, ep.Total)
		throughput := s.throughput(time.Now())
		p95 := percentile(append([]time.Duration(nil), s.latencies...), 95)

		stats := fmt.Sprintf("%s  %6.1f r/s  p95 %s",
			padLeft(counter, 9),
			throughput,
			padLeft(formatDuration(p95), 7),
		)

		line := fmt.Sprintf("  %s  %s  %s", label, bar, stats)

		switch {
		case i == m.currentIdx && !s.finished:
			line = currentStyle.Render(line)
		case i == m.selectedIdx:
			line = selectedStyle.Render(line)
		case s.finished:
			line = successStyle.Render("✓") + line[1:]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderAggregate prints the live status histogram, percentile
// ticker, and throughput sparkline.
func (m Model) renderAggregate() string {
	statuses := map[int]int{}
	totalErr := 0
	var allLatencies []time.Duration
	for _, s := range m.states {
		for code, n := range s.statusCodes {
			statuses[code] += n
		}
		totalErr += s.errCount
		allLatencies = append(allLatencies, s.latencies...)
	}

	var status strings.Builder
	status.WriteString(headerStyle.Render("Status codes "))
	codes := sortedCodes(statuses)
	if len(codes) == 0 {
		status.WriteString(dimStyle.Render("(no responses yet)"))
	} else {
		var parts []string
		for _, c := range codes {
			parts = append(parts, classColor(c).Render(fmt.Sprintf("%d:%d", c, statuses[c])))
		}
		status.WriteString(strings.Join(parts, "  "))
	}
	if totalErr > 0 {
		status.WriteString("  " + errorStyle.Render(fmt.Sprintf("errors:%d", totalErr)))
	}

	// Percentile ticker.
	p50 := percentile(append([]time.Duration(nil), allLatencies...), 50)
	p95 := percentile(append([]time.Duration(nil), allLatencies...), 95)
	p99 := percentile(append([]time.Duration(nil), allLatencies...), 99)
	pctile := fmt.Sprintf("p50 %s   p95 %s   p99 %s",
		formatDuration(p50), formatDuration(p95), formatDuration(p99))

	// Sparkline.
	spark := renderSparkline(m.throughputHistory)
	currentRPS := 0.0
	if n := len(m.throughputHistory); n > 0 {
		currentRPS = m.throughputHistory[n-1]
	}
	throughput := fmt.Sprintf("%-40s  %.1f r/s", spark, currentRPS)

	return strings.Join([]string{
		status.String(),
		headerStyle.Render("Latency      ") + pctile,
		headerStyle.Render("Throughput   ") + throughput,
	}, "\n")
}

func (m Model) renderFooter() string {
	if m.drillMode {
		return dimStyle.Render("[esc/←] back   [q] quit")
	}
	if m.done() {
		return dimStyle.Render("[q] dismiss")
	}
	return dimStyle.Render("[↑↓] select   [↵/→] drill in   [q] quit")
}

// renderDrilldown shows the per-endpoint detail view.
func (m Model) renderDrilldown() string {
	ep := m.endpoints[m.selectedIdx]
	s := &m.states[m.selectedIdx]

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("%s %s", ep.Method, ep.Name)))
	b.WriteString("  " + dimStyle.Render(ep.URL))
	b.WriteString("\n\n")

	pct := 0
	if ep.Total > 0 {
		pct = s.completed * 100 / ep.Total
	}
	b.WriteString(fmt.Sprintf(
		"%s  %s  (%d%%)\n",
		renderProgressBar(s.completed, ep.Total, 30),
		fmt.Sprintf("%d/%d", s.completed, ep.Total),
		pct,
	))
	b.WriteString(fmt.Sprintf("Throughput: %.1f r/s\n", s.throughput(time.Now())))
	if len(s.latencies) > 0 {
		p50 := percentile(append([]time.Duration(nil), s.latencies...), 50)
		p95 := percentile(append([]time.Duration(nil), s.latencies...), 95)
		p99 := percentile(append([]time.Duration(nil), s.latencies...), 99)
		b.WriteString(fmt.Sprintf("Latency:    p50 %s   p95 %s   p99 %s\n",
			formatDuration(p50), formatDuration(p95), formatDuration(p99)))
	}
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("Status codes\n"))
	codes := sortedCodes(s.statusCodes)
	if len(codes) == 0 {
		b.WriteString("  " + dimStyle.Render("(none yet)\n"))
	} else {
		for _, c := range codes {
			b.WriteString(fmt.Sprintf("  %s\n", classColor(c).Render(fmt.Sprintf("%d: %d", c, s.statusCodes[c]))))
		}
	}
	if s.errCount > 0 {
		b.WriteString("  " + errorStyle.Render(fmt.Sprintf("errors: %d", s.errCount)) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

// renderProgressBar produces a filled/empty block bar of the
// given width, using the unicode block characters for partial
// fills so even short bars stay readable.
func renderProgressBar(done, total, width int) string {
	if total <= 0 || width <= 0 {
		return strings.Repeat(" ", width)
	}
	ratio := float64(done) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	fillUnits := ratio * float64(width)
	fullCells := int(fillUnits)
	partial := fillUnits - float64(fullCells)

	var b strings.Builder
	b.WriteString(strings.Repeat("█", fullCells))
	if fullCells < width {
		// Partial block. Eighth-block characters give 8 graduations.
		partials := []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}
		idx := int(partial * float64(len(partials)))
		if idx >= len(partials) {
			idx = len(partials) - 1
		}
		b.WriteRune(partials[idx])
		b.WriteString(strings.Repeat(" ", width-fullCells-1))
	}
	return b.String()
}

// classColor maps an HTTP status code to a style. 2xx green,
// 3xx blue, 4xx yellow, 5xx red, everything else dim.
func classColor(code int) lipgloss.Style {
	switch {
	case code >= 200 && code < 300:
		return successStyle
	case code >= 300 && code < 400:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // light blue
	case code >= 400 && code < 500:
		return warningStyle
	case code >= 500:
		return errorStyle
	default:
		return dimStyle
	}
}

func sortedCodes(m map[int]int) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// formatDuration produces a compact rkload-style duration string
// (e.g. "423ms", "1.2s", "10.3s"). Avoids Go's default formatter,
// which produces noisy "1.234567891s" precision.
func formatDuration(d time.Duration) string {
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

func (m Model) runElapsed() time.Duration {
	if m.startedAt.IsZero() {
		return 0
	}
	if !m.endedAt.IsZero() {
		return m.endedAt.Sub(m.startedAt)
	}
	return time.Since(m.startedAt)
}

func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func padLeft(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}
