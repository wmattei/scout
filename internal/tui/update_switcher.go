package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// updateSwitcher handles key events while the profile/region overlay is
// open. Esc hides the overlay and restores the previous mode; Enter
// commits the selection and triggers a context swap via
// commitSwitcherCmd; Tab flips focused panes; ↑/↓ move the selection;
// printable keys append to the focused pane's filter; Backspace trims
// one rune from the focused filter.
func (m Model) updateSwitcher(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.switcher.Hide()
		m.mode = m.prevMode
		return m, nil
	case "tab":
		if m.switcher.focused == switcherPaneProfile {
			m.switcher.focused = switcherPaneRegion
		} else {
			m.switcher.focused = switcherPaneProfile
		}
		return m, nil
	case "up":
		if m.switcher.focused == switcherPaneProfile && m.switcher.profileSel > 0 {
			m.switcher.profileSel--
		}
		if m.switcher.focused == switcherPaneRegion && m.switcher.regionSel > 0 {
			m.switcher.regionSel--
		}
		return m, nil
	case "down":
		if m.switcher.focused == switcherPaneProfile {
			vals, _ := m.switcher.filteredProfiles()
			if m.switcher.profileSel < len(vals)-1 {
				m.switcher.profileSel++
			}
		}
		if m.switcher.focused == switcherPaneRegion {
			vals, _ := m.switcher.filteredRegions()
			if m.switcher.regionSel < len(vals)-1 {
				m.switcher.regionSel++
			}
		}
		return m, nil
	case "enter":
		profile := m.switcher.selectedProfile()
		region := m.switcher.selectedRegion()
		if profile == "" || region == "" {
			m.toast = newErrorToast("switcher: nothing selected")
			return m, nil
		}
		if profile == m.awsCtx.Profile && region == m.awsCtx.Region {
			m.switcher.Hide()
			m.mode = m.prevMode
			return m, nil
		}
		m.inFlight = true
		m.inFlightLabel = "switching context…"
		return m, commitSwitcherCmd(profile, region)
	case "backspace":
		if m.switcher.focused == switcherPaneProfile && len(m.switcher.profileFilter) > 0 {
			r := []rune(m.switcher.profileFilter)
			m.switcher.profileFilter = string(r[:len(r)-1])
			m.switcher.profileSel = 0
		}
		if m.switcher.focused == switcherPaneRegion && len(m.switcher.regionFilter) > 0 {
			r := []rune(m.switcher.regionFilter)
			m.switcher.regionFilter = string(r[:len(r)-1])
			m.switcher.regionSel = 0
		}
		return m, nil
	}
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= 32 {
			if m.switcher.focused == switcherPaneProfile {
				m.switcher.profileFilter += string(r)
				m.switcher.profileSel = 0
			} else {
				m.switcher.regionFilter += string(r)
				m.switcher.regionSel = 0
			}
		}
	}
	return m, nil
}
