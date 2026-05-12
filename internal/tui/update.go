package tui

import (
	"time"

	"github.com/RKInnovate/rkload/internal/loader"

	tea "github.com/charmbracelet/bubbletea"
)

// sampleInterval is how often the throughput sparkline samples
// completed-request count. 250ms is short enough to feel live and
// long enough that a momentary stall doesn't drop the sparkline to
// zero.
const sampleInterval = 250 * time.Millisecond

// Messages that drive the Model. EndpointStart / EndpointEnd /
// Done are sent from outside the TUI (by the run orchestration
// goroutine) via Program.Send. resultMsg carries the loader's
// OnResult callback through the same channel.
type (
	// EndpointStart marks endpoint i as the currently-running one.
	EndpointStart struct{ Index int }
	// EndpointEnd finalises endpoint i's stats.
	EndpointEnd struct{ Index int }
	// Done signals every endpoint has finished. Triggers a clean
	// shutdown of the TUI so View() can render its final frame
	// before the program returns.
	Done struct{}
	// resultMsg is the per-request signal from loader.OnResult.
	resultMsg struct {
		index  int
		result loader.Result
	}
	// tickMsg fires on the sparkline sampling timer.
	tickMsg struct{ At time.Time }
)

// Result wraps a (endpoint index, loader.Result) pair for
// callers using bubbletea's Send(). Exported so cmd-side code can
// construct it from outside the package.
func Result(index int, r loader.Result) tea.Msg {
	return resultMsg{index: index, result: r}
}

// Init starts the sparkline tick.
func (m Model) Init() tea.Cmd {
	now := time.Now()
	return tea.Batch(
		func() tea.Msg { return tickMsg{At: now} },
		tea.Tick(sampleInterval, func(t time.Time) tea.Msg { return tickMsg{At: t} }),
	)
}

// Update is the bubbletea Model contract. Every state mutation
// goes through this function — tests in tui_test.go exercise it
// directly without spinning up the terminal.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case EndpointStart:
		m.currentIdx = msg.Index
		if msg.Index >= 0 && msg.Index < len(m.states) {
			now := time.Now()
			if m.startedAt.IsZero() {
				m.startedAt = now
				m.lastTickAt = now
			}
			m.states[msg.Index].startedAt = now
		}
		// Default the selection to the current endpoint when the
		// user hasn't already picked one with the arrow keys.
		if m.selectedIdx < 0 || m.selectedIdx == msg.Index-1 {
			m.selectedIdx = msg.Index
		}
		return m, nil

	case EndpointEnd:
		if msg.Index >= 0 && msg.Index < len(m.states) {
			m.states[msg.Index].finished = true
			m.states[msg.Index].finishedAt = time.Now()
		}
		return m, nil

	case Done:
		m.currentIdx = len(m.endpoints)
		m.endedAt = time.Now()
		return m, tea.Quit

	case resultMsg:
		if msg.index >= 0 && msg.index < len(m.states) {
			m.states[msg.index].record(msg.result)
		}
		return m, nil

	case tickMsg:
		m.advanceSparkline(msg.At)
		// Keep ticking unless the run is done.
		if m.done() || m.quitting {
			return m, nil
		}
		return m, tea.Tick(sampleInterval, func(t time.Time) tea.Msg { return tickMsg{At: t} })
	}
	return m, nil
}

// handleKey processes keyboard input. q / ctrl+c quits; arrows
// move the selection; enter drills in to the selected endpoint;
// esc / left arrow comes back to the overview.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if len(m.endpoints) > 0 && m.selectedIdx > 0 {
			m.selectedIdx--
		}
		return m, nil
	case "down", "j":
		if len(m.endpoints) > 0 && m.selectedIdx < len(m.endpoints)-1 {
			m.selectedIdx++
		}
		return m, nil
	case "enter", "right", "l":
		if len(m.endpoints) > 0 {
			m.drillMode = true
		}
		return m, nil
	case "esc", "left", "h":
		m.drillMode = false
		return m, nil
	}
	return m, nil
}

// advanceSparkline appends one sample of req/sec since the last
// tick to the ring buffer, dropping the oldest entry once the
// buffer is full.
func (m *Model) advanceSparkline(at time.Time) {
	completed := m.completedRequests()
	if !m.lastTickAt.IsZero() {
		delta := at.Sub(m.lastTickAt).Seconds()
		if delta > 0 {
			rps := float64(completed-m.lastTickCompleted) / delta
			m.throughputHistory = append(m.throughputHistory, rps)
			if len(m.throughputHistory) > sparklineWidth {
				m.throughputHistory = m.throughputHistory[len(m.throughputHistory)-sparklineWidth:]
			}
		}
	}
	m.lastTickAt = at
	m.lastTickCompleted = completed
}
