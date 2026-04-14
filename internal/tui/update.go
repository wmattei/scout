package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Custom messages emitted by commands.
type (
	msgResourcesUpdated struct{}
	msgAccount          struct{ account string }
	msgSpinTick         struct{}

	// msgScopedResults carries the merged cache+live result set for a
	// scoped (bucket/prefix) search. `query` is the exact input value
	// that produced these results — the handler drops the message if
	// the input has moved on since, so stale results can't clobber
	// fresher ones.
	msgScopedResults struct {
		query   string
		results []search.Result
	}
)

// Update routes messages to state mutations and side-effect commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		default:
			return m.updateSearch(msg)
		}

	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current top-level list against the updated snapshot.
		m.results = computeResults(m.input.Value(), m.memory)
		m.clampSelected()
		return m, nil

	case msgScopedResults:
		// Drop the message if the input has moved on since the command
		// was issued. This prevents stale ListObjectsV2 responses from
		// clobbering the results for a query the user has already typed
		// past.
		if msg.query != m.input.Value() {
			return m, nil
		}
		m.scopedResults = msg.results
		m.scopedQuery = msg.query
		m.clampSelected()
		return m, nil

	case msgAccount:
		m.account = msg.account
		return m, nil

	case msgSpinTick:
		m.spinTick++
		// Clear an expired toast so the view falls back to the normal
		// status bar. Only the spinner ticker is reliably called often
		// enough to do this without a dedicated timer.
		if !m.toast.isActive() {
			m.toast = Toast{}
		}
		return m, spinTickCmd()
	}

	return m, nil
}

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
		// Enter the Details view for the currently selected row.
		visible := m.visibleSearchResults()
		if len(visible) == 0 {
			return m, nil
		}
		if m.selected < 0 || m.selected >= len(visible) {
			return m, nil
		}
		m.detailsResource = visible[m.selected].Resource
		m.actionSel = 0
		m.mode = modeDetails
		return m, nil
	case "tab":
		return m.handleTab()
	case "ctrl+p", "ctrl+r", "esc":
		// Reserved for later phases. No-op in Phase 2.
		return m, nil
	}

	// Let the textinput consume the keystroke, then recompute.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m.recomputeResults(cmd)
}

// updateDetails handles key events while in modeDetails.
func (m Model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	actions := ActionsFor(m.detailsResource.Type)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeSearch
		m.actionSel = 0
		return m, nil
	case "up":
		if m.actionSel > 0 {
			m.actionSel--
		}
		return m, nil
	case "down":
		if m.actionSel < len(actions)-1 {
			m.actionSel++
		}
		return m, nil
	case "enter":
		if len(actions) == 0 {
			return m, nil
		}
		m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
		return m, nil
	}
	// Number hotkeys 1..9 for direct selection + execution.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(actions) {
				m.actionSel = idx
				m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
				return m, nil
			}
		}
	}
	return m, nil
}

// handleTab implements Tab drill-in. When a bucket or folder row is
// selected, Tab replaces the input value with that row's full path and
// appends a trailing `/` so the scope advances on the next recompute.
// For leaf rows (objects, ECS services, ECS task-def families) Tab
// replaces the input with the row's name without a trailing separator.
func (m Model) handleTab() (tea.Model, tea.Cmd) {
	visible := m.visibleSearchResults()
	if len(visible) == 0 {
		return m, nil
	}
	if m.selected < 0 || m.selected >= len(visible) {
		return m, nil
	}
	row := visible[m.selected].Resource

	scope := search.ParseScope(m.input.Value())
	var newInput string
	switch row.Type {
	case core.RTypeBucket:
		newInput = row.Key + "/"
	case core.RTypeFolder:
		// row.Key is the full relative key under the bucket
		// (e.g. "logs/2026/"). Reconstruct "bucket/logs/2026/".
		newInput = scope.Bucket + "/" + row.Key
	case core.RTypeObject:
		// Object keys don't get a trailing slash.
		newInput = scope.Bucket + "/" + row.Key
	default:
		// Top-level leaves (ECS service, ECS task-def family) — replace
		// the input with the display name so subsequent text matches the
		// current selection.
		newInput = row.DisplayName
	}
	m.input.SetValue(newInput)
	m.input.CursorEnd()
	return m.recomputeResults(nil)
}

// recomputeResults recomputes the result list based on the current input
// and returns the combined tea.Cmd for text-input update and any
// follow-up scoped-search command. `cmd` is the command already produced
// by the text-input update (or nil if none).
func (m Model) recomputeResults(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	scope := search.ParseScope(m.input.Value())

	if scope.IsTopLevel() {
		m.results = computeResults(m.input.Value(), m.memory)
		m.scopedResults = nil
		m.scopedQuery = ""
		m.clampSelected()
		return m, cmd
	}

	// Scoped mode: clear top-level and any stale scoped state, then fire
	// the scoped search. Clearing scopedQuery puts isLoadingScoped() into
	// the loading state, which the view uses to show a loading message
	// instead of a premature "no matches" error.
	m.results = nil
	m.scopedResults = nil
	m.scopedQuery = ""
	m.clampSelected()
	scoped := scopedSearchCmd(m.awsCtx, m.db, m.input.Value())
	if cmd != nil {
		return m, tea.Batch(cmd, scoped)
	}
	return m, scoped
}

// isLoadingScoped reports whether a scoped search is currently in flight:
// the input is scoped and the last completed scoped query does not match
// what the user is currently looking at. The view uses this to render a
// loading indicator instead of the "no matches" empty state while live
// results are still coming back.
func (m Model) isLoadingScoped() bool {
	scope := search.ParseScope(m.input.Value())
	return !scope.IsTopLevel() && m.scopedQuery != m.input.Value()
}

// visibleSearchResults returns whichever result list is currently active
// (scoped or top-level) so arrow keys and Enter operate on the same set
// the user is seeing.
func (m Model) visibleSearchResults() []search.Result {
	scope := search.ParseScope(m.input.Value())
	if !scope.IsTopLevel() {
		return m.scopedResults
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

// computeResults returns the fuzzy match results for a TOP-LEVEL query,
// or an empty slice if the query is empty.
func computeResults(query string, mem *index.Memory) []search.Result {
	if query == "" {
		return nil
	}
	return search.Fuzzy(query, mem.All(), MaxDisplayedResults)
}

// spinTickCmd schedules the next spinner frame.
func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}
