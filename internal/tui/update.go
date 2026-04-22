package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/search"
)

// Custom messages emitted by commands.
type (
	msgResourcesUpdated struct {
		errors []string // one string per failed subtask, empty on full success
	}
	msgAccount     struct{ account string }
	msgSpinTick    struct{}
	msgPollDetails struct{ key lazyDetailKey }

	// msgScopedResults carries the merged cache+live result set for a
	// scoped (bucket/prefix) search. `query` is the exact input value
	// that produced these results — the handler drops the message if
	// the input has moved on since, so stale results can't clobber
	// fresher ones. `err` is set when the live fetch failed.
	msgScopedResults struct {
		query   string
		results []search.Result
		err     string
	}
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
		resizeExecutionViewport(&m)
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
		case modeExecutionDetails:
			return updateExecutionDetails(m, msg)
		case modeOnboarding:
			return m.updateOnboarding(msg)
		default:
			return m.updateSearch(msg)
		}

	case msgResourcesUpdated:
		return m.handleResourcesUpdated(msg)
	case msgScopedResults:
		return m.handleScopedResults(msg)
	case msgActionDone:
		return m.handleActionDone(msg)
	case msgEditorClosed:
		return m.handleEditorClosed(msg)
	case msgLazyDetailsResolved:
		return m.handleLazyDetailsResolved(msg)
	case msgAutomationStarted:
		return m.handleAutomationStarted(msg)
	case msgExecutionFetched:
		return handleExecutionFetched(m, msg)
	case msgExecutionStepLogs:
		return handleExecutionStepLogs(m, msg)
	case msgExecutionPollTick:
		return handleExecutionPollTick(m, msg)
	case msgPollDetails:
		return m.handlePollDetails(msg)
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
