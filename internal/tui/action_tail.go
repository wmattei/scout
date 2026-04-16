package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/services"
)

// execTailLogs resolves the log group via the provider registry and
// switches the TUI into modeTailLogs. If resolution is still in flight
// (lazyStateInFlight) we show a retry toast. If the provider has no log
// group configured, an informational toast is shown.
func execTailLogs(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no log group configured on this resource", 4*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	// If lazy resolution hasn't landed yet, the user can either
	// wait for the in-flight resolveDetails command (which fires
	// on entering Details) or retry. We can't distinguish "still
	// resolving" from "no log group configured" without tracking
	// in-flight per resource, so the message is unified.
	group := p.LogGroup(r, lazy)
	if group == "" {
		// Detect "in-flight" via lazyDetailsState — see Task 14.
		if m.lazyDetailsState[lazyDetailKey{Type: r.Type, Key: r.Key}] == lazyStateInFlight {
			m.toast = newToast("details still resolving — try again", 2*time.Second)
			return m, nil
		}
		m.toast = newToast("no CloudWatch log group configured on this resource", 4*time.Second)
		return m, nil
	}

	m.mode = modeTailLogs
	m.tailGroup = group
	m.tailLines = nil
	m.tailFilter = ""
	m.tailFilterEditing = false
	m.tailViewport.SetContent("")
	m.tailViewport.GotoTop()
	m.inFlight = true
	m.inFlightLabel = "starting tail…"
	return m, tailLogsStartCmd(m.awsCtx, group, m.account)
}
