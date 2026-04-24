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

	// Favorite-toggle hint. Rendered on its own line immediately
	// above the bottom divider when a resource is in focus. The tail
	// and switcher modes suppress it because they have their own
	// footer content / overlays.
	hintText := ""
	if m.mode == modeSearch || m.mode == modeDetails {
		hintText = favoriteHintFor(m)
	}
	hintLine := ""
	if hintText != "" {
		hintLine = padRight(styleRowDim.Render(" "+hintText), m.width)
	}

	// Base body height budget: total − input − 2 dividers − status.
	// When the hint is visible we borrow one more line from body.
	bodyHeight := m.height - 4
	if hintLine != "" {
		bodyHeight--
	}
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch m.mode {
	case modeDetails:
		// renderDetails populates m.detailsHitMap with regions in
		// body-local coordinates. The body starts two lines down
		// from the top of the frame (input + divider), so every
		// region's Y is shifted by 2 before the mouse handler sees
		// it.
		body = renderDetails(m, m.width, bodyHeight)
		body = padBlock(body, bodyHeight)
		if m.detailsHitMap != nil {
			const bodyOriginY = 2
			for i := range *m.detailsHitMap {
				(*m.detailsHitMap)[i].Y0 += bodyOriginY
				(*m.detailsHitMap)[i].Y1 += bodyOriginY
			}
		}
	case modeTailLogs:
		body = renderTailLogs(m, bodyHeight)
	case modeSwitcher:
		body = renderSwitcher(m.switcher, m.width, bodyHeight)
	case modeOnboarding:
		body = renderOnboarding(m, m.width, bodyHeight)
	default:
		body = m.renderSearchBody(bodyHeight)
	}

	// Compose the frame with or without the optional hint line.
	lines := []string{inputLine, divider, body}
	if hintLine != "" {
		lines = append(lines, hintLine)
	}
	lines = append(lines, divider)

	if m.toast.isActive() {
		lines = append(lines, renderToast(m.toast, m.width))
	} else {
		lines = append(lines, status)
	}

	return strings.Join(lines, "\n")
}

// renderSearchBody produces the middle zone for modeSearch — either the
// top-level fuzzy list, the scoped prefix list, the Favorites+Recents
// home page (empty input with user prefs), or the right empty state
// when nothing is active.
func (m Model) renderSearchBody(height int) string {
	inputValue := m.input.Value()

	// Home page takes over when the input is empty AND the user has
	// at least one favorite or recent. Otherwise fall through to the
	// normal empty-state logic below so first-run users see the
	// cache-empty guidance.
	if inputValue == "" && homeActive(m) {
		return renderHome(m, buildHomeSections(m), m.width, height)
	}

	visible := m.visibleSearchResults()

	emptyMsg := "no results"
	switch {
	case inputValue == "":
		emptyMsg = "start typing to search cached resources, or type a service scope (s3:, ecs:, td:)"
	case m.isLoadingScoped() && len(visible) == 0:
		emptyMsg = fmt.Sprintf("%s  loading %s", spinnerFrame(m.spinTick), inputValue)
	case len(visible) == 0:
		emptyMsg = fmt.Sprintf("no matches for %q", inputValue)
	}
	return renderResults(visible, m.selected, m.width, height, emptyMsg, m.prefsState, m.registry)
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
