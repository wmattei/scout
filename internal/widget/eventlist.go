package widget

import "strings"

type EventRow struct {
	Text         string
	ActivationID string
}

type EventList struct {
	Rows       []EventRow
	Selectable bool
	Selected   int // core sets this based on m.eventSel
	Focused    bool
}

func (el EventList) Render(width, height int) string {
	if len(el.Rows) == 0 {
		return ""
	}
	var lines []string
	for i, r := range el.Rows {
		line := r.Text
		if el.Selectable && el.Focused && i == el.Selected {
			line = styleRowSel.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (EventList) ClickableRegions() []ClickRegion { return nil }
