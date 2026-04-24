package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/core"
)

// updateDetails handles key events while in modeDetails.
func (m Model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Generic confirmation gate for destructive actions. When a callback
	// is pending, 'y' fires it; any other key cancels.
	if m.pendingConfirmFn != nil {
		if msg.String() == "y" {
			fn := m.pendingConfirmFn
			m.pendingConfirmFn = nil
			return fn(m)
		}
		m.pendingConfirmFn = nil
		m.toast = newToast("cancelled", 2*time.Second)
		return m, nil
	}

	// Module path intercepts navigation + Enter when m.detailsRow is
	// the active selection. Events-zone activation + favorite toggle
	// are wired in later Cutover tasks (11, 12).
	if m.detailsRow != nil {
		if out, handled := m.handleModuleDetailsKey(msg); handled {
			return out.model, out.cmd
		}
	}

	actions := ActionsFor(m.detailsResource.Type)
	events := selectableEventRows(m)
	hasSelectableEvents := len(events) > 0
	switch msg.String() {
	case "ctrl+p":
		m.switcher = newSwitcher(m.awsCtx.Profile, m.awsCtx.Region)
		m.switcher.Show()
		m.prevMode = modeDetails
		m.mode = modeSwitcher
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Esc on a virtual-row Details page (Automation execution
		// pop-up) pops back to the owning module Details view instead
		// of returning to search.
		if m.virtualRow != nil {
			m.virtualRow = nil
			m.actionSel = 0
			m.detailsFocus = detailsFocusActions
			m.eventSel = 0
			return m, nil
		}
		m.mode = modeSearch
		m.actionSel = 0
		m.detailsFocus = detailsFocusActions
		m.eventSel = 0
		m.detailsRow = nil
		return m, nil
	case "tab":
		// Tab cycles focus between Actions and Events, but only when
		// the Events zone has selectable rows.
		if !hasSelectableEvents {
			return m, nil
		}
		if m.detailsFocus == detailsFocusActions {
			m.detailsFocus = detailsFocusEvents
			if m.eventSel >= len(events) {
				m.eventSel = 0
			}
		} else {
			m.detailsFocus = detailsFocusActions
		}
		return m, nil
	case "up":
		if m.detailsFocus == detailsFocusEvents && hasSelectableEvents {
			if m.eventSel > 0 {
				m.eventSel--
			}
			return m, nil
		}
		if m.actionSel > 0 {
			m.actionSel--
		}
		return m, nil
	case "down":
		if m.detailsFocus == detailsFocusEvents && hasSelectableEvents {
			if m.eventSel < len(events)-1 {
				m.eventSel++
			}
			return m, nil
		}
		if m.actionSel < len(actions)-1 {
			m.actionSel++
		}
		return m, nil
	case "enter":
		if m.detailsFocus == detailsFocusEvents && hasSelectableEvents {
			if m.eventSel >= len(events) {
				return m, nil
			}
			row := events[m.eventSel]
			activator, ok := eventActivationRegistry[m.detailsResource.Type]
			if !ok {
				m.toast = newToast("no activation handler for this resource type", 2*time.Second)
				return m, nil
			}
			return activator(m, row.ActivationID)
		}
		return m.runAction(actions, m.actionSel)
	case "f":
		_, toast := m.toggleFavoriteForResource(m.detailsResource)
		m.toast = toast
		return m, nil
	}
	// Number hotkeys 1..9 for direct selection + execution.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(actions) {
				m.actionSel = idx
				return m.runAction(actions, idx)
			}
		}
	}
	return m, nil
}

// moduleKeyResult bundles the return pair from handleModuleDetailsKey
// so the caller can branch on `handled` without nil-checking pointers.
type moduleKeyResult struct {
	model tea.Model
	cmd   tea.Cmd
}

// currentModuleEventActivations returns the activation IDs published
// by the most recent renderModuleDetails call. Returns nil when the
// published slice is missing or the Events zone has no rows.
func (m Model) currentModuleEventActivations() []string {
	if m.moduleEventActivations == nil {
		return nil
	}
	return *m.moduleEventActivations
}

// handleModuleDetailsKey handles navigation + Enter for a module-owned
// Details view. Returns handled=false for keys the legacy path still
// owns (ctrl+p, ctrl+c, esc, number hotkeys), so the caller continues
// its own switch.
func (m Model) handleModuleDetailsKey(msg tea.KeyMsg) (moduleKeyResult, bool) {
	mod, ok := m.moduleForID(m.detailsRow.PackageID)
	if !ok {
		return moduleKeyResult{m, nil}, false
	}
	actions := mod.Actions(*m.detailsRow)
	switch msg.String() {
	case "up":
		if m.detailsFocus == detailsFocusEvents {
			if m.eventSel > 0 {
				m.eventSel--
			}
			return moduleKeyResult{m, nil}, true
		}
		if m.actionSel > 0 {
			m.actionSel--
		}
		return moduleKeyResult{m, nil}, true
	case "down":
		if m.detailsFocus == detailsFocusEvents {
			if m.eventSel < len(m.currentModuleEventActivations())-1 {
				m.eventSel++
			}
			return moduleKeyResult{m, nil}, true
		}
		if m.actionSel < len(actions)-1 {
			m.actionSel++
		}
		return moduleKeyResult{m, nil}, true
	case "enter":
		if m.detailsFocus == detailsFocusEvents {
			ids := m.currentModuleEventActivations()
			if m.eventSel >= 0 && m.eventSel < len(ids) && ids[m.eventSel] != "" {
				ctx := m.moduleContextFor(m.detailsRow.PackageID)
				eff := mod.HandleEvent(ctx, *m.detailsRow, ids[m.eventSel])
				nm, cmd := ApplyEffect(m, eff)
				return moduleKeyResult{nm, cmd}, true
			}
			return moduleKeyResult{m, nil}, true
		}
		if m.actionSel < 0 || m.actionSel >= len(actions) {
			return moduleKeyResult{m, nil}, true
		}
		ctx := m.moduleContextFor(m.detailsRow.PackageID)
		eff := actions[m.actionSel].Run(ctx, *m.detailsRow)
		nm, cmd := ApplyEffect(m, eff)
		return moduleKeyResult{nm, cmd}, true
	case "tab":
		ids := m.currentModuleEventActivations()
		if len(ids) == 0 {
			return moduleKeyResult{m, nil}, true
		}
		if m.detailsFocus == detailsFocusActions {
			m.detailsFocus = detailsFocusEvents
			if m.eventSel >= len(ids) {
				m.eventSel = 0
			}
		} else {
			m.detailsFocus = detailsFocusActions
		}
		return moduleKeyResult{m, nil}, true
	case "f":
		if m.prefs != nil {
			res := resourceFromRow(*m.detailsRow)
			if m.prefsState != nil && m.prefsState.IsFavorite(res.Type, res.Key) {
				_ = m.prefs.UnsetFavorite(m.prefsState, res.Type, res.Key)
				m.toast = newToast("unfavorited", 2*time.Second)
			} else {
				_ = m.prefs.SetFavorite(m.prefsState, res)
				m.toast = newToast("favorited", 2*time.Second)
			}
		}
		return moduleKeyResult{m, nil}, true
	}
	// Number hotkeys 1..9 for direct action selection + execution.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(actions) {
				m.actionSel = idx
				ctx := m.moduleContextFor(m.detailsRow.PackageID)
				eff := actions[idx].Run(ctx, *m.detailsRow)
				nm, cmd := ApplyEffect(m, eff)
				return moduleKeyResult{nm, cmd}, true
			}
		}
	}
	return moduleKeyResult{m, nil}, false
}

// runAction dispatches the selected action via its Execute closure. If
// Execute is nil (not yet implemented), it surfaces a toast to the user.
func (m Model) runAction(actions []Action, idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(actions) {
		return m, nil
	}
	a := actions[idx]
	if a.Execute == nil {
		m.toast = newToast("not yet implemented", 3*time.Second)
		return m, nil
	}
	return a.Execute(m)
}

// toggleFavoriteForResource flips favorite state on the given resource,
// persists the change, and returns the matching toast. Returns true when
// the resource was favorited, false when unfavorited.
func (m *Model) toggleFavoriteForResource(r core.Resource) (favorited bool, toast Toast) {
	if m.prefs == nil || m.prefsState == nil {
		return false, newErrorToast("favorites unavailable")
	}
	if m.prefsState.IsFavorite(r.Type, r.Key) {
		if err := m.prefs.UnsetFavorite(m.prefsState, r.Type, r.Key); err != nil {
			return false, newErrorToast("unfavorite failed: " + err.Error())
		}
		return false, newSuccessToast("unfavorited " + r.DisplayName)
	}
	if err := m.prefs.SetFavorite(m.prefsState, r); err != nil {
		return false, newErrorToast("favorite failed: " + err.Error())
	}
	return true, newSuccessToast("★ favorited " + r.DisplayName)
}
