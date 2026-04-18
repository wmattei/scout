package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/services"
)

// renderDetails produces the zoned Details screen for m.detailsResource.
// width is the frame width; the caller (view.go) passes the full frame
// width in. Layout (wide mode):
//
//	┌ IDENTITY ──┐ ┌ STATUS ─┐ ┌ METADATA ─────┐
//	│ Name  …    │ │ …       │ │ …             │
//	│ Type  [X]  │ │ …       │ │               │
//	│ ARN   …    │ └─────────┘ └───────────────┘
//	└────────────┘
//	┌ ACTIONS ───┐              ┌ RECENT EVENTS ┐
//	│ 1. …       │              │ …             │
//	│ 2. …       │              └───────────────┘
//	└────────────┘
//
// Task 4 renders Identity + Metadata + Actions only; Status + Events
// are added in Task 5. The hit-map is populated in Task 7; this task
// leaves m.detailsHitMap untouched.
func renderDetails(m Model, width int) string {
	r := m.detailsResource

	// Partition provider rows by zone. Providers that pre-date the
	// zoned layout all emit ZoneMetadata (the zero value), so this
	// partition is a no-op for them.
	var statusRows, metadataRows, eventRows []services.DetailRow
	var logRow *services.DetailRow
	if p, ok := services.Get(r.Type); ok {
		lazy := m.lazyDetailsFor(r)
		rows := p.DetailRows(r, lazy)
		for _, row := range rows {
			switch row.Zone {
			case ZoneStatus:
				statusRows = append(statusRows, row)
			case ZoneEvents:
				eventRows = append(eventRows, row)
			default: // ZoneMetadata + zero value
				metadataRows = append(metadataRows, row)
			}
		}
		if len(rows) == 0 {
			if group := p.LogGroup(r, lazy); group != "" {
				logRow = &services.DetailRow{Label: "Log", Value: group}
			}
		}
	}

	identityBlock := renderIdentityZone(m, r, 34)
	statusBlock := renderStatusZone(statusRows, 22)
	metadataBlock := renderMetadataZone(m, metadataRows, logRow, 40)
	eventsBlock := renderEventsZone(eventRows, 52)
	actionsBlock := renderActionsZone(m, 28)

	// Top row: only include zones that have content.
	topParts := []string{identityBlock}
	if statusBlock != "" {
		topParts = append(topParts, "  ", statusBlock)
	}
	if metadataBlock != "" {
		topParts = append(topParts, "  ", metadataBlock)
	}
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topParts...)

	// Bottom row: actions on the left, events (if any) on the right.
	bottomRow := actionsBlock
	if eventsBlock != "" {
		bottomRow = lipgloss.JoinHorizontal(lipgloss.Top, actionsBlock, "  ", eventsBlock)
	}

	return lipgloss.JoinVertical(lipgloss.Left, topRow, "", bottomRow)
}

// ZoneMetadata/ZoneStatus/ZoneEvents constant aliases local to this
// package, so renderDetails doesn't have to write services.ZoneStatus
// on every switch. Same underlying type.
const (
	ZoneMetadata = services.ZoneMetadata
	ZoneStatus   = services.ZoneStatus
	ZoneEvents   = services.ZoneEvents
)

// renderIdentityZone renders the top-left Identity zone: Name, Type
// (color-coded via the provider's TagStyle), and ARN. Width is the
// preferred column width; the caller may pass a larger value for
// narrow-mode full-width rendering.
func renderIdentityZone(m Model, r core.Resource, width int) string {
	var b strings.Builder

	// Name row.
	writeZoneField(&b, "Name", r.DisplayName)

	// Type row — colored tag chip + descriptive suffix.
	if p, ok := services.Get(r.Type); ok {
		chip := p.TagStyle().Render(padTag(p.TagLabel()))
		typeLine := chip + " " + typeDescription(r.Type)
		writeZoneField(&b, "Type", typeLine)
	} else {
		writeZoneField(&b, "Type", typeDescription(r.Type))
	}

	// ARN row. While the lazy-details resolve is in-flight, show the
	// same "…resolving" placeholder the pre-zoned layout used.
	writeZoneField(&b, "ARN", detailsARN(r, m))

	body := strings.TrimRight(b.String(), "\n")
	return renderZoneBlock("IDENTITY", body, width)
}

// renderMetadataZone renders the top-right Metadata zone from the
// provider's ZoneMetadata rows. Preserves the prior section-header
// and blank-spacer semantics (empty label + empty value = blank
// line; empty label + non-empty value = section header).
func renderMetadataZone(m Model, rows []services.DetailRow, logFallback *services.DetailRow, width int) string {
	if len(rows) == 0 && logFallback == nil {
		return ""
	}

	var b strings.Builder
	inFlight := false
	if p, ok := services.Get(m.detailsResource.Type); ok {
		_ = p // suppress unused-in-else warning
		inFlight = m.lazyDetailsState[lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}] == lazyStateInFlight
	}

	if len(rows) == 0 && inFlight {
		// No rows yet but a resolve is running — placeholder.
		b.WriteString(styleRowDim.Render("resolving details…"))
	} else {
		for _, row := range rows {
			switch {
			case row.Label == "" && row.Value == "":
				b.WriteString("\n")
			case row.Label == "":
				b.WriteString(row.Value)
				b.WriteString("\n")
			default:
				writeZoneFieldWide(&b, row.Label, row.Value)
			}
		}
		if len(rows) == 0 && logFallback != nil {
			writeZoneField(&b, logFallback.Label, logFallback.Value)
		}
	}

	body := strings.TrimRight(b.String(), "\n")
	return renderZoneBlock("METADATA", body, width)
}

// renderActionsZone renders the bottom-left Actions zone.
func renderActionsZone(m Model, width int) string {
	actions := ActionsFor(m.detailsResource.Type)
	if len(actions) == 0 {
		body := styleRowDim.Render("(no actions available)")
		return renderZoneBlock("ACTIONS", body, width)
	}

	var b strings.Builder
	for i, a := range actions {
		indi := "  "
		if i == m.actionSel {
			indi = styleSelIndi.Render("▸ ")
		}
		line := fmt.Sprintf("%s%d. %s", indi, i+1, a.Label)
		if m.pendingConfirmFn != nil && i == m.actionSel {
			line += "  " + styleConfirmHint.Render("(confirm: y/n)")
		}
		b.WriteString(line)
		if i < len(actions)-1 {
			b.WriteString("\n")
		}
	}
	return renderZoneBlock("ACTIONS", b.String(), width)
}

// renderStatusZone renders the top-center Status zone from rows the
// provider tagged ZoneStatus. Returns "" (signaling collapse) when
// there are no status rows.
func renderStatusZone(rows []services.DetailRow, width int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(row.Value)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return renderZoneBlock("STATUS", b.String(), width)
}

// renderEventsZone renders the bottom-right Events zone. Each row's
// Value is rendered on its own line (Label is intentionally ignored;
// event lines are preformatted strings from the provider).
func renderEventsZone(rows []services.DetailRow, width int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(row.Value)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return renderZoneBlock("RECENT EVENTS", b.String(), width)
}

// renderZoneBlock wraps body in a rounded-border block with a dim
// header label in the top-left of the border. width is the total
// visible width including the 2-column border; content inside gets
// width-4 (2 border + 2 padding columns).
func renderZoneBlock(header, body string, width int) string {
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	block := styleZoneBorder.Width(innerWidth).Render(body)
	// Overlay the header in the top border. The top border is the
	// first line of the rendered block; splice the header label into
	// it preserving the border characters on either side.
	return overlayZoneHeader(block, header)
}

// overlayZoneHeader replaces part of the first line of block with
// " HEADER " so the rounded top border reads "╭─ HEADER ──────╮".
// Leaves other lines untouched.
func overlayZoneHeader(block, header string) string {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return block
	}
	top := lines[0]
	label := " " + styleZoneHeader.Render(header) + " "
	labelW := lipgloss.Width(label)
	topW := lipgloss.Width(top)
	if topW < labelW+4 {
		return block // border too narrow, skip header
	}
	// Walk the rune sequence of `top`, starting at column 2 (after the
	// left corner+dash). Replace the next `labelW` visual columns with
	// the header label, then keep the rest of the top border intact.
	runes := []rune(stripANSI(top))
	if len(runes) < labelW+4 {
		return block
	}
	newTop := string(runes[:2]) + label + string(runes[2+labelW:])
	lines[0] = newTop
	return strings.Join(lines, "\n")
}

// writeZoneField appends "Label   Value\n" to b using the zone's
// narrow label column (6 chars, like the pre-zoned Identity rows).
func writeZoneField(b *strings.Builder, label, value string) {
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 6)))
	b.WriteString(" ")
	b.WriteString(value)
	b.WriteString("\n")
}

// writeZoneFieldWide uses the 11-char label column for the Metadata
// zone's wider labels (Deployment, LB target, Task def, …).
func writeZoneFieldWide(b *strings.Builder, label, value string) {
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 11)))
	b.WriteString(" ")
	b.WriteString(value)
	b.WriteString("\n")
}

// typeDescription returns the human-readable suffix for the Identity
// zone's Type row ("bucket", "service", "function", etc.).
func typeDescription(t core.ResourceType) string {
	switch t {
	case core.RTypeBucket:
		return "bucket"
	case core.RTypeFolder:
		return "folder"
	case core.RTypeObject:
		return "object"
	case core.RTypeEcsService:
		return "service"
	case core.RTypeEcsTaskDefFamily:
		return "task definition"
	case core.RTypeLambdaFunction:
		return "function"
	case core.RTypeSSMParameter:
		return "parameter"
	default:
		return ""
	}
}

// detailsARN resolves the ARN shown in the Details Identity zone.
// While resolution is in-flight it shows "…resolving"; otherwise it
// delegates to the provider's ARN method so each provider decides
// what ARN is authoritative (service ARN for ECS services, revision
// ARN for task-def families, etc.).
func detailsARN(r core.Resource, m Model) string {
	key := lazyDetailKey{Type: r.Type, Key: r.Key}
	if m.lazyDetailsState[key] == lazyStateInFlight {
		return "…resolving"
	}
	lazy := m.lazyDetailsFor(r)
	if p, ok := services.Get(r.Type); ok {
		if a := p.ARN(r, lazy); a != "" {
			return a
		}
	}
	return ""
}

// padRightPlain right-pads s with ASCII spaces to n runes. Used for
// label columns that contain no lipgloss styling. Distinct from the
// lipgloss-aware padRight in results.go.
func padRightPlain(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
