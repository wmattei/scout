package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/awsctx/automation"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	"github.com/wmattei/scout/internal/cache"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
)

// lazyDetailKey identifies a single resource or row in a lazy-detail
// store. Historically keyed by (core.ResourceType, string) via the
// services.Provider flow. During the module cutover, Type == 0 is a
// sentinel meaning "module-owned key" and Key carries
// "<packageID>:<rowKey>". Phase-3 retires the Type field.
type lazyDetailKey struct {
	Type core.ResourceType
	Key  string
}

// moduleDetailKey builds the lazyDetailKey used to index moduleLazy
// for a module-owned row. Mirrors the layout written by
// modelHost.SetLazyDetails so both sides agree on the format.
func moduleDetailKey(packageID, rowKey string) lazyDetailKey {
	return lazyDetailKey{Key: packageID + ":" + rowKey}
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

	// Details-mode state. During the cutover, exactly one of
	// detailsResource (legacy) / detailsRow (module path) is set for
	// the lifetime of modeDetails.
	detailsResource core.Resource
	detailsRow      *core.Row
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
	// runbook execution. See executionState for field docs.
	exec executionState

	// onboardingReason is the AWS-resolve error message shown on the
	// onboarding screen. Only populated when the TUI is launched in
	// modeOnboarding because awsctx.Resolve failed. Empty in every
	// other mode.
	onboardingReason string

	// onboardingProfiles lists ~/.aws/{config,credentials} profile
	// names at startup, used by the onboarding screen to decide
	// whether to prompt "Ctrl+P to pick a profile" or the full AWS
	// setup instructions.
	onboardingProfiles []string

	// --- modules refactor (Phase 1) ---

	// moduleState stores per-module opaque state, keyed by module
	// manifest ID. Persists across keystrokes; wiped by the
	// switcher commit flow.
	moduleState map[string]effect.State

	// moduleCache is the new shared-cache handle (internal/cache
	// package). Runs alongside the legacy index/ handles during
	// migration; Phase 3 consolidates.
	moduleCache *cache.DB

	// registry is the module registry. Populated at startup; the
	// effect reducer uses it to look up modules by ID.
	registry *module.Registry

	// moduleLazy replaces lazyDetails during Phase 2 migrations.
	// Keyed by (packageID, key). Lazy maps landed via SetLazy
	// effects from module.ResolveDetails.
	moduleLazy map[lazyDetailKey]map[string]string

	// pendingEditorEffectOnSave is the callback wired by an Editor
	// effect. When the editor closes with saved content, this
	// produces the next Effect to reduce.
	pendingEditorEffectOnSave func([]byte) effect.Effect

	// virtualRow, when non-nil, is a module-owned synthetic row used
	// for Details rendering (Automation execution detail view).
	// Populated by the OpenVirtualDetails effect; cleared on Esc.
	virtualRow *core.Row

	// moduleEventActivations is the ordered list of ActivationIDs
	// emitted by the most recent module BuildDetails EventList zone.
	// renderModuleDetails writes into *moduleEventActivations; the
	// updateDetails Enter handler reads the entry at m.eventSel when
	// detailsFocus == detailsFocusEvents. Stored behind a pointer
	// so View-time writes mutate the real slice (renderDetails runs
	// against a value-copy of Model).
	moduleEventActivations *[]string
}

// executionState bundles every field that is only meaningful while the
// TUI is in modeExecution. Consolidating the fields keeps the top-level
// Model focused on search/details concerns and makes it obvious which
// state to reset when leaving the mode.
type executionState struct {
	ID             string
	Document       core.Resource
	Data           *automation.ExecutionDetails
	StepLogs       map[string][]string
	StepSel        int
	Error          string
	PollEpoch      int // invalidates in-flight polls after Esc/mode switches
	GraceRemaining int // extra ticks to poll after terminal for log catch-up
	Viewport       viewport.Model
}

const (
	detailsFocusActions = 0
	detailsFocusEvents  = 1
)

// NewModel constructs the initial model for the bubbletea program.
func NewModel(
	memory *index.Memory, db *index.DB,
	awsCtx *awsctx.Context, activity *awsctx.Activity,
	prefsDB *prefs.DB, prefsState *prefs.State,
	registry *module.Registry, moduleCache *cache.DB,
) Model {
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
		exec: executionState{
			Viewport: viewport.New(80, 10),
			StepLogs: map[string][]string{},
		},
		serviceScopeFetched: make(map[string]struct{}),
		detailsHitMap:          new([]clickRegion),
		moduleState:            make(map[string]effect.State),
		moduleLazy:             make(map[lazyDetailKey]map[string]string),
		registry:               registry,
		moduleCache:            moduleCache,
		moduleEventActivations: new([]string),
	}
}

// WithOnboarding returns a copy of the model prepared to recover from
// an AWS-resolve failure at startup. Called by cmd/scout/root when
// awsctx.Resolve returns an error. Branches on whether the user has
// any profiles configured locally:
//
//   - Profiles exist → jump straight to the profile/region switcher.
//     The resolve error is surfaced as a toast so the user still sees
//     why scout couldn't start normally. prevMode is wired to
//     modeOnboarding so pressing Esc inside the switcher lands on the
//     onboarding screen (which still offers Ctrl+P) rather than a
//     blank search bar with no AWS context.
//
//   - No profiles → show the onboarding screen with setup
//     instructions. The user must configure AWS before there's
//     anything for the switcher to show.
//
// `reason` is the resolve error's message; `profiles` is the output
// of awsctx.ListProfiles.
func (m Model) WithOnboarding(reason string, profiles []string) Model {
	m.onboardingReason = reason
	m.onboardingProfiles = profiles
	if len(profiles) > 0 {
		m.switcher = newSwitcher("", "")
		m.switcher.Show()
		m.prevMode = modeOnboarding
		m.mode = modeSwitcher
		m.toast = newErrorToast("aws: " + reason)
	} else {
		m.mode = modeOnboarding
	}
	return m
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
