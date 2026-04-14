package tui

import "fmt"

// renderTailLogs produces the full Tail Logs screen body. The viewport
// holds the streamed content; this function just wraps it with a header
// and a footer-help row.
func renderTailLogs(m Model, height int) string {
	header := styleDetailsHeader.Render("Tail Logs — " + m.tailGroup)
	help := styleRowDim.Render("Esc back    Ctrl+C stop")

	vpHeight := height - 3
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.tailViewport.Height = vpHeight
	m.tailViewport.Width = m.width

	body := m.tailViewport.View()

	return fmt.Sprintf("%s\n\n%s\n\n%s",
		header,
		body,
		help,
	)
}
