package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Model is the bubbletea model for the search + details views. Phase 2
// introduces a Mode split: modeSearch runs the input bar + result list,
// modeDetails runs the Details panel + Actions list for a chosen row.
type Model struct {
	// Injected dependencies.
	memory   *index.Memory
	db       *index.DB
	awsCtx   *awsctx.Context
	activity *awsctx.Activity

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
	// taskDefDetails caches the result of DescribeFamily (or equivalent)
	// for any task-def family whose Details view has been opened. Keyed
	// by family name. A present-but-nil entry means "resolution in
	// flight"; a missing entry means "not yet requested".
	taskDefDetails map[string]*awsecs.TaskDefDetails

	// Tail-logs-mode state.
	tailGroup    string              // log group name currently being tailed
	tailLines    []string            // already-formatted lines in the scrollback
	tailStream   *awslogs.TailStream // cancellable stream handle
	tailViewport viewport.Model      // scrolling log viewport

	// Unused in Phase 2; reserved for Phase 4's refresh progress tracking.
	lastTopLevel []core.Resource
}

// NewModel constructs the initial model for the bubbletea program.
func NewModel(memory *index.Memory, db *index.DB, awsCtx *awsctx.Context, activity *awsctx.Activity) Model {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 512

	return Model{
		memory:         memory,
		db:             db,
		awsCtx:         awsCtx,
		activity:       activity,
		input:          ti,
		width:          80,
		height:         24,
		mode:           modeSearch,
		taskDefDetails: make(map[string]*awsecs.TaskDefDetails),
		tailViewport:   viewport.New(80, 10),
	}
}

// Init is called once when the program starts. Phase 1 left the initial
// result list empty on purpose; Phase 2 preserves that behavior.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}
