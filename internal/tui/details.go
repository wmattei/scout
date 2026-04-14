package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// renderDetails produces the full Details screen for the current
// detailsResource and actionSel. width/height are the frame bounds.
//
// Layout:
//
//	┌──────────────────────────────────┐
//	│ Details                          │
//	│                                  │
//	│   Name   <name>                  │
//	│   ARN    <arn>                   │
//	│                                  │
//	│ Actions                          │
//	│                                  │
//	│ ▸ 1. Open in Browser             │
//	│   2. Copy URI                    │
//	│   ...                            │
//	│                                  │
//	└──────────────────────────────────┘
//
// The function returns just the body rows — the caller composes them
// alongside the input bar, dividers, and status line in view.go.
func renderDetails(m Model, width int) string {
	r := m.detailsResource
	actionSel := m.actionSel

	var b strings.Builder

	b.WriteString(styleDetailsHeader.Render("Details"))
	b.WriteString("\n\n")

	writeField(&b, "Name", r.DisplayName)
	writeField(&b, "ARN", detailsARN(r, m))

	// Log group row when we've resolved task-def details and at least
	// one log group is configured. Uses the same family-lookup logic as
	// the Tail Logs action so services and task-def families share the
	// same row.
	if family := taskDefFamilyForDetails(m); family != "" {
		if d, ok := m.taskDefDetails[family]; ok && d != nil && len(d.LogGroups) > 0 {
			writeField(&b, "Log", d.LogGroups[0])
		}
	}
	b.WriteString("\n")

	b.WriteString(styleDetailsHeader.Render("Actions"))
	b.WriteString("\n\n")

	actions := ActionsFor(r.Type)
	if len(actions) == 0 {
		b.WriteString(styleRowDim.Render("  (no actions available)"))
		return b.String()
	}

	for i, a := range actions {
		indi := "  "
		if i == actionSel {
			indi = styleSelIndi.Render("▸ ")
		}
		line := fmt.Sprintf("%s%d. %s", indi, i+1, a.Label)
		if i == actionSel {
			b.WriteString(styleRowSel.Width(width).Render(line))
		} else {
			b.WriteString(line)
		}
		if i < len(actions)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// detailsARN resolves the ARN shown in the Details view. For task-def
// families it returns the lazily-resolved revision ARN if available;
// otherwise it falls back to "…resolving" or the family pseudo-ARN.
func detailsARN(r core.Resource, m Model) string {
	if r.Type != core.RTypeEcsTaskDefFamily {
		return r.ARN()
	}
	d, ok := m.taskDefDetails[r.Key]
	if !ok {
		return r.ARN()
	}
	if d == nil {
		return "…resolving"
	}
	return d.ARN
}

// writeField appends a single "  Label    Value" row to b.
func writeField(b *strings.Builder, label, value string) {
	b.WriteString("  ")
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 6)))
	b.WriteString(" ")
	b.WriteString(value)
	b.WriteString("\n")
}

// padRightPlain right-pads a string to n runes with ASCII spaces. Kept
// separate from padRight in results.go because that one operates on
// already-styled strings via lipgloss.Width.
func padRightPlain(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// styleDetailsHeader styles the "Details" / "Actions" section headers.
var styleDetailsHeader = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})

// styleDetailsLabel dims the field label so values read brighter.
var styleDetailsLabel = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
