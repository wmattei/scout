// Package secrets is the Secrets Manager module. Entered via
// "secrets:", "secret:", "sm:", or "sec:". Values are masked by
// default; Reveal Value action toggles a state flag so BuildDetails
// renders the raw string.
package secrets

import (
	"net/url"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

const packageID = "secrets"

type Module struct{}

func New() *Module { return &Module{} }

func (Module) Manifest() module.Manifest {
	return module.Manifest{
		ID:      packageID,
		Name:    "Secrets Manager",
		Aliases: []string{"secrets", "secret", "sm", "sec"},
		Tag:     "SEC",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#FF5F5F"}),
		SortPriority: 5,
	}
}

func (Module) PollingInterval() time.Duration { return 0 }
func (Module) AlwaysRefresh() bool             { return true }

var _ module.Module = (*Module)(nil)

func (m *Module) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	return handleSearch(ctx, query, state)
}

func (Module) ARN(r core.Row) string { return r.Meta["arn"] }

func (Module) ConsoleURL(r core.Row, region string) string {
	return "https://" + region + ".console.aws.amazon.com/secretsmanager/secret?name=" +
		url.QueryEscape(r.Key) + "&region=" + region
}

func (m *Module) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return resolveDetails(ctx, r)
}

func (m *Module) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	return buildDetails(ctx, r, lazy)
}

func (m *Module) Actions(r core.Row) []module.Action {
	return moduleActions(m)
}

func (Module) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	return effect.None{}
}

// revealed reports whether the "Reveal Value" flag is set in the
// module's opaque state. State.Bytes[0] == 1 means reveal is on.
func revealed(s effect.State) bool {
	return len(s.Bytes) > 0 && s.Bytes[0] == 1
}

func toggleReveal(s effect.State) effect.State {
	flag := byte(1)
	if revealed(s) {
		flag = 0
	}
	return effect.State{Bytes: []byte{flag}}
}
