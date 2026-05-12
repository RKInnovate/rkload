package tui

import (
	"errors"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// Program is a small wrapper around tea.Program that exposes only
// the operations cmd needs: Send to push messages in, Run to block
// until the TUI exits.
type Program struct {
	p *tea.Program
}

// NewProgram constructs a TUI program over the given endpoints,
// writing output to w (use os.Stdout in production). Returns the
// program and the initial model so callers that want to inspect
// final state (for the post-run plain-text aggregate) can.
func NewProgram(endpoints []Endpoint, w io.Writer) *Program {
	m := New(endpoints)
	opts := []tea.ProgramOption{
		tea.WithAltScreen(), // dedicated screen buffer; restores terminal on exit
	}
	if w != nil {
		opts = append(opts, tea.WithOutput(w))
	}
	return &Program{p: tea.NewProgram(m, opts...)}
}

// Send pushes a tea.Msg into the program's queue. Safe to call
// from any goroutine.
func (p *Program) Send(msg tea.Msg) {
	if p == nil || p.p == nil {
		return
	}
	p.p.Send(msg)
}

// Run blocks until the program exits (Done message + tea.Quit, or
// q / ctrl+c). The returned bool reports whether the user quit
// early (true) or the run completed naturally (false). Returns
// the underlying error from tea.Program.Run if the TUI failed
// to start or render.
func (p *Program) Run() (userQuit bool, err error) {
	if p == nil || p.p == nil {
		return false, errors.New("tui: nil program")
	}
	final, runErr := p.p.Run()
	if runErr != nil {
		return false, runErr
	}
	m, ok := final.(Model)
	if !ok {
		return false, nil
	}
	return m.quitting, nil
}
