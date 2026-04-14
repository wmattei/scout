package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execTailLogs resolves the log group from cached task-def details and
// switches the TUI into modeTailLogs. Lazy resolution is triggered on
// entering modeDetails for both ECS services and task-def families
// (Task 17), so by the time the user activates this action the cache
// should be populated. If it isn't yet (resolution still in flight) we
// show a "details still loading" toast and the user can retry.
func execTailLogs(m Model) (Model, tea.Cmd) {
	family := taskDefFamilyForDetails(m)
	if family == "" {
		m.toast = newToast("no task definition linked to this resource", 4*time.Second)
		return m, nil
	}
	d, ok := m.taskDefDetails[family]
	if !ok {
		m.toast = newToast("task definition not yet resolved", 3*time.Second)
		return m, nil
	}
	if d == nil {
		m.toast = newToast("task definition still resolving — try again", 2*time.Second)
		return m, nil
	}
	if len(d.LogGroups) == 0 {
		m.toast = newToast("no CloudWatch log group configured on this task definition", 4*time.Second)
		return m, nil
	}
	group := d.LogGroups[0]

	m.mode = modeTailLogs
	m.tailGroup = group
	m.tailLines = nil
	// Clear the viewport content too — without this, the previous tail
	// session's lines linger visually until the new stream emits enough
	// events to overwrite them.
	m.tailViewport.SetContent("")
	m.tailViewport.GotoTop()
	m.inFlight = true
	m.inFlightLabel = "starting tail…"
	return m, tailLogsStartCmd(m.awsCtx, group, m.account)
}

// taskDefFamilyForDetails returns the task-def family name associated
// with the current details resource. For ECS task-def families the
// resource's Key is already the family name. For ECS services we read
// the family from Meta["taskDefFamily"], which is populated by the
// ListServices adapter extension in Task 17.
func taskDefFamilyForDetails(m Model) string {
	r := m.detailsResource
	switch r.Type {
	case core.RTypeEcsTaskDefFamily:
		return r.Key
	case core.RTypeEcsService:
		return r.Meta["taskDefFamily"]
	}
	return ""
}
