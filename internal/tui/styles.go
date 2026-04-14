// Package tui renders and drives the bubbletea program. Styles for the
// whole TUI live in this file so colors and borders can be tweaked in one
// place.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// adaptive pair helper.
func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	// Input bar.
	styleInputPrompt = lipgloss.NewStyle().Bold(true).Foreground(ac("#005FAF", "#5FD7FF"))
	styleInputText   = lipgloss.NewStyle().Foreground(ac("#000000", "#FFFFFF"))

	// Result rows.
	styleRowBase   = lipgloss.NewStyle()
	styleRowSel    = lipgloss.NewStyle().Background(ac("#D0D0FF", "#2A2A5A"))
	styleRowDim    = lipgloss.NewStyle().Foreground(ac("#767676", "#8A8A8A"))
	styleHighlight = lipgloss.NewStyle().Bold(true).Foreground(ac("#000000", "#FFFFFF"))
	styleSelIndi   = lipgloss.NewStyle().Bold(true).Foreground(ac("#005FAF", "#5FD7FF"))

	// Tag styles per resource type. Keys are ResourceType.Tag() strings.
	styleTagS3   = tagStyle("#005FAF", "#5FD7FF")
	styleTagDir  = tagStyle("#008787", "#5FFFFF")
	styleTagObj  = tagStyle("#585858", "#A8A8A8")
	styleTagEcs  = tagStyle("#AF5F00", "#FFAF5F")
	styleTagTask = tagStyle("#AF8700", "#FFD75F")

	// Status bar. No .Padding(0, 1) here: renderStatus already wraps its
	// content with explicit leading/trailing spaces to reach exactly
	// `width` columns. Adding lipgloss Padding on top of that
	// double-counted the margins, which made Width(width) wrap the
	// overflow onto a second line and scrolled the input bar off the
	// alt-screen during every refresh.
	styleStatusBar = lipgloss.NewStyle().Background(ac("#D0D0E0", "#1A1A2E")).Foreground(ac("#000000", "#D0D0D0"))
	styleSpinner   = lipgloss.NewStyle().Foreground(ac("#005F87", "#5FAFD7"))
	styleError     = lipgloss.NewStyle().Bold(true).Foreground(ac("#870000", "#FF5F5F"))
	styleToastError = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
		Background(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#870000"}).
		Padding(0, 1)
	styleDivider   = lipgloss.NewStyle().Foreground(ac("#A8A8A8", "#303030"))
)

func tagStyle(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(ac(light, dark))
}

// tagStyleFor returns the colored tag style for a resource type.
func tagStyleFor(t core.ResourceType) lipgloss.Style {
	switch t {
	case core.RTypeBucket:
		return styleTagS3
	case core.RTypeFolder:
		return styleTagDir
	case core.RTypeObject:
		return styleTagObj
	case core.RTypeEcsService:
		return styleTagEcs
	case core.RTypeEcsTaskDefFamily:
		return styleTagTask
	default:
		return styleRowDim
	}
}

// padTag right-pads a tag label to a fixed width so names align.
// Example: "S3" -> "[S3  ]", "TASK" -> "[TASK]".
func padTag(label string) string {
	const width = 4
	out := label
	for len(out) < width {
		out += " "
	}
	return "[" + out + "]"
}
