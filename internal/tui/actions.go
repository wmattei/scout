package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
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
// `inFlight` and showing the resulting toast.
type msgActionDone struct {
	toast string
	err   error
}

// ActionsFor returns the ordered action list for a resource type. The
// Execute fields are left nil in this declaration and populated by each
// action-implementation task so this file stays a single source of truth
// for ordering, labeling, and hotkey assignment.
func ActionsFor(t core.ResourceType) []Action {
	switch t {
	case core.RTypeBucket:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
		}
	case core.RTypeFolder:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
		}
	case core.RTypeObject:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Download", Execute: execDownload},
			{Label: "Preview", Execute: execPreview},
		}
	case core.RTypeEcsService:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Force new Deployment", Execute: execForceDeploy},
			{Label: "Tail Logs", Execute: execTailLogs},
		}
	case core.RTypeEcsTaskDefFamily:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Tail Logs", Execute: execTailLogs},
		}
	case core.RTypeLambdaFunction:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Tail Logs", Execute: execTailLogs},
			{Label: "Run", Execute: execLambdaRun},
		}
	case core.RTypeSSMParameter:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Copy Value", Execute: execSSMCopyValue},
			{Label: "Update Value", Execute: execSSMUpdateValue},
		}
	default:
		return nil
	}
}
