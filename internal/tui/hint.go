package tui

import (
	"github.com/wmattei/scout/internal/core"
)

// favoriteHintFor returns the dim one-line hint rendered above the
// status bar, or "" if no hint should show. The hint only appears
// when a resource is in focus (search selection, home selection, or
// details view) and the prefs state is available to query.
//
// Width is the frame width so the caller can left-pad / style
// uniformly. Returns the raw text without styling; caller wraps it.
func favoriteHintFor(m Model) string {
	if m.prefsState == nil {
		return ""
	}
	r, ok := focusedResource(m)
	if !ok {
		return ""
	}
	if m.prefsState.IsFavorite(r.Type, r.Key) {
		return "[f] unfavorite"
	}
	return "[f] favorite"
}

// focusedResource returns the resource currently in focus per the
// active mode:
//   - modeDetails → m.detailsResource (always set when mode is
//     details).
//   - modeSearch  → the selected row from visibleSearchResults, if
//     any.
// Any other mode returns (zero, false).
func focusedResource(m Model) (core.Resource, bool) {
	switch m.mode {
	case modeDetails:
		return m.detailsResource, true
	case modeSearch:
		visible := m.visibleSearchResults()
		if len(visible) == 0 {
			return core.Resource{}, false
		}
		if m.selected < 0 || m.selected >= len(visible) {
			return core.Resource{}, false
		}
		return visible[m.selected].Resource, true
	}
	return core.Resource{}, false
}
