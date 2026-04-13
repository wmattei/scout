package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Model is the bubbletea model for the search view. Phase 1 only has the
// search mode — Phase 2 will introduce a Mode enum and additional sub-models.
type Model struct {
	// Injected dependencies.
	memory   *index.Memory
	db       *index.DB
	awsCtx   *awsctx.Context
	activity *awsctx.Activity

	// UI state.
	input    textinput.Model
	width    int
	height   int
	selected int
	results  []search.Result
	account  string
	spinTick int

	// Derived: the last snapshot we cached resources into memory from.
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
	}
}

// Init is called once when the program starts. Phase 1 starts with an empty
// result list on purpose — the TUI shows a "start typing" hint until the
// user enters a query. Background refresh and account resolution run
// concurrently with the first render.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}
