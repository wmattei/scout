package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	"github.com/wmattei/scout/internal/cache"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
)

// lazyDetailKey identifies a single module row in the lazy-detail
// store. Keyed by (PackageID, Key) so two modules can use the same
// row Key without colliding.
type lazyDetailKey struct {
	PackageID string
	Key       string
}

// moduleDetailKey builds the lazyDetailKey used to index lazyDetails
// for a module-owned row. Mirrors the layout written by
// modelHost.SetLazyDetails so both sides agree on the format.
func moduleDetailKey(packageID, rowKey string) lazyDetailKey {
	return lazyDetailKey{PackageID: packageID, Key: rowKey}
}

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

	// Details-mode state. detailsRow is the module-owned row currently
	// being inspected; virtualRow replaces it for synthetic drill-in
	// views (e.g. Automation execution detail).
	detailsRow *core.Row
	actionSel  int
	// detailsHitMap holds the clickable regions for the currently-
	// rendered Details frame. It is a pointer so the View-time
	// rendering (which sees a value-copy of Model) can populate it
	// without returning a new Model. Update reads the slice on
	// tea.MouseMsg to match a click against a cell.
	detailsHitMap *[]clickRegion
	// lazyDetails is the per-row extra-data store populated by
	// module.ResolveDetails via SetLazyDetails. Keyed by (packageID,
	// rowKey) so two modules can reuse the same row Key safely.
	lazyDetails map[lazyDetailKey]map[string]string

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

	// Details keyboard focus. In modeDetails the focus toggles
	// between the Actions zone (default) and the Events zone when
	// it contains selectable rows (e.g. runbook execution history).
	// eventSel is the selected index within the current zone's
	// selectable-row list.
	detailsFocus int
	eventSel     int

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

	// --- modules runtime ---

	// moduleState stores per-module opaque state, keyed by module
	// manifest ID. Persists across keystrokes; wiped by the
	// switcher commit flow.
	moduleState map[string]effect.State

	// moduleCache is the shared cache handle all modules read and
	// write through via effect.UpsertCacheRows / cache.Reader.
	moduleCache *cache.DB

	// registry is the module registry. Populated at startup; the
	// effect reducer uses it to look up modules by ID.
	registry *module.Registry

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

const (
	detailsFocusActions = 0
	detailsFocusEvents  = 1
)

// NewModel constructs the initial model for the bubbletea program.
func NewModel(
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
		awsCtx:                 awsCtx,
		activity:               activity,
		prefs:                  prefsDB,
		prefsState:             prefsState,
		input:                  ti,
		width:                  80,
		height:                 24,
		mode:                   modeSearch,
		lazyDetails:            make(map[lazyDetailKey]map[string]string),
		tailViewport:           viewport.New(80, 10),
		detailsHitMap:          new([]clickRegion),
		moduleState:            make(map[string]effect.State),
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

// Init is called once when the program starts. Kicks off the spinner
// ticker and the one-shot caller-identity resolver.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}
