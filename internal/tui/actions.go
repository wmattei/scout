package tui

import "github.com/wagnermattei/better-aws-cli/internal/core"

// Action is a single selectable entry in the Details view's Actions list.
// Phase 2 only uses Label — execution is stubbed and uniformly produces a
// "not yet implemented — Phase 3" toast. Phase 3 will add an Execute
// function field (or swap this struct for an interface).
type Action struct {
	Label string
}

// ActionsFor returns the ordered action list for a resource type. The
// ordering matches the spec's actions matrix and determines the number
// hotkey assigned to each action (first item is `1`, second is `2`, etc.).
//
// Unknown types return an empty slice; the Details view handles that
// gracefully by showing only the Details panel.
func ActionsFor(t core.ResourceType) []Action {
	switch t {
	case core.RTypeBucket:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
		}
	case core.RTypeFolder:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
		}
	case core.RTypeObject:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
			{Label: "Download"},
			{Label: "Preview"},
		}
	case core.RTypeEcsService:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Force new Deployment"},
			{Label: "Tail Logs"},
		}
	case core.RTypeEcsTaskDefFamily:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy ARN"},
			{Label: "Tail Logs"},
		}
	default:
		return nil
	}
}
