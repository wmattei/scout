package widget

import "github.com/charmbracelet/lipgloss"

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	styleLabel     = lipgloss.NewStyle().Foreground(ac("#767676", "#8A8A8A"))
	styleValue     = lipgloss.NewStyle()
	styleClickable = lipgloss.NewStyle().Foreground(ac("#005FAF", "#5FD7FF")).Underline(true)
	styleDim       = lipgloss.NewStyle().Foreground(ac("#767676", "#8A8A8A"))
	styleRowSel    = lipgloss.NewStyle().Background(ac("#D0D0FF", "#2A2A5A"))

	pillInfo    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(ac("#FFFFFF", "#FFFFFF")).Background(ac("#005F87", "#005F87"))
	pillSuccess = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(ac("#FFFFFF", "#FFFFFF")).Background(ac("#005F00", "#005F00"))
	pillWarn    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(ac("#FFFFFF", "#FFFFFF")).Background(ac("#875F00", "#875F00"))
	pillError   = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(ac("#FFFFFF", "#FFFFFF")).Background(ac("#870000", "#870000"))
)

var _ = styleValue
var _ = styleDim
