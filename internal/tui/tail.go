package tui

import "fmt"

// renderTailLogs produces the full Tail Logs screen body. The viewport
// holds the streamed content; this function just wraps it with a header
// and a footer-help row.
func renderTailLogs(m Model, height int) string {
	header := styleDetailsHeader.Render("Tail Logs — " + m.tailGroup)
	help := styleRowDim.Render("Esc back    Ctrl+C stop    ↑/↓ scroll")

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
