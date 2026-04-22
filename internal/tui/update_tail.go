package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	"github.com/wmattei/scout/internal/services"
)

// updateTail handles key events while in modeTailLogs. Esc stops the
// stream and returns to the Details view; Ctrl+C quits the program. All
// other keys are forwarded to the viewport so the user can scroll.
func (m Model) updateTail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter editing mode: the user pressed "/" and is typing a filter
	// string. Every printable key appends; Backspace trims; Enter/Esc
	// commits (Esc also clears). While editing, scroll is suppressed.
	if m.tailFilterEditing {
		switch msg.String() {
		case "enter":
			m.tailFilterEditing = false
			m.rebuildTailViewport()
			m.tailViewport.GotoBottom()
			return m, nil
		case "esc":
			m.tailFilter = ""
			m.tailFilterEditing = false
			m.rebuildTailViewport()
			m.tailViewport.GotoBottom()
			return m, nil
		case "backspace":
			if len(m.tailFilter) > 0 {
				r := []rune(m.tailFilter)
				m.tailFilter = string(r[:len(r)-1])
				m.rebuildTailViewport()
				m.tailViewport.GotoBottom()
			}
			return m, nil
		}
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			m.tailFilter += string(msg.Runes[0])
			m.rebuildTailViewport()
			m.tailViewport.GotoBottom()
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		return m, tea.Quit
	case "esc":
		// First Esc clears an active filter; second Esc (or no filter)
		// exits the tail view.
		if m.tailFilter != "" {
			m.tailFilter = ""
			m.rebuildTailViewport()
			m.tailViewport.GotoBottom()
			return m, nil
		}
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		m.mode = modeDetails
		m.toast = newToast("stopped tailing", 2*time.Second)
		// Re-fire a fresh resolve so the details panel shows current
		// data after returning from the tail view.
		if p, ok := services.Get(m.detailsResource.Type); ok {
			key := lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}
			if p.AlwaysRefresh() {
				delete(m.lazyDetails, key)
				m.lazyDetailsState[key] = lazyStateInFlight
			}
			return m, resolveLazyDetailsCmd(m.awsCtx, p, m.detailsResource)
		}
		return m, nil
	case "ctrl+down":
		m.tailViewport.GotoBottom()
		return m, nil
	case "/":
		m.tailFilterEditing = true
		return m, nil
	}
	var cmd tea.Cmd
	m.tailViewport, cmd = m.tailViewport.Update(msg)
	return m, cmd
}

// rebuildTailViewport recomputes the viewport content from m.tailLines
// applying the current tailFilter.
func (m *Model) rebuildTailViewport() {
	if m.tailFilter == "" {
		m.tailViewport.SetContent(strings.Join(m.tailLines, "\n"))
		return
	}
	var filtered []string
	lower := strings.ToLower(m.tailFilter)
	for _, line := range m.tailLines {
		if strings.Contains(strings.ToLower(line), lower) {
			filtered = append(filtered, line)
		}
	}
	m.tailViewport.SetContent(strings.Join(filtered, "\n"))
}

// formatTailLine renders a single tail event into a display line with a
// local-time timestamp prefix.
func formatTailLine(ev awslogs.TailEvent) string {
	ts := time.Unix(0, ev.Timestamp*int64(time.Millisecond)).Local().Format("15:04:05.000")
	return ts + " " + ev.Message
}
