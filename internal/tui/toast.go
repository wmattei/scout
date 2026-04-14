package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Toast is a transient bottom-centered overlay displayed over whatever
// screen is currently rendered. Phase 2 only shows toasts for action
// stubs; Phase 4 re-uses the same machinery for AWS errors and credential
// failures.
//
// A zero-valued Toast is "inactive": renderToast returns "" and the view
// layer skips the overlay.
type Toast struct {
	Message   string
	ExpiresAt time.Time
}

// newToast returns a Toast that expires after `dur` from now.
func newToast(message string, dur time.Duration) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(dur),
	}
}

// isActive reports whether the toast should currently render.
func (t Toast) isActive() bool {
	return t.Message != "" && time.Now().Before(t.ExpiresAt)
}

// renderToast returns a single-line overlay string centered horizontally,
// or "" if the toast is inactive. The caller is responsible for composing
// this into the final frame (replacing the bottom divider for the toast's
// lifetime). width is the full frame width.
func renderToast(t Toast, width int) string {
	if !t.isActive() {
		return ""
	}
	const padding = 2
	msg := t.Message
	inner := " " + msg + " "
	if lipglossWidth(inner) > width-padding {
		inner = inner[:width-padding-1] + "…"
	}
	boxed := styleToast.Render(inner)
	left := (width - lipglossWidth(boxed)) / 2
	if left < 0 {
		left = 0
	}
	return strings.Repeat(" ", left) + boxed
}

// styleToast is defined here rather than in styles.go to keep the toast
// component fully self-contained — it is the only consumer. Phase 4 can
// promote it to styles.go once error-surface toasts share the look.
var styleToast = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
	Background(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#5F005F"}).
	Padding(0, 1)
