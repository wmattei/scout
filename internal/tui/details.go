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

	identityW := 34
	statusW := 22
	metadataW := 40
	eventsW := 52
	actionsW := 28
	if width < 75 {
		identityW, statusW, metadataW, eventsW, actionsW = width, width, width, width, width
	}
	identityBlock, identityRegs := renderIdentityZone(m, r, identityW)
	statusBlock, _ := renderStatusZone(statusRows, statusW)
	metadataBlock, metaRegs := renderMetadataZone(m, metadataRows, logRow, metadataW)
	eventsBlock, _ := renderEventsZone(eventRows, eventsW)
	actionsBlock, _ := renderActionsZone(m, actionsW)

	// Inside each zone block the body starts at (2, 1) — 1 border
	// row at the top plus 1 padding column on the left, plus the 1
	// character wide border. Callers offset their zone-local regions
	// by this inner origin plus the zone's position in the overall
	// frame.
	const zoneBodyOffsetX = 2
	const zoneBodyOffsetY = 1

	if width < 75 {
		return renderDetailsStackedWithRegions(m, width, zoneBodyOffsetX, zoneBodyOffsetY,
			identityBlock, statusBlock, metadataBlock, eventsBlock, actionsBlock,
			identityRegs, metaRegs)
	}

	// Wide layout: three-column top row (Identity, Status, Metadata),
	// two-column bottom row (Actions, Events). Track zone origins so
	// the returned regions can be offset into frame-absolute coords.
	topParts := []string{identityBlock}
	identityX := 0
	statusX := 0
	metadataX := 0
	cursorX := lipgloss.Width(identityBlock)
	if statusBlock != "" {
		topParts = append(topParts, "  ", statusBlock)
		statusX = cursorX + 2
		cursorX = statusX + lipgloss.Width(statusBlock)
	}
	if metadataBlock != "" {
		topParts = append(topParts, "  ", metadataBlock)
		metadataX = cursorX + 2
	}
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topParts...)
	_ = statusX // Status zone has no clickable cells in v1

	topHeight := lipgloss.Height(topRow)
	bottomY := topHeight + 1 // +1 for the blank separator row inserted by JoinVertical
	_ = metadataX

	// Wire frame-absolute regions from the zone-local ones.
	var regions []clickRegion
	for _, rg := range identityRegs {
		regions = append(regions, offsetRegion(rg, identityX+zoneBodyOffsetX, 0+zoneBodyOffsetY))
	}
	for _, rg := range metaRegs {
		regions = append(regions, offsetRegion(rg, metadataX+zoneBodyOffsetX, 0+zoneBodyOffsetY))
	}

	// Publish regions so Update's mouse handler can match clicks.
	if m.detailsHitMap != nil {
		*m.detailsHitMap = regions
	}
	_ = bottomY // Actions/Events rows have no clickable cells in v1

	bottomRow := actionsBlock
	if eventsBlock != "" {
		bottomRow = lipgloss.JoinHorizontal(lipgloss.Top, actionsBlock, "  ", eventsBlock)
	}

	return lipgloss.JoinVertical(lipgloss.Left, topRow, "", bottomRow)
}

// offsetRegion shifts a zone-local clickRegion into frame-absolute
// coordinates by adding (dx, dy) to both corners.
func offsetRegion(r clickRegion, dx, dy int) clickRegion {
	r.X0 += dx
	r.Y0 += dy
	r.X1 += dx
	r.Y1 += dy
	return r
}

// renderDetailsStackedWithRegions composes the narrow-mode vertical
// stack and offsets zone-local regions into frame-absolute coords.
// Zones stack in canonical order (Identity, Status, Metadata, Events,
// Actions); Y offsets are tracked as the stack grows. Because
// Identity and Metadata are the only zones with regions in v1, only
// those are offset and published.
func renderDetailsStackedWithRegions(
	m Model, _ int, bodyX, bodyY int,
	identity, status, metadata, events, actions string,
	identityRegs, metaRegs []clickRegion,
) string {
	zones := []string{identity}
	y := 0

	identityY := y
	y += lipgloss.Height(identity)

	if status != "" {
		zones = append(zones, status)
		y += lipgloss.Height(status)
	}
	metadataY := -1
	if metadata != "" {
		metadataY = y
		zones = append(zones, metadata)
		y += lipgloss.Height(metadata)
	}
	if events != "" {
		zones = append(zones, events)
		y += lipgloss.Height(events)
	}
	if actions != "" {
		zones = append(zones, actions)
	}

	var regions []clickRegion
	for _, rg := range identityRegs {
		regions = append(regions, offsetRegion(rg, bodyX, identityY+bodyY))
	}
	if metadataY >= 0 {
		for _, rg := range metaRegs {
			regions = append(regions, offsetRegion(rg, bodyX, metadataY+bodyY))
		}
	}
	if m.detailsHitMap != nil {
		*m.detailsHitMap = regions
	}

	return lipgloss.JoinVertical(lipgloss.Left, zones...)
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
// (color-coded via the provider's TagStyle), and ARN. Name and ARN
// are always marked clickable. Returns the rendered block plus
// zone-local click regions (in cell coordinates relative to the
// zone block's top-left corner) for the Name and ARN rows.
func renderIdentityZone(m Model, r core.Resource, width int) (string, []clickRegion) {
	var b strings.Builder
	var regions []clickRegion

	// Name row — always clickable.
	nameValue := styleClickable.Render(r.DisplayName)
	regions = append(regions, zoneRowRegion(0, 6, nameValue, r.DisplayName, "Name"))
	writeZoneFieldRaw(&b, "Name", nameValue)

	// Type row — colored tag chip + descriptive suffix, not clickable.
	if p, ok := services.Get(r.Type); ok {
		chip := p.TagStyle().Render(padTag(p.TagLabel()))
		typeLine := chip + " " + typeDescription(r.Type)
		writeZoneFieldRaw(&b, "Type", typeLine)
	} else {
		writeZoneFieldRaw(&b, "Type", typeDescription(r.Type))
	}

	// ARN row — clickable unless resolution is in-flight.
	arnValue := detailsARN(r, m)
	if arnValue != "" && arnValue != "…resolving" {
		styled := styleClickable.Render(arnValue)
		regions = append(regions, zoneRowRegion(2, 6, styled, arnValue, "ARN"))
		writeZoneFieldRaw(&b, "ARN", styled)
	} else {
		writeZoneFieldRaw(&b, "ARN", arnValue)
	}

	body := strings.TrimRight(b.String(), "\n")
	return renderZoneBlock("IDENTITY", body, width), regions
}

// renderMetadataZone renders the top-right Metadata zone. Clickable
// rows are styled and tracked in the returned region slice (local
// zone coordinates; caller offsets them into frame-absolute coords).
func renderMetadataZone(m Model, rows []services.DetailRow, logFallback *services.DetailRow, width int) (string, []clickRegion) {
	if len(rows) == 0 && logFallback == nil {
		inFlight := m.lazyDetailsState[lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}] == lazyStateInFlight
		if !inFlight {
			return "", nil
		}
		body := styleRowDim.Render("resolving details…")
		return renderZoneBlock("METADATA", body, width), nil
	}

	var b strings.Builder
	var regions []clickRegion
	y := 0 // line index within the zone body
	for _, row := range rows {
		switch {
		case row.Label == "" && row.Value == "":
			b.WriteString("\n")
			y++
		case row.Label == "":
			b.WriteString(row.Value)
			b.WriteString("\n")
			y++
		default:
			value := row.Value
			if row.Clickable {
				clip := row.ClipboardValue
				if clip == "" {
					clip = stripANSI(row.Value)
				}
				value = styleClickable.Render(value)
				regions = append(regions, zoneRowRegion(y, 11, value, clip, row.Label))
			}
			writeZoneFieldRawWide(&b, row.Label, value)
			y++
		}
	}
	if len(rows) == 0 && logFallback != nil {
		value := logFallback.Value
		if logFallback.Clickable {
			clip := logFallback.ClipboardValue
			if clip == "" {
				clip = stripANSI(logFallback.Value)
			}
			value = styleClickable.Render(value)
			regions = append(regions, zoneRowRegion(y, 6, value, clip, logFallback.Label))
		}
		writeZoneFieldRaw(&b, logFallback.Label, value)
	}

	body := strings.TrimRight(b.String(), "\n")
	return renderZoneBlock("METADATA", body, width), regions
}

// renderActionsZone renders the bottom-left Actions zone. Actions
// are keyboard-driven — not clickable in v1.
func renderActionsZone(m Model, width int) (string, []clickRegion) {
	actions := ActionsFor(m.detailsResource.Type)
	if len(actions) == 0 {
		body := styleRowDim.Render("(no actions available)")
		return renderZoneBlock("ACTIONS", body, width), nil
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
	return renderZoneBlock("ACTIONS", b.String(), width), nil
}

// renderStatusZone renders the top-center Status zone. Status rows
// are informational — never clickable — so no regions are produced.
func renderStatusZone(rows []services.DetailRow, width int) (string, []clickRegion) {
	if len(rows) == 0 {
		return "", nil
	}
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(row.Value)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return renderZoneBlock("STATUS", b.String(), width), nil
}

// renderEventsZone renders the Events zone. Event rows are dim text
// lines — not clickable in v1.
func renderEventsZone(rows []services.DetailRow, width int) (string, []clickRegion) {
	if len(rows) == 0 {
		return "", nil
	}
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(row.Value)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return renderZoneBlock("RECENT EVENTS", b.String(), width), nil
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

// writeZoneFieldRaw is like writeZoneField but doesn't re-style the
// value (caller may have wrapped it in styleClickable). Both variants
// use the 6-char label column.
func writeZoneFieldRaw(b *strings.Builder, label, styledValue string) {
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 6)))
	b.WriteString(" ")
	b.WriteString(styledValue)
	b.WriteString("\n")
}

// writeZoneFieldRawWide uses the 11-char label column for the wider
// Metadata-zone labels.
func writeZoneFieldRawWide(b *strings.Builder, label, styledValue string) {
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 11)))
	b.WriteString(" ")
	b.WriteString(styledValue)
	b.WriteString("\n")
}

// zoneRowRegion builds a zone-local clickRegion for a single label/value
// row at line `y`, where the value begins at column `labelW + 1`
// (label column + single space). The region's X1 is the cell after
// the last rune of the plain-text value.
func zoneRowRegion(y, labelW int, styledValue, clipboard, label string) clickRegion {
	plain := stripANSI(styledValue)
	valueCols := lipgloss.Width(plain)
	x0 := labelW + 1
	return clickRegion{
		X0:        x0,
		Y0:        y,
		X1:        x0 + valueCols,
		Y1:        y + 1,
		Clipboard: clipboard,
		Label:     label,
	}
}
