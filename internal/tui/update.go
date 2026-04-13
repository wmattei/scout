package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// computeResults returns the fuzzy match results for a query, or an empty
// slice if the query is empty (so the TUI shows the "start typing" hint
// instead of every cached resource).
func computeResults(query string, mem *index.Memory, height int) []search.Result {
	if query == "" {
		return nil
	}
	return search.Fuzzy(query, mem.All(), maxInt(1, height-3))
}

// Custom messages emitted by commands.
type (
	msgResourcesUpdated struct{}
	msgAccount          struct{ account string }
	msgSpinTick         struct{}
)

// Update routes messages to state mutations and side-effect commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil
		case "enter", "tab", "ctrl+p", "ctrl+r", "esc":
			// Reserved for later phases. No-op in Phase 1.
			return m, nil
		}

		// Let the textinput consume the keystroke.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Recompute results from the current cache + new query. An empty
		// query shows no results (the TUI renders a "start typing" hint
		// instead) so the user isn't dumped into a sea of everything on
		// launch or after clearing the input.
		m.results = computeResults(m.input.Value(), m.memory, m.height)
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, cmd

	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current result list against the updated snapshot. No-op when the
		// query is empty (the empty-state hint handles that case in View).
		m.results = computeResults(m.input.Value(), m.memory, m.height)
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, nil

	case msgAccount:
		m.account = msg.account
		return m, nil

	case msgSpinTick:
		m.spinTick++
		return m, spinTickCmd()
	}

	return m, nil
}

// spinTickCmd schedules the next spinner frame. 100ms gives ~10fps which is
// plenty for a braille spinner and costs almost nothing.
func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
