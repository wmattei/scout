// Package widget provides composable building blocks modules use to
// fill the five detail zones. Core renders zone chrome; widgets
// render the inner content.
package widget

// ClickRegion is a rectangular hit box inside a rendered widget,
// returned to core so copy-on-click participates in the details
// hit-map.
type ClickRegion struct {
	X0, Y0, X1, Y1 int
	Clipboard      string
	Label          string
}

// Block is the common widget contract. Zones collapse when Render
// returns "". ClickableRegions may return nil.
type Block interface {
	Render(width, height int) string
	ClickableRegions() []ClickRegion
}

// Empty is the zero-content block — zones that receive it collapse.
type Empty struct{}

func (Empty) Render(int, int) string          { return "" }
func (Empty) ClickableRegions() []ClickRegion { return nil }
