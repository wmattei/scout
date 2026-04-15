package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/services"
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

	if p, ok := services.Get(r.Type); ok {
		if group := p.LogGroup(r, m.lazyDetailsFor(r)); group != "" {
			writeField(&b, "Log", group)
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

// detailsARN resolves the ARN shown in the Details view. It checks the
// generic lazy-details store keyed by (type, resource key): if
// resolution is in-flight it shows "…resolving"; if resolved it returns
// the familyArn from the map (if present); otherwise it falls back to
// the provider's ARN and then to core.Resource.ARN().
func detailsARN(r core.Resource, m Model) string {
	key := lazyDetailKey{Type: r.Type, Key: r.Key}
	state := m.lazyDetailsState[key]
	switch state {
	case lazyStateInFlight:
		return "…resolving"
	case lazyStateResolved:
		if lazy := m.lazyDetails[key]; lazy != nil {
			if a := lazy["familyArn"]; a != "" {
				return a
			}
		}
	}
	if p, ok := services.Get(r.Type); ok {
		if a := p.ARN(r); a != "" {
			return a
		}
	}
	return r.ARN()
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
