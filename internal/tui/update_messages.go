package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/search"
	"github.com/wmattei/scout/internal/services"
)

// Async-message handlers. One method per tea.Msg variant emitted by
// commands.go / tail.go / editor.go / commands.go. Each returns a
// (tea.Model, tea.Cmd) the same way Update does, so the main dispatch
// is just `case msgX: return m.handleX(msg)`.

func (m Model) handleResourcesUpdated(msg msgResourcesUpdated) (tea.Model, tea.Cmd) {
	// The refresh (top-level or service-scope) wrote new data into
	// m.memory. Recompute the current result list against the updated
	// snapshot — respecting the active scope so a service-scoped
	// session fuzzy-matches only its own type.
	scope := search.ParseScope(m.input.Value())
	if scope.HasService {
		m.results = partitionByFavorites(
			search.Fuzzy(scope.ServiceQuery, m.memory.ByType(scope.Service), MaxDisplayedResults),
			m.prefsState,
		)
	} else {
		m.results = partitionByFavorites(computeResults(m.input.Value(), m.memory), m.prefsState)
	}
	m.clampSelected()
	if len(msg.errors) > 0 {
		m.toast = newErrorToast(summarizeErrors(msg.errors))
	}
	return m, nil
}

func (m Model) handleScopedResults(msg msgScopedResults) (tea.Model, tea.Cmd) {
	// Drop the message if the input has moved on since the command was
	// issued. Prevents stale ListObjectsV2 responses from clobbering
	// results for a query the user has already typed past.
	if msg.query != m.input.Value() {
		return m, nil
	}
	m.scopedResults = msg.results
	m.scopedQuery = msg.query
	m.clampSelected()
	if msg.err != "" {
		m.toast = newErrorToast(msg.err)
	}
	return m, nil
}

func (m Model) handleActionDone(msg msgActionDone) (tea.Model, tea.Cmd) {
	m.inFlight = false
	m.inFlightLabel = ""
	if msg.err != nil {
		m.toast = newErrorToast(msg.toast)
		// Persist the error into the current resource's lazy data so
		// the Details render surfaces it prominently — toasts are
		// transient; this keeps the reason visible until the next
		// successful action or refetch.
		if m.mode == modeDetails {
			key := lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}
			if m.lazyDetails[key] == nil {
				m.lazyDetails[key] = map[string]string{}
			}
			m.lazyDetails[key]["actionError"] = msg.toast
		}
	} else if msg.success {
		m.toast = newSuccessToast(msg.toast)
		// Clear any prior action error on success.
		if m.mode == modeDetails {
			key := lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}
			if m.lazyDetails[key] != nil {
				delete(m.lazyDetails[key], "actionError")
			}
		}
	} else {
		m.toast = newToast(msg.toast, 4*time.Second)
	}
	// If the action requests a details refetch, invalidate the lazy
	// cache and re-fire ResolveDetails so the panel shows fresh data.
	if msg.refetchDetails && m.mode == modeDetails {
		key := lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}
		delete(m.lazyDetails, key)
		m.lazyDetailsState[key] = lazyStateInFlight
		if p, ok := services.Get(m.detailsResource.Type); ok {
			return m, resolveLazyDetailsCmd(m.awsCtx, p, m.detailsResource)
		}
	}
	return m, nil
}

func (m Model) handleEditorClosed(msg msgEditorClosed) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.toast = newErrorToast("editor: " + msg.Err.Error())
		m.pendingEditorAction = editorActionNone
		return m, nil
	}
	// Check whether the user actually saved. If the file's mtime is
	// unchanged the user quit without saving (`:q!` in vim); skip the
	// follow-up action entirely.
	if info, err := os.Stat(m.pendingEditorPath); err == nil {
		if !info.ModTime().After(msg.MtimePre) {
			_ = os.Remove(m.pendingEditorPath)
			m.toast = newToast("editor closed without saving — cancelled", 3*time.Second)
			m.pendingEditorAction = editorActionNone
			return m, nil
		}
	}
	content, err := os.ReadFile(m.pendingEditorPath)
	_ = os.Remove(m.pendingEditorPath) // clean up regardless
	if err != nil {
		m.toast = newErrorToast("read editor output: " + err.Error())
		m.pendingEditorAction = editorActionNone
		return m, nil
	}
	switch m.pendingEditorAction {
	case editorActionLambdaInvoke:
		if !json.Valid(content) {
			m.toast = newErrorToast("invalid JSON payload — invoke cancelled")
			m.pendingEditorAction = editorActionNone
			return m, nil
		}
		m.inFlight = true
		m.inFlightLabel = "invoking…"
		m.pendingEditorAction = editorActionNone
		return m, lambdaInvokeCmd(m.awsCtx, m.pendingEditorResource, content)
	case editorActionSSMUpdate:
		m.inFlight = true
		m.inFlightLabel = "updating…"
		m.pendingEditorAction = editorActionNone
		return m, ssmUpdateCmd(m.awsCtx, m.pendingEditorResource, content)
	case editorActionSecretUpdate:
		m.inFlight = true
		m.inFlightLabel = "updating…"
		m.pendingEditorAction = editorActionNone
		return m, secretUpdateCmd(m.awsCtx, m.pendingEditorResource, content)
	case editorActionAutomationRun:
		if !json.Valid(content) {
			m.toast = newErrorToast("invalid JSON payload — run cancelled")
			m.pendingEditorAction = editorActionNone
			return m, nil
		}
		m.inFlight = true
		m.inFlightLabel = "running…"
		m.pendingEditorAction = editorActionNone
		return m, automationRunCmd(m.awsCtx, m.pendingEditorResource, content)
	}
	m.pendingEditorAction = editorActionNone
	return m, nil
}

func (m Model) handleLazyDetailsResolved(msg msgLazyDetailsResolved) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.lazyDetailsState[msg.key] = lazyStateResolved
		m.toast = newErrorToast("resolve details failed: " + msg.err.Error())
		// Still schedule the next poll so the user sees fresh data once
		// a transient failure clears.
		return m, schedulePollIfNeeded(m, msg.key)
	}
	if m.lazyDetails == nil {
		m.lazyDetails = make(map[lazyDetailKey]map[string]string)
	}
	m.lazyDetails[msg.key] = msg.details
	m.lazyDetailsState[msg.key] = lazyStateResolved
	return m, schedulePollIfNeeded(m, msg.key)
}

func (m Model) handleAutomationStarted(msg msgAutomationStarted) (tea.Model, tea.Cmd) {
	// A fresh Run returned an execution ID — clear inFlight, show the
	// success toast, and jump into the execution details page.
	m.inFlight = false
	m.inFlightLabel = ""
	m.toast = newSuccessToast(fmt.Sprintf("execution started: %s", shortExecID(msg.execID)))
	m.detailsResource = msg.docResource
	key := lazyDetailKey{Type: msg.docResource.Type, Key: msg.docResource.Key}
	if m.lazyDetails[key] != nil {
		delete(m.lazyDetails[key], "actionError")
	}
	delete(m.lazyDetails, key)
	m.lazyDetailsState[key] = lazyStateInFlight
	nm, cmd := enterExecutionDetails(m, msg.execID)
	if p, ok := services.Get(msg.docResource.Type); ok {
		refetch := resolveLazyDetailsCmd(m.awsCtx, p, msg.docResource)
		return nm, tea.Batch(cmd, refetch)
	}
	return nm, cmd
}

func (m Model) handlePollDetails(msg msgPollDetails) (tea.Model, tea.Cmd) {
	// A scheduled poll tick fired. Re-fire ResolveDetails only if the
	// user is still looking at the same resource in the same mode.
	// Don't set lazyStateInFlight — the current data stays visible
	// until the fresh result silently overwrites it.
	if m.mode != modeDetails {
		return m, nil
	}
	if msg.key.Type != m.detailsResource.Type || msg.key.Key != m.detailsResource.Key {
		return m, nil
	}
	if p, ok := services.Get(m.detailsResource.Type); ok {
		return m, resolveLazyDetailsCmd(m.awsCtx, p, m.detailsResource)
	}
	return m, nil
}

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

// summarizeErrors turns a slice of subtask error strings into a single
// toast message.
func summarizeErrors(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) == 1 {
		return "refresh failed: " + errs[0]
	}
	return fmt.Sprintf("%d subtasks failed: %s", len(errs), errs[0])
}
