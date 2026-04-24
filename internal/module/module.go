package module

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/widget"
)

// Manifest is the static description of a module. Every module
// returns the same values for the lifetime of the process.
type Manifest struct {
	ID           string   // stable identifier, e.g. "s3"
	Name         string   // "S3 Buckets"
	Aliases      []string // prefix tokens entered by the user
	Tag          string   // 3-4 char pill, e.g. "S3 "
	TagStyle     lipgloss.Style
	SortPriority int // tiebreaker when fuzzy scores tie across modules
}

// DetailZones is the 5-zone content shape. Zones with empty widgets
// collapse at render time.
type DetailZones struct {
	Status   widget.Block
	Metadata widget.Block
	Value    widget.Block
	Events   widget.Block
}

// Action is one entry in the Actions zone. Run is pure — it receives
// the module context plus the selected Row and returns an Effect.
type Action struct {
	Label string
	Run   func(ctx Context, r core.Row) effect.Effect
}

// Module is the contract.
type Module interface {
	Manifest() Manifest

	// HandleSearch runs per-keystroke while the user is in this
	// module (prefix present in input). Returns rows to display
	// (usually from cache for instant paint) plus effects to kick
	// off any live AWS calls via Async.
	HandleSearch(ctx Context, query string, state effect.State) (
		rows []core.Row, newState effect.State, effects []effect.Effect)

	// ARN returns the resource ARN for the Identity zone header and
	// the generic "Copy ARN" action.
	ARN(r core.Row) string

	// ConsoleURL is the "Open in Browser" target.
	ConsoleURL(r core.Row, region string) string

	// ResolveDetails is called when the user enters the Details
	// view. The returned effect is typically Async wrapping an AWS
	// Describe call; the handler lands a SetLazy effect.
	ResolveDetails(ctx Context, r core.Row) effect.Effect

	// BuildDetails fills the 5 zones for this row given the lazy
	// map from the resolved-details call.
	BuildDetails(ctx Context, r core.Row, lazy map[string]string) DetailZones

	// Actions declares the action list for this row.
	Actions(r core.Row) []Action

	// HandleEvent is called when the user activates a selectable
	// EventList row. Replaces today's eventActivationRegistry map.
	HandleEvent(ctx Context, r core.Row, activationID string) effect.Effect

	// PollingInterval > 0 causes core to re-invoke ResolveDetails
	// on that cadence while Details is open.
	PollingInterval() time.Duration

	// AlwaysRefresh forces ResolveDetails to fire on every entry
	// into Details (bypasses the lazy cache). Used by modules with
	// rapidly-changing state (ECS service running count, Automation
	// execution status).
	AlwaysRefresh() bool
}
