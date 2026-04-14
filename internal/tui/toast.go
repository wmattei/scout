package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ToastLevel tags a toast as informational or as an error. Errors get
// the red style variant and a slightly longer default lifetime so the
// user can actually read the message.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastError
)

// Toast is a transient bottom-centered overlay displayed over whatever
// screen is currently rendered. A zero-valued Toast is "inactive":
// renderToast returns "" and the view layer skips the overlay.
type Toast struct {
	Message   string
	ExpiresAt time.Time
	Level     ToastLevel
}

// newToast returns an info-level Toast that expires after dur.
func newToast(message string, dur time.Duration) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(dur),
		Level:     ToastInfo,
	}
}

// newErrorToast returns an error-level Toast that stays up for 6s by
// default so the user has time to read it.
func newErrorToast(message string) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(6 * time.Second),
		Level:     ToastError,
	}
}

// isActive reports whether the toast should currently render.
func (t Toast) isActive() bool {
	return t.Message != "" && time.Now().Before(t.ExpiresAt)
}

// renderToast returns a single-line overlay string centered horizontally,
// or "" if the toast is inactive. Errors render in a red style; info
// toasts use the default purple style. width is the full frame width.
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
	style := styleToast
	if t.Level == ToastError {
		style = styleToastError
	}
	boxed := style.Render(inner)
	left := (width - lipglossWidth(boxed)) / 2
	if left < 0 {
		left = 0
	}
	return strings.Repeat(" ", left) + boxed
}

// styleToast is the default (info) toast look. styleToastError is
// declared in styles.go alongside the rest of the palette.
var styleToast = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
	Background(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#5F005F"}).
	Padding(0, 1)
