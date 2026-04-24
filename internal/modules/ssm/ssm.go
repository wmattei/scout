// Package ssm is the SSM parameters module. Entered via "ssm:",
// "param:", "params:", or "parameter:" prefixes. AlwaysRefresh is true
// because parameter values commonly change out-of-band.
package ssm

import (
	"net/url"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

const packageID = "ssm"

type Module struct{}

func New() *Module { return &Module{} }

func (Module) Manifest() module.Manifest {
	return module.Manifest{
		ID:      packageID,
		Name:    "SSM Parameters",
		Aliases: []string{"ssm", "param", "params", "parameter"},
		Tag:     "SSM",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#005F87", Dark: "#5FD7FF"}),
		SortPriority: 4,
	}
}

func (Module) PollingInterval() time.Duration { return 0 }
func (Module) AlwaysRefresh() bool             { return true }

var _ module.Module = (*Module)(nil)

func (m *Module) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	return handleSearch(ctx, query, state)
}

// ARN is available once details have been resolved (stored in Meta by
// resolveDetails). Empty at list time.
func (Module) ARN(r core.Row) string { return r.Meta["arn"] }

func (Module) ConsoleURL(r core.Row, region string) string {
	return "https://" + region + ".console.aws.amazon.com/systems-manager/parameters/" +
		url.PathEscape(r.Key) + "/description?region=" + region
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
