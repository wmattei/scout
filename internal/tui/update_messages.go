package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/effect"
)

// Async-message handlers. One method per tea.Msg variant emitted by
// commands.go / tail.go / effects.go. Each returns a (tea.Model,
// tea.Cmd) the same way Update does, so the main dispatch is just
// `case msgX: return m.handleX(msg)`.

func (m Model) handleTailStarted(msg msgTailStarted) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.inFlight = false
		m.inFlightLabel = ""
		m.mode = modeSearch
		m.toast = newErrorToast("tail start failed: " + msg.err.Error())
		return m, nil
	}
	m.tailStream = msg.stream
	m.inFlight = false
	m.inFlightLabel = ""
	// Seed the viewport with historical lines so the user sees recent
	// context immediately. A visual divider separates history from the
	// incoming live stream.
	if len(msg.historicalLines) > 0 {
		m.tailLines = append(m.tailLines, msg.historicalLines...)
		m.tailLines = append(m.tailLines, strings.Repeat("─", 40)+" live ─▶")
	}
	m.rebuildTailViewport()
	m.tailViewport.GotoBottom()
	m.toast = newToast("tailing "+m.tailGroup, 2*time.Second)
	return m, tailLogsNextCmd(msg.stream)
}

func (m Model) handleTailEvent(msg msgTailEvent) (tea.Model, tea.Cmd) {
	if msg.eof {
		m.tailStream = nil
		if msg.err != nil {
			m.toast = newErrorToast("tail ended: " + msg.err.Error())
		}
		return m, nil
	}
	line := formatTailLine(msg.ev)
	m.tailLines = append(m.tailLines, line)
	if len(m.tailLines) > 2000 {
		// Soft cap on scrollback to keep memory bounded.
		m.tailLines = m.tailLines[len(m.tailLines)-2000:]
	}
	// Only auto-scroll to the bottom if the user hasn't scrolled up to
	// read history.
	wasAtBottom := m.tailViewport.AtBottom()
	m.rebuildTailViewport()
	if wasAtBottom {
		m.tailViewport.GotoBottom()
	}
	if m.tailStream != nil {
		return m, tailLogsNextCmd(m.tailStream)
	}
	return m, nil
}

func (m Model) handleSwitcherCommitted(msg msgSwitcherCommitted) (tea.Model, tea.Cmd) {
	m.inFlight = false
	m.inFlightLabel = ""
	if msg.err != nil {
		m.toast = newErrorToast("switch failed: " + msg.err.Error())
		return m, nil
	}
	// Close the old DB handles — we're done with them.
	if m.db != nil {
		_ = m.db.Close()
	}
	if m.prefs != nil {
		_ = m.prefs.Close()
	}
	m.awsCtx = msg.ctx
	m.db = msg.db
	m.memory = msg.memory
	m.prefs = msg.prefs
	m.prefsState = msg.prefsState
	// The new context needs its own activity middleware.
	m.activity.Attach(&m.awsCtx.Cfg)
	// Reset search state so the user lands on a clean frame.
	m.input.SetValue("")
	m.results = nil
	m.scopedResults = nil
	m.scopedQuery = ""
	m.selected = 0
	m.lazyDetails = make(map[lazyDetailKey]map[string]string)
	m.lazyDetailsState = make(map[lazyDetailKey]lazyDetailState)
	m.serviceScopeFetched = make(map[string]struct{})
	m.account = ""
	// Reset module-era state + reopen the shared cache for the new
	// context. Clearing these before reopen means the very first
	// HandleSearch under the new profile sees an empty state (fires
	// the live Async) and an empty lazy (renders "resolving…").
	m.moduleState = make(map[string]effect.State)
	m.moduleLazy = make(map[lazyDetailKey]map[string]string)
	m.reopenModuleCache(context.Background(), m.awsCtx.Profile, m.awsCtx.Region)
	m.switcher.Hide()
	m.mode = modeSearch
	if msg.prefsErr != nil {
		m.toast = newErrorToast("prefs unavailable for " + m.awsCtx.Profile + "/" + m.awsCtx.Region + ": " + msg.prefsErr.Error())
	} else {
		m.toast = newToast(fmt.Sprintf("context: %s / %s", m.awsCtx.Profile, m.awsCtx.Region), 3*time.Second)
	}
	// Re-resolve caller identity for the new profile.
	return m, resolveAccountCmd(m.awsCtx)
}

func (m Model) handleSpinTick() (tea.Model, tea.Cmd) {
	m.spinTick++
	// Clear an expired toast so the view falls back to the normal
	// status bar. Only the spinner ticker is reliably called often
	// enough to do this without a dedicated timer.
	if !m.toast.isActive() {
		m.toast = Toast{}
	}
	return m, spinTickCmd()
}
