// Package lambda is the Lambda functions module. Entered via "lambda:",
// "fn:", or "functions:" prefixes. Exposes list + details + actions
// (open, copy ARN, run, tail logs).
package lambda

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

const packageID = "lambda"

type Module struct{}

func New() *Module { return &Module{} }

func (Module) Manifest() module.Manifest {
	return module.Manifest{
		ID:      packageID,
		Name:    "Lambda Functions",
		Aliases: []string{"lambda", "fn", "functions"},
		Tag:     "FN ",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#8700AF", Dark: "#D787FF"}),
		SortPriority: 3,
	}
}

func (Module) PollingInterval() time.Duration { return 0 }
func (Module) AlwaysRefresh() bool            { return false }

var _ module.Module = (*Module)(nil)

func (m *Module) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	return handleSearch(ctx, query, state)
}

func (Module) ARN(r core.Row) string {
	return r.Meta["arn"]
}

func (Module) ConsoleURL(r core.Row, region string) string {
	return "https://" + region + ".console.aws.amazon.com/lambda/home?region=" + region + "#/functions/" + r.Key
}

func (m *Module) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return resolveDetails(ctx, r)
}

func (m *Module) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	return buildDetails(r, lazy)
}

func (m *Module) Actions(r core.Row) []module.Action {
	return moduleActions(m)
}

func (Module) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	return effect.None{}
}
