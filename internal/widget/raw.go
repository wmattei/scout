package widget

type Raw struct {
	Content string
}

func (r Raw) Render(width, height int) string { return r.Content }
func (Raw) ClickableRegions() []ClickRegion   { return nil }
