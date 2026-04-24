// Package automation is the SSM Automation module. Documents surface
// as top-level rows; executions are module-owned virtual rows
// (Key prefix "exec:") opened via OpenVirtualDetails from a document's
// Events zone.
package automation

import (
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

const (
	packageID     = "automation"
	execKeyPrefix = "exec:"
)

type Module struct{}

func New() *Module { return &Module{} }

func (Module) Manifest() module.Manifest {
	return module.Manifest{
		ID:      packageID,
		Name:    "SSM Automation",
		Aliases: []string{"auto", "automation", "runbook"},
		Tag:     "AUTO",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#005F00", Dark: "#5FD75F"}),
		SortPriority: 6,
	}
}

func (Module) PollingInterval() time.Duration { return 0 } // polling handled via Tick from ResolveDetails
func (Module) AlwaysRefresh() bool             { return true }

var _ module.Module = (*Module)(nil)

func (m *Module) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	return handleSearch(ctx, query, state)
}

// Automation documents don't have an ARN in the AWS SDK response; the
// Copy ARN action surfaces an error toast when invoked.
func (Module) ARN(r core.Row) string { return "" }

func (Module) ConsoleURL(r core.Row, region string) string {
	if strings.HasPrefix(r.Key, execKeyPrefix) {
		execID := strings.TrimPrefix(r.Key, execKeyPrefix)
		return "https://" + region + ".console.aws.amazon.com/systems-manager/automation/execution/" +
			url.PathEscape(execID) + "?region=" + region
	}
	return "https://" + region + ".console.aws.amazon.com/systems-manager/documents/" +
		url.PathEscape(r.Key) + "/description?region=" + region
}

func (m *Module) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	if strings.HasPrefix(r.Key, execKeyPrefix) {
		return resolveExecution(ctx, r)
	}
	return resolveDocument(ctx, r)
}

func (m *Module) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	if strings.HasPrefix(r.Key, execKeyPrefix) {
		return buildExecution(r, lazy)
	}
	return buildDocument(r, lazy)
}

func (m *Module) Actions(r core.Row) []module.Action {
	return moduleActions(m, r)
}

func (Module) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	// Activation IDs from a document's Events zone are execution IDs —
	// open the virtual-row detail view for that execution.
	return effect.OpenVirtualDetails{
		PackageID: packageID,
		Key:       execKeyPrefix + activationID,
		Name:      activationID,
	}
}

func isExec(r core.Row) bool {
	return strings.HasPrefix(r.Key, execKeyPrefix)
}
