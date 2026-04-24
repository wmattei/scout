package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Custom messages emitted by commands.
type (
	msgAccount  struct{ account string }
	msgSpinTick struct{}
)

// Update routes messages to per-mode key handlers and per-message
// handlers, defined in update_search.go, update_details.go,
// update_tail.go, update_switcher.go, and update_messages.go.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Keep the tail-logs viewport sized to the available body area.
		vpHeight := m.height - 7 // input + 2 dividers + status + header + help + margin
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.tailViewport.Width = m.width
		m.tailViewport.Height = vpHeight
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		if m.inFlight && msg.String() != "ctrl+c" {
			// Block every other action while an async action is running;
			// Ctrl+C always aborts the program regardless.
			return m, nil
		}
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		case modeTailLogs:
			return m.updateTail(msg)
		case modeSwitcher:
			return m.updateSwitcher(msg)
		case modeOnboarding:
			return m.updateOnboarding(msg)
		default:
			return m.updateSearch(msg)
		}

	case msgTailStarted:
		return m.handleTailStarted(msg)
	case msgTailEvent:
		return m.handleTailEvent(msg)
	case msgAccount:
		m.account = msg.account
		return m, nil
	case msgSwitcherCommitted:
		return m.handleSwitcherCommitted(msg)
	case msgSpinTick:
		return m.handleSpinTick()
	case msgEffectDone:
		return m.handleEffectDone(msg)
	}

	return m, nil
}

// handleMouse routes clicks inside modeDetails to the copy-on-click
// hit-map. Other modes and non-left-press events are ignored.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeDetails {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.detailsHitMap == nil {
		return m, nil
	}
	for _, rg := range *m.detailsHitMap {
		if msg.X >= rg.X0 && msg.X < rg.X1 && msg.Y >= rg.Y0 && msg.Y < rg.Y1 {
			if err := copyToClipboard(rg.Clipboard); err != nil {
				m.toast = newErrorToast("copy failed: " + err.Error())
			} else {
				m.toast = newSuccessToast("copied " + rg.Label)
			}
			return m, nil
		}
	}
	return m, nil
}

// spinTickCmd schedules the next spinner frame.
func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}
