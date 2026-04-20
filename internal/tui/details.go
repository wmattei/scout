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
	var statusRows, metadataRows, eventRows, valueRows []services.DetailRow
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
			case ZoneValue:
				valueRows = append(valueRows, row)
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
	vBody, valueRow := buildValueBody(valueRows)

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
		valueBlock := ""
		if vBody != "" {
			valueBlock = renderZoneBlock("VALUE", vBody, width, 0)
		}
		eventsBlock := ""
		if evBody != "" {
			eventsBlock = renderZoneBlock("RECENT EVENTS", evBody, width, 0)
		}
		actionsBlock := renderZoneBlock("ACTIONS", acBody, width, 0)
		const zoneBodyOffsetX = 2
		const zoneBodyOffsetY = 1
		return renderDetailsStackedWithRegions(m, width, zoneBodyOffsetX, zoneBodyOffsetY,
			identityBlock, statusBlock, metadataBlock, valueBlock, eventsBlock, actionsBlock,
			identityRegs, metaRegs, valueRow)
	}

	// Wide mode — compute zone widths from content using a flex-
	// like allocator. Each zone starts at an equal share of the
	// frame, then zones whose natural content width is smaller
	// give their excess back to zones that want more. Absolute
	// minimum prevents a zone from collapsing below legibility.
	const (
		gap         = 2
		absoluteMin = 20
	)

	// Top row participants.
	topPresent := []bool{true, stBody != "", mdBody != ""} // identity, status, metadata
	topMaxW := []int{
		measureBodyWidth(idBody) + 4,
		measureBodyWidth(stBody) + 4,
		measureBodyWidth(mdBody) + 4,
	}
	topN := 0
	for _, p := range topPresent {
		if p {
			topN++
		}
	}
	topGaps := 0
	if topN > 1 {
		topGaps = (topN - 1) * gap
	}
	topBudget := width - topGaps

	// Collect present zones, call flex, then scatter back.
	topMins := make([]int, 0, topN)
	topMaxs := make([]int, 0, topN)
	for i, present := range topPresent {
		if present {
			topMins = append(topMins, absoluteMin)
			topMaxs = append(topMaxs, topMaxW[i])
		}
	}
	topAllocs := distributeFlex(topBudget, topMins, topMaxs)

	identityW, statusW, metadataW := 0, 0, 0
	idx := 0
	for i, present := range topPresent {
		if !present {
			continue
		}
		switch i {
		case 0:
			identityW = topAllocs[idx]
		case 1:
			statusW = topAllocs[idx]
		case 2:
			metadataW = topAllocs[idx]
		}
		idx++
	}

	// Bottom row participants: Actions is always present; Events
	// only if event rows exist.
	actionsW := 0
	eventsW := 0
	if evBody != "" {
		bottomBudget := width - gap
		botAllocs := distributeFlex(bottomBudget,
			[]int{absoluteMin, absoluteMin},
			[]int{measureBodyWidth(acBody) + 4, measureBodyWidth(evBody) + 4})
		actionsW, eventsW = botAllocs[0], botAllocs[1]
	} else {
		actionsW = measureBodyWidth(acBody) + 4
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

	// Value zone — full-width middle row between top and bottom.
	// Capped at half of what's left after the top row so the bottom
	// row (Actions, Events) always has usable vertical room. When the
	// raw body exceeds the cap we truncate visible lines and surface
	// an explanatory suffix — click-to-copy still carries the full
	// original value.
	var valueBlock string
	valueHeight := 0
	if vBody != "" {
		vBodyRendered := vBody
		available := height - topHeight - 2 // -2 for the two separator rows
		// Reserve at least 5 rows for the bottom row so Actions stays
		// usable even when the value is enormous.
		maxValueHeight := available - 5
		if maxValueHeight < 5 {
			maxValueHeight = 5
		}
		natural := strings.Count(vBody, "\n") + 1 + 2 // +2 for border rows
		if natural <= maxValueHeight {
			valueHeight = natural
		} else {
			valueHeight = maxValueHeight
			bodyLines := strings.Split(vBody, "\n")
			keep := valueHeight - 2 - 1 // border rows + truncation marker
			if keep < 1 {
				keep = 1
			}
			if keep < len(bodyLines) {
				vBodyRendered = strings.Join(bodyLines[:keep], "\n") + "\n" +
					styleRowDim.Render("  … (truncated — click to copy full value)")
			}
		}
		valueBlock = renderZoneBlock("VALUE", vBodyRendered, width, valueHeight)
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
	if valueHeight > 0 {
		bottomHeight -= valueHeight + 1 // value row + its separator
	}
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

	// Value zone click region spans the full inner body when the
	// value row opted into click-to-copy. The zone starts one blank
	// separator row below the top row's last border line.
	if valueBlock != "" && valueRow != nil && valueRow.Clickable {
		clip := valueRow.ClipboardValue
		if clip == "" {
			clip = stripANSI(valueRow.Value)
		}
		valueY := topHeight + 1 // +1 for the blank separator row
		innerW := width - 2
		innerH := valueHeight - 2
		if innerH < 1 {
			innerH = 1
		}
		regions = append(regions, clickRegion{
			X0:        0 + zoneBodyOffsetX,
			Y0:        valueY + zoneBodyOffsetY,
			X1:        innerW,
			Y1:        valueY + zoneBodyOffsetY + innerH,
			Clipboard: clip,
			Label:     valueRow.Label,
		})
	}

	// Publish regions so Update's mouse handler can match clicks.
	if m.detailsHitMap != nil {
		*m.detailsHitMap = regions
	}

	bottomRow := actionsBlock
	if eventsBlock != "" {
		bottomRow = lipgloss.JoinHorizontal(lipgloss.Top, actionsBlock, "  ", eventsBlock)
	}

	parts := []string{topRow, ""}
	if valueBlock != "" {
		parts = append(parts, valueBlock, "")
	}
	parts = append(parts, bottomRow)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
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
	m Model, frameW int, bodyX, bodyY int,
	identity, status, metadata, value, events, actions string,
	identityRegs, metaRegs []clickRegion,
	valueRow *services.DetailRow,
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
	valueY := -1
	if value != "" {
		valueY = y
		zones = append(zones, value)
		y += lipgloss.Height(value)
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
	if valueY >= 0 && valueRow != nil && valueRow.Clickable {
		clip := valueRow.ClipboardValue
		if clip == "" {
			clip = stripANSI(valueRow.Value)
		}
		innerW := frameW - 2
		innerH := lipgloss.Height(value) - 2
		if innerH < 1 {
			innerH = 1
		}
		regions = append(regions, clickRegion{
			X0:        bodyX,
			Y0:        valueY + bodyY,
			X1:        innerW,
			Y1:        valueY + bodyY + innerH,
			Clipboard: clip,
			Label:     valueRow.Label,
		})
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
	ZoneValue    = services.ZoneValue
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
			y += writeZoneFieldRawWide(&b, row.Label, value)
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

// buildValueBody returns the Value zone body (typically a single
// large payload — secret value, decoded blob) plus a pointer to the
// originating row so the wide-mode renderer can publish a whole-zone
// click region when the row opts into click-to-copy. Returns ("", nil)
// when no provider row targeted ZoneValue.
func buildValueBody(rows []services.DetailRow) (string, *services.DetailRow) {
	if len(rows) == 0 {
		return "", nil
	}
	row := rows[0]
	return row.Value, &row
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

// distributeFlex allocates `budget` cells across N zones using a
// CSS-flex-like model: every zone starts at an equal share of the
// budget; zones whose natural max is smaller than the equal share
// give their excess back to a pool; zones whose equal share is below
// their max pull from that pool to grow up to their max. Absolute
// minimums are enforced last — a zone won't shrink below its min
// even if the budget is tight (overflow/line-wrap is acceptable).
//
// maxs[i] is the zone's natural content width (preferred ceiling);
// mins[i] is the absolute floor. len(mins) must equal len(maxs).
// Returns one allocation per zone in the same order.
func distributeFlex(budget int, mins, maxs []int) []int {
	n := len(mins)
	if n == 0 {
		return nil
	}
	allocs := make([]int, n)

	// Pass 1: start at equal share, cap at each zone's max.
	equalShare := budget / n
	for i := 0; i < n; i++ {
		w := equalShare
		if w > maxs[i] {
			w = maxs[i]
		}
		allocs[i] = w
	}

	// Pass 2: redistribute slack. Zones currently below their max
	// compete for any remaining budget; small share goes round-robin
	// so the allocation is fair when the pool doesn't split cleanly.
	for {
		used := 0
		for _, a := range allocs {
			used += a
		}
		slack := budget - used
		if slack <= 0 {
			break
		}
		growable := 0
		for i := 0; i < n; i++ {
			if allocs[i] < maxs[i] {
				growable++
			}
		}
		if growable == 0 {
			break
		}
		share := slack / growable
		if share == 0 {
			share = 1
		}
		progress := false
		for i := 0; i < n && slack > 0; i++ {
			if allocs[i] >= maxs[i] {
				continue
			}
			add := share
			if add > maxs[i]-allocs[i] {
				add = maxs[i] - allocs[i]
			}
			if add > slack {
				add = slack
			}
			if add > 0 {
				allocs[i] += add
				slack -= add
				progress = true
			}
		}
		if !progress {
			break
		}
	}

	// Pass 3: enforce absolute minimums. A zone below its floor is
	// bumped up — if this pushes the total above budget the overflow
	// is tolerated (the zone would otherwise be unusable).
	for i := 0; i < n; i++ {
		if allocs[i] < mins[i] {
			allocs[i] = mins[i]
		}
	}
	return allocs
}

// renderZoneBlock wraps body in a rounded-border block with a dim
// header label in the top-left of the border. width is the total
// visible width including the border; lipgloss's Width() argument
// is `width - 2` because Width() sets the rendered width excluding
// the border but INCLUDING any padding. The inner content area
// inside the border and the 1-col padding on each side is therefore
// `width - 4` cells wide. When height > 0, the block is padded
// vertically so the total rendered height (border included) equals
// that value; use 0 to size naturally to content.
func renderZoneBlock(header, body string, width, height int) string {
	styleW := width - 2 // total width minus the 2 border columns
	if styleW < 1 {
		styleW = 1
	}
	style := styleZoneBorder.Width(styleW)
	if height > 0 {
		styleH := height - 2 // total height minus the 2 border rows
		if styleH < 1 {
			styleH = 1
		}
		style = style.Height(styleH)
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
// Metadata-zone labels. When styledValue contains embedded newlines
// each continuation line is indented to column 12 so the wrapped text
// stays visually aligned with the first line's value. Returns the
// number of lines written so the caller can advance its y cursor
// accurately for hit-map bookkeeping.
func writeZoneFieldRawWide(b *strings.Builder, label, styledValue string) int {
	lines := strings.Split(styledValue, "\n")
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 11)))
	b.WriteString(" ")
	b.WriteString(lines[0])
	b.WriteString("\n")
	for _, line := range lines[1:] {
		b.WriteString(strings.Repeat(" ", 12))
		b.WriteString(line)
		b.WriteString("\n")
	}
	return len(lines)
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
