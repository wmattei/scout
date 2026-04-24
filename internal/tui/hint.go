package tui

import (
	"github.com/wmattei/scout/internal/core"
)

// favoriteHintFor returns the dim one-line hint rendered above the
// status bar, or "" if no hint should show. The hint only appears
// when a module row is in focus (search selection, home selection, or
// details view) and the prefs state is available to query.
//
// Width is the frame width so the caller can left-pad / style
// uniformly. Returns the raw text without styling; caller wraps it.
func favoriteHintFor(m Model) string {
	if m.prefsState == nil {
		return ""
	}
	r, ok := focusedRow(m)
	if !ok {
		return ""
	}
	if m.prefsState.IsFavorite(r.PackageID, r.Key) {
		return "[f] unfavorite"
	}
	return "[f] favorite"
}

// focusedRow returns the module row currently in focus per the active
// mode:
//   - modeDetails → m.detailsRow (or m.virtualRow if a synthetic
//     drill-in view is active).
//   - modeSearch  → the selected row from visibleSearchResults, if
//     any.
//
// Any other mode returns (zero, false).
func focusedRow(m Model) (core.Row, bool) {
	switch m.mode {
	case modeDetails:
		if m.virtualRow != nil {
			return *m.virtualRow, true
		}
		if m.detailsRow != nil {
			return *m.detailsRow, true
		}
		return core.Row{}, false
	case modeSearch:
		visible := m.visibleSearchResults()
		if len(visible) == 0 {
			return core.Row{}, false
		}
		if m.selected < 0 || m.selected >= len(visible) {
			return core.Row{}, false
		}
		return visible[m.selected].Row, true
	}
	return core.Row{}, false
}
