package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// renderTailLogs produces the full Tail Logs screen body. The viewport
// holds the streamed content; this function just wraps it with a header
// and a footer-help row.
func renderTailLogs(m Model, height int) string {
	header := styleDetailsHeader.Render("Tail Logs — " + m.tailGroup)

	// Footer: context-sensitive based on filter state.
	var help string
	switch {
	case m.tailFilterEditing:
		help = styleFilterPrompt.Render("/") + m.tailFilter + styleFilterCursor.Render("█") +
			"    " + styleRowDim.Render("Enter apply    Esc cancel")
	case m.tailFilter != "":
		help = styleFilterActive.Render("filter: "+m.tailFilter) +
			"    " + styleRowDim.Render("/ edit    Esc clear    Ctrl+↓ follow")
	default:
		help = styleRowDim.Render("Esc back    Ctrl+C stop    ↑/↓ scroll    Ctrl+↓ follow    / filter")
	}

	// The viewport dimensions are set in the WindowSizeMsg handler
	// inside update.go so that scroll math during Update uses the
	// real terminal height. We just read the viewport's current
	// content here — no height mutation on the local copy.
	body := m.tailViewport.View()

	return fmt.Sprintf("%s\n\n%s\n\n%s",
		header,
		body,
		help,
	)
}

var (
	styleFilterPrompt = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
	styleFilterCursor = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
	styleFilterActive = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
				Background(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#005F87"}).
				Padding(0, 1)
)
