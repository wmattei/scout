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
		m.mode = modeSearch
		m.actionSel = 0
		m.detailsFocus = detailsFocusActions
		m.eventSel = 0
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
