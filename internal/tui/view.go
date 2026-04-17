package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full frame. The input bar, dividers, and status bar
// are shared across all modes; the middle zone is mode-specific.
func (m Model) View() string {
	// Minimum usable width check (per spec §7).
	if m.width < 60 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleError.Render(fmt.Sprintf("terminal too narrow — resize ≥60 columns (current: %d)", m.width)))
	}

	input := m.input.View()
	inputLine := fmt.Sprintf("%s%s", padRight(input, m.width-3), " 🔍")

	divider := styleDivider.Render(strings.Repeat("─", m.width))

	status := renderStatus(m.width, m.awsCtx.Profile, m.awsCtx.Region, m.account, m.activity.Snapshot(), m.spinTick)

	bodyHeight := m.height - 4
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch m.mode {
	case modeDetails:
		body = renderDetails(m, m.width)
		body = padBlock(body, bodyHeight)
	case modeTailLogs:
		body = renderTailLogs(m, bodyHeight)
	case modeSwitcher:
		body = renderSwitcher(m.switcher, m.width, bodyHeight)
	default:
		body = m.renderSearchBody(bodyHeight)
	}

	// Optional toast overlay replaces the status line with a centered box
	// while the toast is active, keeping total height the same.
	if m.toast.isActive() {
		toastLine := renderToast(m.toast, m.width)
		return strings.Join([]string{
			inputLine,
			divider,
			body,
			divider,
			toastLine,
		}, "\n")
	}

	return strings.Join([]string{
		inputLine,
		divider,
		body,
		divider,
		status,
	}, "\n")
}

// renderSearchBody produces the middle zone for modeSearch — either the
// top-level fuzzy list or the scoped prefix list, with the right empty
// state when nothing is active.
func (m Model) renderSearchBody(height int) string {
	visible := m.visibleSearchResults()

	emptyMsg := "no results"
	inputValue := m.input.Value()
	switch {
	case inputValue == "" && m.memory.Len() == 0:
		emptyMsg = "empty cache — run `scout preload all` or type a service scope (s3:, ecs:, td:)"
	case inputValue == "":
		emptyMsg = "start typing to search cached resources"
	case m.isLoadingScoped() && len(visible) == 0:
		// Scoped search is in flight and we have nothing to show yet.
		// Render a loading message with the spinner frame so the user
		// knows the list is still being fetched, not genuinely empty.
		emptyMsg = fmt.Sprintf("%s  loading %s", spinnerFrame(m.spinTick), inputValue)
	case len(visible) == 0:
		emptyMsg = fmt.Sprintf("no matches for %q", inputValue)
	}
	return renderResults(visible, m.selected, m.width, height, emptyMsg, m.prefsState)
}

// padBlock appends blank lines to `body` until it has exactly `height`
// lines. If it already has more, it's returned unchanged.
func padBlock(body string, height int) string {
	lines := strings.Count(body, "\n") + 1
	if lines >= height {
		return body
	}
	return body + strings.Repeat("\n", height-lines)
}
