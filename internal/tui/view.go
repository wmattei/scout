package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full frame: input row, divider, result list, divider,
// status row.
func (m Model) View() string {
	// Minimum usable width check (per spec §7).
	if m.width < 60 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleError.Render(fmt.Sprintf("terminal too narrow — resize ≥60 columns (current: %d)", m.width)))
	}

	input := m.input.View()
	// Right-aligned glyph so the bar looks intentional.
	inputLine := fmt.Sprintf("%s%s", padRight(input, m.width-3), " 🔍")

	// Divider.
	divider := styleDivider.Render(strings.Repeat("─", m.width))

	// Status.
	status := renderStatus(m.width, m.awsCtx.Profile, m.awsCtx.Region, m.account, m.activity.Snapshot(), m.spinTick)

	// Result list height = terminal height - input(1) - divider(1) - divider(1) - status(1).
	resultsHeight := m.height - 4
	if resultsHeight < 1 {
		resultsHeight = 1
	}

	emptyMsg := "no results"
	switch {
	case m.input.Value() == "" && len(m.results) == 0:
		emptyMsg = "cache is empty — fetching…"
	case m.input.Value() != "" && len(m.results) == 0:
		emptyMsg = fmt.Sprintf("no matches for %q", m.input.Value())
	}
	results := renderResults(m.results, m.selected, m.width, resultsHeight, emptyMsg)

	return strings.Join([]string{
		inputLine,
		divider,
		results,
		divider,
		status,
	}, "\n")
}
