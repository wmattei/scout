package widget

import "github.com/wmattei/scout/internal/effect"

type StatusPill struct {
	Text  string
	Level effect.Level
}

func (p StatusPill) Render(width, height int) string {
	switch p.Level {
	case effect.LevelSuccess:
		return pillSuccess.Render(p.Text)
	case effect.LevelWarning:
		return pillWarn.Render(p.Text)
	case effect.LevelError:
		return pillError.Render(p.Text)
	default:
		return pillInfo.Render(p.Text)
	}
}

func (StatusPill) ClickableRegions() []ClickRegion { return nil }
