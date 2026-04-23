package widget

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type KVRow struct {
	Label     string
	Value     string
	Clickable bool
	ClipValue string // clipboard content override when Value is styled
}

type KeyValue struct {
	Rows []KVRow
}

func (kv KeyValue) Render(width, height int) string {
	if len(kv.Rows) == 0 {
		return ""
	}
	labelW := 0
	for _, r := range kv.Rows {
		if w := lipgloss.Width(r.Label); w > labelW {
			labelW = w
		}
	}
	if labelW > width/3 {
		labelW = width / 3
	}
	var lines []string
	for _, r := range kv.Rows {
		label := padRight(r.Label, labelW)
		value := r.Value
		if r.Clickable {
			value = styleClickable.Render(value)
		}
		lines = append(lines, styleLabel.Render(label)+"  "+value)
	}
	return strings.Join(lines, "\n")
}

func (kv KeyValue) ClickableRegions() []ClickRegion {
	// Populated by the tui layer during render-with-coords; widgets
	// don't know their absolute frame position. Return nil here; the
	// render pipeline in tui/details.go attaches coords.
	return nil
}

func padRight(s string, w int) string {
	diff := w - lipgloss.Width(s)
	if diff <= 0 {
		return s
	}
	return s + strings.Repeat(" ", diff)
}
