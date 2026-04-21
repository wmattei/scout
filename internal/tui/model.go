package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/awsctx/automation"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
)

// lazyDetailKey identifies a single (resource type, resource key)
// pair in the m.lazyDetails store. Used by the generic
// services.Provider.ResolveDetails flow.
type lazyDetailKey struct {
	Type core.ResourceType
	Key  string
}

// lazyDetailState tracks whether a given lazyDetailKey has had its
// resolve fired, completed, or never been requested.
type lazyDetailState int

const (
	lazyStateNone     lazyDetailState = iota // never requested
	lazyStateInFlight                        // resolveDetails command running
	lazyStateResolved                        // command landed, m.lazyDetails populated
)

// clickRegion is a rectangular hit-box in the rendered frame used by
// the Details-mode mouse handler. X0/Y0 are inclusive, X1/Y1 are
// exclusive; coordinates are in frame-absolute cell units so a
// tea.MouseMsg's X/Y can be tested directly.
type clickRegion struct {
	X0, Y0, X1, Y1 int
	Clipboard      string
	Label          string
}

// Model is the bubbletea model for the search + details views.
// Mode split: modeSearch runs the input bar + result list,
// modeDetails runs the Details panel + Actions list for a chosen row.
type Model struct {
	// Injected dependencies.
	memory     *index.Memory
	db         *index.DB
	awsCtx     *awsctx.Context
	activity   *awsctx.Activity
	prefs      *prefs.DB
	prefsState *prefs.State

	// Shared UI state.
	input    textinput.Model
	width    int
	height   int
	account  string
	spinTick int
	toast    Toast
	mode     Mode

	// In-flight async action — blocks further input until msgActionDone.
	inFlight      bool
	inFlightLabel string

	// Search-mode state.
	selected      int
	results       []search.Result
	scopedResults []search.Result
	scopedQuery   string

	// Details-mode state.
	detailsResource core.Resource
	actionSel       int
	// detailsHitMap holds the clickable regions for the currently-
	// rendered Details frame. It is a pointer so the View-time
	// rendering (which sees a value-copy of Model) can populate it
	// without returning a new Model. Update reads the slice on
	// tea.MouseMsg to match a click against a cell.
	detailsHitMap *[]clickRegion
	// lazyDetails is the generic per-resource extra-data store
	// populated by services.Provider.ResolveDetails. Keyed by
	// (resource type, resource key) so different types can't
	// collide on the same string key.
	lazyDetails      map[lazyDetailKey]map[string]string
	lazyDetailsState map[lazyDetailKey]lazyDetailState

	// Tail-logs-mode state.
	tailGroup    string              // log group name currently being tailed
	tailLines    []string            // already-formatted lines in the scrollback
	tailStream   *awslogs.TailStream // cancellable stream handle
	tailViewport viewport.Model      // scrolling log viewport

	// Generic confirmation gate. When non-nil, updateDetails intercepts
	// the next keystroke: 'y' fires the callback, anything else cancels.
	// Set by destructive actions (Force Deploy, future deletes, etc.).
	pendingConfirmFn func(Model) (Model, tea.Cmd)

	// Tail filter — when non-empty, only lines containing this
	// substring are shown in the viewport. The backend continues
	// collecting every line into tailLines so clearing the filter
	// restores the full scrollback. tailFilterEditing is true while
	// the user is typing into the filter prompt (/ key).
	tailFilter        string
	tailFilterEditing bool

	// Switcher overlay state and the previous mode to return to on
	// Esc. `switcher.Visible` mirrors `mode == modeSwitcher`; keeping
	// both in sync is the responsibility of the Update handlers.
	switcher Switcher
	prevMode Mode

	// serviceScopeFetched tracks which service-scope aliases have had
	// their "first-entry" live fetch fire during this session. On the
	// first keystroke that activates "<alias>:", recomputeResults
	// dispatches refreshServiceCmd and adds the alias to this set;
	// subsequent keystrokes under the same alias just re-filter the
	// in-memory index. The set is cleared by the switcher commit
	// handler when the AWS context swaps.
	serviceScopeFetched map[string]struct{}

	// Editor state for interactive actions (Lambda invoke, SSM update).
	// pendingEditorAction identifies what to do after the editor closes;
	// pendingEditorPath is the temp file the editor writes to;
	// pendingEditorResource is the resource the editor was opened for.
	pendingEditorAction   editorAction
	pendingEditorPath     string
	pendingEditorResource core.Resource

	// Details keyboard focus. In modeDetails the focus toggles
	// between the Actions zone (default) and the Events zone when
	// it contains selectable rows (e.g. runbook execution history).
	// eventSel is the selected index within the current zone's
	// selectable-row list.
	detailsFocus int
	eventSel     int

	// Execution-details mode state — populated when entering a
	// runbook execution. executionPollEpoch invalidates in-flight
	// polls after Esc/mode switches so stale msgExecutionFetched
	// messages don't overwrite fresh state.
	executionID             string
	executionDocument       core.Resource
	executionData           *automation.ExecutionDetails
	executionStepLogs       map[string][]string
	executionStepSel        int
	executionError          string
	executionPollEpoch      int
	executionGraceRemaining int // extra ticks to poll after terminal for log catch-up
	executionViewport       viewport.Model
}

const (
	detailsFocusActions = 0
	detailsFocusEvents  = 1
)

// NewModel constructs the initial model for the bubbletea program.
func NewModel(memory *index.Memory, db *index.DB, awsCtx *awsctx.Context, activity *awsctx.Activity, prefsDB *prefs.DB, prefsState *prefs.State) Model {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 512

	return Model{
		memory:              memory,
		db:                  db,
		awsCtx:              awsCtx,
		activity:            activity,
		prefs:               prefsDB,
		prefsState:          prefsState,
		input:               ti,
		width:               80,
		height:              24,
		mode:                modeSearch,
		lazyDetails:         make(map[lazyDetailKey]map[string]string),
		lazyDetailsState:    make(map[lazyDetailKey]lazyDetailState),
		tailViewport:        viewport.New(80, 10),
		executionViewport:   viewport.New(80, 10),
		executionStepLogs:   map[string][]string{},
		serviceScopeFetched: make(map[string]struct{}),
		detailsHitMap:       new([]clickRegion),
	}
}

// Init is called once when the program starts. The TUI no longer fires
// a top-level refresh at launch — the cache is populated lazily by
// service-scope first-entry fetches and explicitly by the
// `scout preload <service>` subcommand. Init only kicks off the
// spinner ticker and the one-shot caller-identity resolver here.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}

// lazyDetailsFor returns the resolved lazy detail map for the given
// resource, or nil if nothing has been resolved (or resolution is
// still in flight). Used by per-action providers via the action
// dispatcher; see services.Provider.ConsoleURL / LogGroup signatures.
func (m Model) lazyDetailsFor(r core.Resource) map[string]string {
	if m.lazyDetails == nil {
		return nil
	}
	return m.lazyDetails[lazyDetailKey{Type: r.Type, Key: r.Key}]
}
