package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Custom messages emitted by commands.
type (
	msgResourcesUpdated struct {
		errors []string // one string per failed subtask, empty on full success
	}
	msgAccount  struct{ account string }
	msgSpinTick struct{}

	// msgScopedResults carries the merged cache+live result set for a
	// scoped (bucket/prefix) search. `query` is the exact input value
	// that produced these results — the handler drops the message if
	// the input has moved on since, so stale results can't clobber
	// fresher ones. `err` is set when the live fetch failed; the
	// handler surfaces it as an error toast only if the query still
	// matches the current input.
	msgScopedResults struct {
		query   string
		results []search.Result
		err     string
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
		if m.inFlight && msg.String() != "ctrl+c" {
			// Block every other action while an async action is running;
			// Ctrl+C always aborts the program regardless.
			return m, nil
		}
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		case modeTailLogs:
			return m.updateTail(msg)
		case modeSwitcher:
			return m.updateSwitcher(msg)
		default:
			return m.updateSearch(msg)
		}

	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current top-level list against the updated snapshot.
		m.results = computeResults(m.input.Value(), m.memory)
		m.clampSelected()
		if len(msg.errors) > 0 {
			m.toast = newErrorToast(summarizeErrors(msg.errors))
		}
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
		if msg.err != "" {
			m.toast = newErrorToast(msg.err)
		}
		return m, nil

	case msgActionDone:
		m.inFlight = false
		m.inFlightLabel = ""
		if msg.err != nil {
			m.toast = newErrorToast(msg.toast)
		} else {
			m.toast = newToast(msg.toast, 4*time.Second)
		}
		return m, nil

	case msgTaskDefResolved:
		if msg.err != nil {
			m.toast = newErrorToast("describe task def failed: " + msg.err.Error())
			return m, nil
		}
		if m.taskDefDetails == nil {
			m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
		}
		m.taskDefDetails[msg.family] = msg.details
		return m, nil

	case msgTailStarted:
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
		m.toast = newToast("tailing "+m.tailGroup, 2*time.Second)
		return m, tailLogsNextCmd(msg.stream)

	case msgTailEvent:
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
		m.tailViewport.SetContent(strings.Join(m.tailLines, "\n"))
		m.tailViewport.GotoBottom()
		if m.tailStream != nil {
			return m, tailLogsNextCmd(m.tailStream)
		}
		return m, nil

	case msgAccount:
		m.account = msg.account
		return m, nil

	case msgSwitcherCommitted:
		m.inFlight = false
		m.inFlightLabel = ""
		if msg.err != nil {
			m.toast = newErrorToast("switch failed: " + msg.err.Error())
			return m, nil
		}
		// Close the old DB handle — we're done with it. Any still-
		// running refreshTopLevelCmd from the old context will fail
		// its next UpsertResources silently, which is acceptable for
		// v0 (see the Phase 4 plan's architecture note).
		if m.db != nil {
			_ = m.db.Close()
		}
		m.awsCtx = msg.ctx
		m.db = msg.db
		m.memory = msg.memory
		// The new context needs its own activity middleware so SDK
		// call instrumentation continues to work.
		m.activity.Attach(&m.awsCtx.Cfg)
		// Reset search state so the user lands on a clean frame.
		m.input.SetValue("")
		m.results = nil
		m.scopedResults = nil
		m.scopedQuery = ""
		m.selected = 0
		m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
		m.account = ""
		// Close the switcher overlay.
		m.switcher.Hide()
		m.mode = modeSearch
		m.toast = newToast(fmt.Sprintf("context: %s / %s", m.awsCtx.Profile, m.awsCtx.Region), 3*time.Second)
		// Kick off a fresh top-level refresh + re-resolve caller
		// identity for the new profile.
		return m, tea.Batch(
			refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
			resolveAccountCmd(m.awsCtx),
		)

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
		// Lazily resolve task-definition details (latest revision +
		// log groups) so the Details view can show them and the
		// Tail Logs action has what it needs. Both ECS task-def
		// families (family == resource key) and ECS services (family
		// from Meta["taskDefFamily"], populated by the Task-17
		// services adapter extension) trigger this.
		family := ""
		switch m.detailsResource.Type {
		case core.RTypeEcsTaskDefFamily:
			family = m.detailsResource.Key
		case core.RTypeEcsService:
			family = m.detailsResource.Meta["taskDefFamily"]
		}
		if family != "" {
			if _, ok := m.taskDefDetails[family]; !ok {
				// Mark as "in flight" with a nil value so the details
				// view can show "…resolving" instead of treating the
				// missing key as "not yet requested".
				m.taskDefDetails[family] = nil
				return m, resolveTaskDefCmd(m.awsCtx, family)
			}
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
	case "ctrl+r", "esc":
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
		return m.runAction(actions, m.actionSel)
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

	// Scoped mode: read the SQLite cache synchronously for an instant
	// first paint, then fire the live fetch as a tea.Cmd to augment
	// and persist. Keeping scopedQuery empty so isLoadingScoped() stays
	// true means the status bar keeps spinning until the live call
	// returns — but the result list is already showing whatever we
	// cached on prior visits, so there's no "loading" empty state.
	m.results = nil
	m.scopedResults = readScopedCache(m.db, scope)
	m.scopedQuery = ""
	m.clampSelected()
	scoped := scopedSearchCmd(m.awsCtx, m.db, m.input.Value())
	if cmd != nil {
		return m, tea.Batch(cmd, scoped)
	}
	return m, scoped
}

// readScopedCache does a synchronous SQLite read of bucket_contents for
// the given scope and returns prefix-matched results ready to drop into
// m.scopedResults. Used by recomputeResults to populate the scoped
// result list instantly from prior visits while the async live fetch
// runs in parallel. A brief timeout caps worst-case blocking on the
// SQLite query so a pathological disk stall can't freeze the UI.
func readScopedCache(db *index.DB, scope search.Scope) []search.Result {
	if scope.Bucket == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cached, err := db.QueryBucketContents(ctx, scope.Bucket, scope.Prefix)
	if err != nil {
		return nil
	}
	return search.Prefix(scope.Leaf, cached, MaxDisplayedResults)
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

// runAction dispatches the selected action via its Execute closure. If
// Execute is nil (not yet implemented), it falls back to the original
// stub toast so Phase 3 can migrate actions one at a time without
// breaking the UI.
func (m Model) runAction(actions []Action, idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(actions) {
		return m, nil
	}
	a := actions[idx]
	if a.Execute == nil {
		m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
		return m, nil
	}
	return a.Execute(m)
}

// formatTailLine renders a single tail event into a display line with a
// local-time timestamp prefix.
func formatTailLine(ev awslogs.TailEvent) string {
	ts := time.Unix(0, ev.Timestamp*int64(time.Millisecond)).Local().Format("15:04:05.000")
	return ts + " " + ev.Message
}

// updateTail handles key events while in modeTailLogs. Esc stops the
// stream and returns to the Details view; Ctrl+C quits the program. All
// other keys are forwarded to the viewport so the user can scroll.
func (m Model) updateTail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		return m, tea.Quit
	case "esc":
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		m.mode = modeDetails
		m.toast = newToast("stopped tailing", 2*time.Second)
		return m, nil
	}
	// Forward scroll keys to the viewport.
	var cmd tea.Cmd
	m.tailViewport, cmd = m.tailViewport.Update(msg)
	return m, cmd
}

// summarizeErrors turns a slice of subtask error strings into a single
// toast message. One error yields its text; multiple are prefixed with
// a count so the user knows more than one thing broke.
func summarizeErrors(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) == 1 {
		return "refresh failed: " + errs[0]
	}
	return fmt.Sprintf("%d subtasks failed: %s", len(errs), errs[0])
}

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
		// No-op commit when the user didn't actually change anything.
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
	// Printable characters append to the focused filter.
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
