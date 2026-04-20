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
// width and body height in. Layout (wide mode, width >= 75):
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
// Below 75 cols the zones stack vertically. Provider rows are
// partitioned by DetailRow.Zone; Identity and Metadata zones can
// carry clickable cells that publish hit regions into
// *m.detailsHitMap for the Update-loop mouse handler to copy
// on left-click.
//
// Sizing model (wide mode) is CSS-flex-like:
//   Identity, Status, Actions → flex: 0 0 content  (width = widest line + 4)
//   Metadata → flex: 1                              (grows to fill top row)
//   Events   → flex: 1                              (grows to fill bottom row)
// Top zones share a uniform height (max natural); bottom zones share
// a uniform height that stretches to fill the remaining body budget.
func renderDetails(m Model, width, height int) string {
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

	// Build each zone's body text (and collect zone-local click
	// regions). Body-only — no border wrapping yet, so we can
	// measure the natural content width.
	idBody, identityRegs := buildIdentityBody(m, r)
	stBody := buildStatusBody(statusRows)
	mdBody, metaRegs := buildMetadataBody(m, metadataRows, logRow)
	evBody := buildEventsBody(eventRows)
	acBody := buildActionsBody(m)

	// Narrow mode: each zone renders at full frame width, stacked
	// vertically, natural heights.
	if width < 75 {
		identityBlock := renderZoneBlock("IDENTITY", idBody, width, 0)
		statusBlock := ""
		if stBody != "" {
			statusBlock = renderZoneBlock("STATUS", stBody, width, 0)
		}
		metadataBlock := ""
		if mdBody != "" {
			metadataBlock = renderZoneBlock("METADATA", mdBody, width, 0)
		}
		eventsBlock := ""
		if evBody != "" {
			eventsBlock = renderZoneBlock("RECENT EVENTS", evBody, width, 0)
		}
		actionsBlock := renderZoneBlock("ACTIONS", acBody, width, 0)
		const zoneBodyOffsetX = 2
		const zoneBodyOffsetY = 1
		return renderDetailsStackedWithRegions(m, width, zoneBodyOffsetX, zoneBodyOffsetY,
			identityBlock, statusBlock, metadataBlock, eventsBlock, actionsBlock,
			identityRegs, metaRegs)
	}

	// Wide mode — compute zone widths from content. Identity,
	// Status, and Actions size to their content (flex-initial);
	// Metadata and Events get the remainder (flex: 1).
	const gap = 2
	identityW := measureBodyWidth(idBody) + 4
	// Cap Identity at half the frame so a very long ARN can't
	// starve Metadata of space.
	if maxID := width / 2; identityW > maxID {
		identityW = maxID
	}

	statusW := 0
	if stBody != "" {
		statusW = measureBodyWidth(stBody) + 4
	}

	actionsW := measureBodyWidth(acBody) + 4

	metadataW := width - identityW - gap
	if statusW > 0 {
		metadataW -= statusW + gap
	}
	if metadataW < 24 {
		metadataW = 24
	}

	eventsW := 0
	if evBody != "" {
		eventsW = width - actionsW - gap
		if eventsW < 24 {
			eventsW = 24
		}
	}

	// Render with natural heights first to measure the tallest top
	// zone — then re-render the top row at that uniform height so
	// all three bottom borders align on the same line.
	idNat := renderZoneBlock("IDENTITY", idBody, identityW, 0)
	stNat := ""
	if statusW > 0 {
		stNat = renderZoneBlock("STATUS", stBody, statusW, 0)
	}
	mdNat := ""
	if mdBody != "" {
		mdNat = renderZoneBlock("METADATA", mdBody, metadataW, 0)
	}
	topHeight := lipgloss.Height(idNat)
	if stNat != "" && lipgloss.Height(stNat) > topHeight {
		topHeight = lipgloss.Height(stNat)
	}
	if mdNat != "" && lipgloss.Height(mdNat) > topHeight {
		topHeight = lipgloss.Height(mdNat)
	}

	identityBlock := renderZoneBlock("IDENTITY", idBody, identityW, topHeight)
	statusBlock := ""
	if statusW > 0 {
		statusBlock = renderZoneBlock("STATUS", stBody, statusW, topHeight)
	}
	metadataBlock := ""
	if mdBody != "" {
		metadataBlock = renderZoneBlock("METADATA", mdBody, metadataW, topHeight)
	}

	// Bottom row — Actions + Events (if any). Stretch both to fill
	// the remaining body budget so their borders close on the bottom
	// divider row. Fall back to natural height when the budget is
	// smaller than the natural content height.
	acNat := renderZoneBlock("ACTIONS", acBody, actionsW, 0)
	evNat := ""
	if eventsW > 0 {
		evNat = renderZoneBlock("RECENT EVENTS", evBody, eventsW, 0)
	}
	naturalBottom := lipgloss.Height(acNat)
	if evNat != "" && lipgloss.Height(evNat) > naturalBottom {
		naturalBottom = lipgloss.Height(evNat)
	}
	bottomHeight := height - topHeight - 1 // -1 for the blank separator row
	if bottomHeight < naturalBottom {
		bottomHeight = naturalBottom
	}
	actionsBlock := renderZoneBlock("ACTIONS", acBody, actionsW, bottomHeight)
	eventsBlock := ""
	if eventsW > 0 {
		eventsBlock = renderZoneBlock("RECENT EVENTS", evBody, eventsW, bottomHeight)
	}

	// Inside each zone block the body starts at (2, 1) — 1 border
	// row at the top plus 1 padding column on the left, plus the 1
	// character wide border. Callers offset their zone-local regions
	// by this inner origin plus the zone's position in the overall
	// frame.
	const zoneBodyOffsetX = 2
	const zoneBodyOffsetY = 1

	// Wide layout: three-column top row (Identity, Status, Metadata),
	// two-column bottom row (Actions, Events). Track zone origins so
	// the returned regions can be offset into frame-absolute coords.
	topParts := []string{identityBlock}
	identityX := 0
	metadataX := 0
	cursorX := lipgloss.Width(identityBlock)
	if statusBlock != "" {
		topParts = append(topParts, "  ", statusBlock)
		cursorX = cursorX + gap + lipgloss.Width(statusBlock)
	}
	if metadataBlock != "" {
		topParts = append(topParts, "  ", metadataBlock)
		metadataX = cursorX + gap
	}
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topParts...)

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

// ----------------------------------------------------------------------
// Zone body builders — produce the inner text of each zone without
// wrapping it in a border. renderDetails measures these bodies to pick
// block widths (CSS-flex-like) and then calls renderZoneBlock to apply
// the border + header at the chosen size.
// ----------------------------------------------------------------------

// buildIdentityBody returns the Identity zone body plus zone-local
// click regions for the Name and ARN rows.
func buildIdentityBody(m Model, r core.Resource) (string, []clickRegion) {
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

	return strings.TrimRight(b.String(), "\n"), regions
}

// buildStatusBody returns the Status zone body (one line per status
// row, preformatted by the provider). Returns "" when the provider
// emitted no status rows, signalling zone collapse.
func buildStatusBody(rows []services.DetailRow) string {
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
	return b.String()
}

// buildMetadataBody returns the Metadata zone body plus zone-local
// click regions for any Clickable rows. While the provider's lazy
// resolve is in-flight with no rows yet, a dim "resolving details…"
// placeholder is returned so the zone doesn't collapse before first
// data arrives.
func buildMetadataBody(m Model, rows []services.DetailRow, logFallback *services.DetailRow) (string, []clickRegion) {
	if len(rows) == 0 && logFallback == nil {
		inFlight := m.lazyDetailsState[lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}] == lazyStateInFlight
		if !inFlight {
			return "", nil
		}
		return styleRowDim.Render("resolving details…"), nil
	}

	var b strings.Builder
	var regions []clickRegion
	y := 0
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

	return strings.TrimRight(b.String(), "\n"), regions
}

// buildEventsBody returns the Events zone body, or "" when no event
// rows exist.
func buildEventsBody(rows []services.DetailRow) string {
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
	return b.String()
}

// buildActionsBody returns the Actions zone body — numbered action
// lines with the current selection indicator and optional confirm
// hint for destructive actions.
func buildActionsBody(m Model) string {
	actions := ActionsFor(m.detailsResource.Type)
	if len(actions) == 0 {
		return styleRowDim.Render("(no actions available)")
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
	return b.String()
}

// measureBodyWidth returns the widest visible line width (in cells)
// across every line in body. Used by renderDetails to pick a
// content-sized block width for the flex-initial zones (Identity,
// Status, Actions).
func measureBodyWidth(body string) int {
	max := 0
	for _, line := range strings.Split(body, "\n") {
		w := lipgloss.Width(line)
		if w > max {
			max = w
		}
	}
	return max
}

// renderZoneBlock wraps body in a rounded-border block with a dim
// header label in the top-left of the border. width is the total
// visible width including the 2-column border; content inside gets
// width-4 (2 border + 2 padding columns). When height > 0, the
// block is padded vertically so the total rendered height (border
// included) equals that value; use 0 to size naturally to content.
func renderZoneBlock(header, body string, width, height int) string {
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	style := styleZoneBorder.Width(innerWidth)
	if height > 0 {
		innerHeight := height - 2 // 2 border rows
		if innerHeight < 1 {
			innerHeight = 1
		}
		style = style.Height(innerHeight)
	}
	block := style.Render(body)
	// Overlay the header in the top border. The top border is the
	// first line of the rendered block; splice the header label into
	// it preserving the border characters on either side.
	return overlayZoneHeader(block, header)
}

// overlayZoneHeader replaces part of the first line of block with
// " HEADER " so the rounded top border reads "╭─ HEADER ──────╮".
// The non-label segments of the border are re-styled with the
// border's foreground color so the top line stays visually
// consistent with the rest of the border.
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
	// Re-style the non-label segments with the zone border color so
	// the top line's corners and dashes don't reset to the terminal's
	// default foreground.
	runes := []rune(stripANSI(top))
	if len(runes) < labelW+4 {
		return block
	}
	borderColor := lipgloss.NewStyle().Foreground(ac("#A8A8A8", "#484848"))
	newTop := borderColor.Render(string(runes[:2])) +
		label +
		borderColor.Render(string(runes[2+labelW:]))
	lines[0] = newTop
	return strings.Join(lines, "\n")
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

// writeZoneFieldRaw appends "Label   Value\n" to b using the narrow
// 6-char label column. The value is written as-is so the caller can
// pre-style it (e.g. with styleClickable).
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
