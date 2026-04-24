package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderDetails produces the Details screen. Every details view is
// module-owned; the Model carries a detailsRow (or virtualRow for
// synthetic rows like Automation executions) set by the module path.
func renderDetails(m Model, width, height int) string {
	if m.virtualRow != nil {
		return m.renderModuleDetails(*m.virtualRow, width, height)
	}
	if m.detailsRow != nil {
		return m.renderModuleDetails(*m.detailsRow, width, height)
	}
	return centerEmptyState(width, height, "no resource selected")
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

// measureBodyWidth returns the widest visible line width (in cells)
// across every line in body. Used by the module Details renderer to
// pick a content-sized block width for the flex-initial zones.
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

// padRightPlain right-pads s with ASCII spaces to n runes. Used for
// label columns that contain no lipgloss styling. Distinct from the
// lipgloss-aware padRight in results.go.
func padRightPlain(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
