package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/services"
)

// Action is a single selectable entry in the Details view's Actions list.
// Label is what the user sees; Execute is called (via the dispatcher in
// update.go) when the action is activated. Execute receives the current
// Model and returns a (new Model, tea.Cmd) pair using the same contract
// as Update so it can set fields on m, fire follow-up commands, or both.
//
// An Execute may be nil for actions that are not yet implemented — the
// dispatcher will fall back to the "not yet implemented" toast in that
// case. This is the migration path: Phase 3 tasks fill in each Execute
// one at a time, and nothing breaks along the way.
type Action struct {
	Label   string
	Execute ActionExecute
}

// ActionExecute is the function signature for an action's behavior. It
// mirrors bubbletea's Update signature so actions can freely mutate the
// model and dispatch side-effect commands.
type ActionExecute func(m Model) (Model, tea.Cmd)

// msgActionDone is emitted by any in-flight async action when its work
// completes. The dispatcher in update.go handles the message by clearing
// `inFlight` and showing the resulting toast. If refetchDetails is true,
// the handler also invalidates the lazyDetails cache for the current
// resource and re-fires ResolveDetails so the Details panel shows fresh
// data (used by SSM Update Value after PutParameter succeeds).
type msgActionDone struct {
	toast          string
	err            error
	refetchDetails bool
	success        bool // render as a green success toast instead of neutral info
}

// actionExecuteRegistry maps action IDs (as returned by ActionDef.ID) to
// their Execute functions. Adding actions for a new service means:
// (a) add ActionDefs in the provider's Actions() method with matching IDs,
// (b) add Execute entries here. If an ID isn't in the registry, Execute
// is nil and the dispatcher falls through to the "not yet implemented" toast.
var actionExecuteRegistry = map[string]ActionExecute{
	"open":         execOpenInBrowser,
	"copy-arn":     execCopyARN,
	"copy-uri":     execCopyURI,
	"force-deploy": execForceDeploy,
	"tail-logs":    execTailLogs,
	"download":     execDownload,
	"preview":      execPreview,
	"run":          execLambdaRun,
	"copy-value":   execSSMCopyValue,
	"update-value": execSSMUpdateValue,
}

// ActionsFor returns the ordered action list for a resource type. It looks
// up the provider via services.Get, reads provider.Actions() for the ordered
// ActionDef list, and maps each ActionDef to an Action by looking up the
// Execute function in actionExecuteRegistry. This eliminates the type-switch
// that previously lived here — adding actions for a new service only requires
// changes in the provider's Actions() method and the registry map above.
func ActionsFor(t core.ResourceType) []Action {
	p, ok := services.Get(t)
	if !ok {
		return nil
	}
	defs := p.Actions()
	if len(defs) == 0 {
		return nil
	}
	actions := make([]Action, len(defs))
	for i, def := range defs {
		actions[i] = Action{
			Label:   def.Label,
			Execute: actionExecuteRegistry[def.ID], // nil if ID not registered
		}
	}
	return actions
}
