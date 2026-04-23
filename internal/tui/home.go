package tui

import (
	"strings"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
)

// homeSections represents the data source for the empty-input home
// page: favorites first, then recents. The TUI builds a flat slice of
// search.Result (each wrapping a core.Resource with no fuzzy
// highlights) plus two index offsets that tell the renderer where the
// Recents section begins.
type homeSections struct {
	rows          []search.Result // favorites followed by recents
	favoritesLen  int             // number of rows belonging to Favorites
	recentsLen    int             // number of rows belonging to Recents
}

// buildHomeSections assembles the flat row list from the current
// prefs state. Uses the in-memory resource index to fetch up-to-date
// resource records where possible; falls back to a snapshotted name
// when the resource isn't in the cache (pre-preload, or post-
// deletion).
func buildHomeSections(m Model) homeSections {
	if m.prefsState == nil {
		return homeSections{}
	}

	favs := m.prefsState.Favorites()
	recents := m.prefsState.Recents()

	rows := make([]search.Result, 0, len(favs)+len(recents))
	for _, f := range favs {
		rows = append(rows, search.Result{Resource: resolveResource(m, f.Type, f.Key, f.Name)})
	}
	for _, r := range recents {
		rows = append(rows, search.Result{Resource: resolveResource(m, r.Type, r.Key, r.Name)})
	}

	return homeSections{
		rows:         rows,
		favoritesLen: len(favs),
		recentsLen:   len(recents),
	}
}

// resolveResource looks up a live Resource record in the in-memory
// cache. If the cache has a matching (type, key) entry, that full
// Resource (with Meta for the meta column) is returned. Otherwise a
// minimal stub is synthesized from the snapshotted name so the row
// still renders.
func resolveResource(m Model, t core.ResourceType, key, snapshotName string) core.Resource {
	// memory.ByType is O(n) in the number of resources of that type,
	// but n is bounded by cache size and this only runs on empty
	// input, so it's fine.
	if m.memory != nil {
		for _, r := range m.memory.ByType(t) {
			if r.Key == key {
				return r
			}
		}
	}
	return core.Resource{Type: t, Key: key, DisplayName: snapshotName}
}

// renderHome renders the Favorites + Recents two-section view used
// when the input bar is empty and at least one section is non-empty.
// Selection is a single flat index (m.selected) walking over
// sections.rows in order (favorites first). Section headers are
// inserted between the two groups; when either section is empty its
// header is suppressed entirely.
func renderHome(m Model, sections homeSections, width, height int) string {
	if len(sections.rows) == 0 {
		// No favorites and no recents — fall back to the default
		// empty-state renderer via centerEmptyState. Caller decided
		// to invoke renderHome already, so this is a safety net.
		return centerEmptyState(width, height, "no favorites or recents yet — press f on a resource to favorite it")
	}

	// Layout budget: two optional section headers (one line each)
	// plus the rows themselves. Available row capacity = height -
	// headersUsed.
	var headerFav, headerRec string
	if sections.favoritesLen > 0 {
		headerFav = styleDivider.Render("── FAVORITES ──")
	}
	if sections.recentsLen > 0 {
		headerRec = styleDivider.Render("── RECENT ──")
	}

	// Compose the body line-by-line.
	var b strings.Builder
	lines := 0

	writeLine := func(s string) {
		if lines > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
		lines++
	}

	// Budget: reserve one line per header that will be emitted.
	headersUsed := 0
	if headerFav != "" {
		headersUsed++
	}
	if headerRec != "" {
		headersUsed++
	}
	rowBudget := height - headersUsed
	if rowBudget < 0 {
		rowBudget = 0
	}

	// Distribute the row budget across the two sections in proportion
	// to their sizes, capped at their actual lengths so trailing empty
	// space fills from the bottom.
	favBudget := sections.favoritesLen
	recBudget := sections.recentsLen
	if favBudget+recBudget > rowBudget {
		// Prefer favorites; if they fit entirely, give remainder to
		// recents. If they don't, truncate recents first, then
		// favorites.
		if favBudget >= rowBudget {
			favBudget = rowBudget
			recBudget = 0
		} else {
			recBudget = rowBudget - favBudget
		}
	}

	// Render Favorites section. We pass the first `favBudget` rows
	// only and anchor the viewport at index 0 unless the cursor is
	// in the visible favorites window — otherwise renderResults
	// would scroll the slice to keep the cursor visible, which
	// pushes earlier favorites (including the newest one at index
	// 0) off-screen. The cursor can still live at a position beyond
	// the visible window; it just won't be highlighted there.
	if headerFav != "" && favBudget > 0 {
		writeLine(headerFav)
		favRows := sections.rows[:favBudget]
		favSel := m.selected
		if favSel < 0 || favSel >= favBudget {
			favSel = -1
		}
		writeLine(renderResultsRange(favRows, favSel, 0, favBudget, width, m.prefsState, m.registry))
		lines += favBudget - 1 // renderResultsRange emitted favBudget lines joined by \n; writeLine added 1 more
	}

	// Render Recents section. Same anchoring rule as Favorites:
	// slice to exactly `recBudget` rows and pass -1 when the cursor
	// is outside this window so no viewport scroll occurs.
	if headerRec != "" && recBudget > 0 {
		writeLine(headerRec)
		recStart := sections.favoritesLen
		recRows := sections.rows[recStart : recStart+recBudget]
		recSel := m.selected - sections.favoritesLen
		if recSel < 0 || recSel >= recBudget {
			recSel = -1
		}
		writeLine(renderResultsRange(recRows, recSel, 0, recBudget, width, m.prefsState, m.registry))
		lines += recBudget - 1
	}

	// Pad to full height.
	for ; lines < height; lines++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderResultsRange renders up to `height` rows from `results`
// starting at `start`, with `selected` as the currently highlighted
// row within the displayed window. This is a thin wrapper so the
// home page and the regular search body both share the same per-row
// formatting without renderResults' empty-state centering logic.
func renderResultsRange(results []search.Result, selected, start, height, width int, favs *prefs.State, registry *module.Registry) string {
	// Delegate to renderResults with the same args. renderResults
	// already handles windowing via its scroll-window calculation
	// when selected >= height, so passing height equal to the budget
	// and the full list works.
	_ = start // renderResults computes its own start from `selected`
	return strings.TrimRight(renderResults(results, selected, width, height, "", favs, registry), "\n")
}

// homeActive reports whether the TUI should render the home page
// right now (empty input + at least one entry in favorites or
// recents).
func homeActive(m Model) bool {
	if m.input.Value() != "" {
		return false
	}
	if m.prefsState == nil {
		return false
	}
	return len(m.prefsState.Favorites()) > 0 || len(m.prefsState.Recents()) > 0
}

// homeRows returns the flat row slice the selection cursor walks
// over. Used by updateSearch so the `j/k/Enter` handlers treat the
// home page identically to a regular result list.
func homeRows(m Model) []search.Result {
	if !homeActive(m) {
		return nil
	}
	return buildHomeSections(m).rows
}
