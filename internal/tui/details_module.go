package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

// moduleZoneStyle wraps each filled zone in a titled bordered panel.
// Kept separate from the legacy zone styles so the two renderers can
// evolve independently; Phase-3 cleanup collapses them.
var moduleZoneStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "#A0A0A0", Dark: "#5F5F87"}).
	Padding(0, 1)

var moduleZoneTitle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#303030", Dark: "#D0D0D0"})

// renderModuleDetails is the module-era counterpart to the legacy
// renderDetails body. Five zones — Identity (built by core), Status /
// Metadata / Value / Events (built by the module) — plus an Actions
// zone listing module.Actions(r) entries.
func (m Model) renderModuleDetails(r core.Row, width, height int) string {
	mod, ok := m.moduleForID(r.PackageID)
	if !ok {
		return centerEmptyState(width, height, "module "+r.PackageID+" not registered")
	}

	key := moduleDetailKey(r.PackageID, r.Key)
	lazy := m.moduleLazy[key]
	ctx := m.moduleContextFor(r.PackageID)
	zones := mod.BuildDetails(ctx, r, lazy)

	// Publish EventList activation IDs for the update loop to look
	// up when Enter is pressed in events-focus. Clear first so a
	// zone without events doesn't retain stale IDs from a prior row.
	if m.moduleEventActivations != nil {
		*m.moduleEventActivations = nil
		if el, ok := zones.Events.(widget.EventList); ok {
			ids := make([]string, 0, len(el.Rows))
			for _, row := range el.Rows {
				ids = append(ids, row.ActivationID)
			}
			*m.moduleEventActivations = ids
		}
	}

	identity := m.renderModuleIdentityZone(mod, r, width/3, height/2)
	status := renderModuleZone("Status", zones.Status, width/3, height/2)
	metadata := renderModuleZone("Metadata", zones.Metadata, width/3, height/2)
	value := renderModuleZone("Value", zones.Value, width/3, height/2)
	events := renderModuleZone("Events", zones.Events, width/3, height/2)
	actions := m.renderModuleActionsZone(mod, r, width/3, height/2)

	top := joinNonEmptyHorizontal(identity, status, metadata)
	bottom := joinNonEmptyHorizontal(actions, value, events)
	body := lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	return body
}

// renderModuleIdentityZone renders the always-visible top-left panel
// with the module's tag pill, the row name, and ARN (when known).
func (m Model) renderModuleIdentityZone(mod module.Module, r core.Row, w, h int) string {
	mani := mod.Manifest()
	arn := mod.ARN(r)
	rows := []widget.KVRow{
		{Label: "Name", Value: r.Name, Clickable: true, ClipValue: r.Name},
	}
	if arn != "" {
		rows = append(rows, widget.KVRow{Label: "ARN", Value: arn, Clickable: true, ClipValue: arn})
	}
	title := mani.TagStyle.Render(mani.Tag) + " " + mani.Name
	return renderModuleZoneTitled(title, widget.KeyValue{Rows: rows}, w, h)
}

// renderModuleActionsZone is a lightweight list of action labels with
// the current m.actionSel highlighted. Enter dispatch lands in
// Cutover 8.
func (m Model) renderModuleActionsZone(mod module.Module, r core.Row, w, h int) string {
	actions := mod.Actions(r)
	if len(actions) == 0 {
		return ""
	}
	var lines []string
	for i, a := range actions {
		prefix := "  "
		if i == m.actionSel {
			prefix = "▶ "
		}
		lines = append(lines, prefix+a.Label)
	}
	body := strings.Join(lines, "\n")
	return renderModuleZoneRaw("Actions", body, w, h)
}

// renderModuleZone wraps a widget.Block in a titled bordered panel.
// Returns "" when the widget renders empty so the caller collapses
// the zone.
func renderModuleZone(title string, block widget.Block, w, h int) string {
	if block == nil {
		return ""
	}
	return renderModuleZoneTitled(title, block, w, h)
}

func renderModuleZoneTitled(title string, block widget.Block, w, h int) string {
	innerW := w - 4
	innerH := h - 2
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 1 {
		innerH = 1
	}
	body := block.Render(innerW, innerH)
	if body == "" {
		return ""
	}
	return moduleZoneStyle.Width(w).Render(moduleZoneTitle.Render(title) + "\n" + body)
}

func renderModuleZoneRaw(title, body string, w, h int) string {
	if body == "" {
		return ""
	}
	return moduleZoneStyle.Width(w).Render(moduleZoneTitle.Render(title) + "\n" + body)
}

// joinNonEmptyHorizontal drops zero-width operands before joining.
// Keeps layout tight when a zone collapses (e.g. Value is empty for
// rows that don't have a standalone value).
func joinNonEmptyHorizontal(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, kept...)
}
