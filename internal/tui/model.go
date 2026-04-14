package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
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

	// Search-mode state.
	selected      int
	results       []search.Result
	scopedResults []search.Result // populated in scoped mode from cache + live
	scopedQuery   string          // the input value that produced scopedResults

	// Details-mode state.
	detailsResource core.Resource
	actionSel       int

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
		memory:   memory,
		db:       db,
		awsCtx:   awsCtx,
		activity: activity,
		input:    ti,
		width:    80,
		height:   24,
		mode:     modeSearch,
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
