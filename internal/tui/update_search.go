package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/search"
)

// updateSearch handles key events while in modeSearch.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down":
		visible := m.visibleSearchResults()
		if m.selected < len(visible)-1 {
			m.selected++
		}
		return m, nil
	case "enter":
		visible := m.visibleSearchResults()
		if len(visible) == 0 || m.selected < 0 || m.selected >= len(visible) {
			return m, nil
		}
		picked := visible[m.selected]
		if picked.ModuleRow != nil {
			return m.enterModuleDetails(*picked.ModuleRow)
		}
		return m, nil
	case "tab":
		return m.handleTab()
	case "ctrl+p":
		m.switcher = newSwitcher(m.awsCtx.Profile, m.awsCtx.Region)
		m.switcher.Show()
		m.prevMode = modeSearch
		m.mode = modeSwitcher
		return m, nil
	case "alt+backspace", "ctrl+w":
		// Option+Backspace on macOS (and Ctrl+W elsewhere) deletes the
		// last path segment instead of the whole word. The default
		// textinput behaviour is word-aware by spaces, which is useless
		// for S3 breadcrumbs — we split on "/" instead.
		m.input.SetValue(deleteLastPathSegment(m.input.Value()))
		m.input.CursorEnd()
		return m.recomputeResults(nil)
	case "ctrl+r", "esc":
		return m, nil
	case "f":
		visible := m.visibleSearchResults()
		if len(visible) == 0 || m.selected < 0 || m.selected >= len(visible) {
			return m, nil
		}
		_, toast := m.toggleFavoriteForResource(visible[m.selected].Resource)
		m.toast = toast
		return m, nil
	}

	// Let the textinput consume the keystroke, then recompute.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m.recomputeResults(cmd)
}

// handleTab implements Tab drill-in. When a bucket or folder row is
// selected, Tab replaces the input value with that row's full path and
// appends a trailing `/` so the scope advances on the next recompute.
// For leaf rows Tab replaces with the row's name without a trailing
// separator.
func (m Model) handleTab() (tea.Model, tea.Cmd) {
	visible := m.visibleSearchResults()
	if len(visible) == 0 || m.selected < 0 || m.selected >= len(visible) {
		return m, nil
	}
	row := visible[m.selected]
	if row.ModuleRow != nil {
		m.input.SetValue(row.ModuleRow.Name)
		m.input.CursorEnd()
		return m.recomputeResults(nil)
	}
	m.input.SetValue(row.Resource.DisplayName)
	m.input.CursorEnd()
	return m.recomputeResults(nil)
}

// recomputeResults recomputes the result list based on the current input
// and returns the combined tea.Cmd for text-input update and any
// follow-up scoped-search command.
func (m Model) recomputeResults(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	// Module path: when the input is "<alias>:<query>" and alias is
	// owned by a module, dispatch to module.HandleSearch and short-
	// circuit the top-level fuzzy below.
	if alias, rest, ok := m.scopeFromInput(m.input.Value()); ok {
		return m.dispatchModuleScope(alias, rest, cmd)
	}

	// Top-level fuzzy search: blend module cache rows.
	modRows := m.computeModuleResults(m.input.Value())
	m.results = partitionByFavorites(modRows, m.prefsState)
	m.scopedResults = nil
	m.scopedQuery = ""
	m.clampSelected()
	return m, cmd
}

// deleteLastPathSegment trims the trailing segment of a breadcrumb input,
// treating "/" as the segment delimiter. Used by Option+Backspace and
// Ctrl+W so the user can walk back up the S3 path one level at a time.
//
// Examples:
//
//	"bucket/logs/2026/01/"    -> "bucket/logs/2026/"
//	"bucket/logs/2026/01/fil" -> "bucket/logs/2026/01/"
//	"bucket/"                 -> ""
//	"bucket"                  -> ""
//	""                        -> ""
func deleteLastPathSegment(input string) string {
	s := strings.TrimSuffix(input, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// isLoadingScoped reports whether an S3 drill-in search is in flight.
// Service-scope mode is explicitly excluded — its loading affordance
// is the status-bar spinner.
func (m Model) isLoadingScoped() bool {
	scope := search.ParseScope(m.input.Value())
	return scope.Bucket != "" && m.scopedQuery != m.input.Value()
}

// visibleSearchResults returns whichever result list is currently active
// so arrow keys and Enter operate on the same set the user is seeing.
//
// Selection priorities:
//  1. Scoped-mode results (module-scope HandleSearch output).
//  2. Empty input + at least one favorite or recent → home rows.
//  3. Otherwise → m.results.
func (m Model) visibleSearchResults() []search.Result {
	if len(m.scopedResults) > 0 {
		return m.scopedResults
	}
	if homeActive(m) {
		return homeRows(m)
	}
	return m.results
}

// clampSelected keeps the selected index within the visible list bounds.
func (m *Model) clampSelected() {
	n := len(m.visibleSearchResults())
	if n == 0 {
		m.selected = 0
		return
	}
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// enterModuleDetails transitions into modeDetails for a module-owned
// row. Fires module.ResolveDetails as an Effect unless moduleLazy
// already has an entry and the module doesn't declare AlwaysRefresh.
func (m Model) enterModuleDetails(r core.Row) (tea.Model, tea.Cmd) {
	mod, ok := m.moduleForID(r.PackageID)
	if !ok {
		return m, nil
	}
	m.detailsRow = &r
	m.detailsResource = core.Resource{}
	m.actionSel = 0
	m.mode = modeDetails
	if m.prefs != nil {
		_ = m.prefs.MarkVisited(m.prefsState, resourceFromRow(r))
	}

	key := moduleDetailKey(r.PackageID, r.Key)
	_, haveLazy := m.moduleLazy[key]
	if haveLazy && !mod.AlwaysRefresh() {
		return m, nil
	}
	if mod.AlwaysRefresh() {
		delete(m.moduleLazy, key)
	}
	ctx := m.moduleContextFor(r.PackageID)
	eff := mod.ResolveDetails(ctx, r)
	nm, cmd := ApplyEffect(m, eff)
	return nm, cmd
}

// dispatchModuleScope invokes the module's HandleSearch, updates
// moduleState with the returned State, and reduces the returned
// effects through ApplyEffect (accumulating their tea.Cmds).
func (m Model) dispatchModuleScope(alias, rest string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	mod, ok := m.moduleForAlias(alias)
	if !ok {
		return m, cmd
	}
	id := mod.Manifest().ID
	ctxMod := m.moduleContextFor(id)
	state := m.moduleState[id]
	rows, newState, effects := mod.HandleSearch(ctxMod, rest, state)
	m.moduleState[id] = newState
	m.scopedResults = moduleRowsToResults(rows)
	m.scopedQuery = rest
	m.results = nil
	m.clampSelected()

	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	for _, eff := range effects {
		newM, c := ApplyEffect(m, eff)
		m = newM
		if c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

// moduleRowsToResults wraps a slice of module Rows in search.Result
// records (ModuleRow populated, Resource zero).
func moduleRowsToResults(rows []core.Row) []search.Result {
	out := make([]search.Result, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, search.Result{ModuleRow: &r})
	}
	return out
}

// computeModuleResults fuzz-matches the query against every row
// cached by the modules. Returns nil when no module cache is open or
// when the query is empty.
func (m Model) computeModuleResults(query string) []search.Result {
	if m.moduleCache == nil || query == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	rows, err := m.moduleCache.AllRows(ctx)
	if err != nil {
		return nil
	}
	return search.FuzzyOverRows(query, rows, MaxDisplayedResults)
}
