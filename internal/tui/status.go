package tui

import (
	"fmt"
	"strings"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/version"
)

// spinnerFrames is a simple braille-dot spinner. Index % len picks a frame.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame(tick int) string { return spinnerFrames[tick%len(spinnerFrames)] }

// renderStatus composes the bottom status bar: profile, region, account,
// and activity indicator. width is the full frame width; the returned
// string is exactly one line tall and exactly `width` columns wide.
func renderStatus(width int, profile, region, account string, activity awsctx.ActivitySnapshot, tick int) string {
	// Non-stable-build banner. version.BannerText returns "" for stable
	// releases (clean status bar), "DEV BUILD" for local builds, and
	// "PRE-RELEASE v1.2.0-beta" for tagged pre-releases. The pill sits
	// at the leading edge so it's the first thing the eye catches.
	leadingBanner := ""
	if label := version.BannerText(); label != "" {
		leadingBanner = styleDevBanner.Render(label) + " "
	}

	left := ""
	if profile == "" && region == "" {
		// Onboarding / pre-resolve state — no AWS context yet.
		left = styleRowDim.Render("no AWS context — Ctrl+P to pick a profile")
	} else {
		left = fmt.Sprintf("profile=%s  region=%s", profile, region)
		if account != "" {
			left += fmt.Sprintf("  acct=%s", account)
		}
	}

	right := ""
	switch {
	case activity.InFlight > 1:
		right = fmt.Sprintf("%s %d calls…", styleSpinner.Render(spinnerFrame(tick)), activity.InFlight)
	case activity.InFlight == 1:
		op := activity.LastOp
		if op == "" {
			op = "…"
		}
		right = fmt.Sprintf("%s %s", styleSpinner.Render(spinnerFrame(tick)), op)
	}

	gap := width - visibleWidth(leadingBanner) - visibleWidth(left) - visibleWidth(right) - 2 // -2 for padding
	if gap < 1 {
		gap = 1
	}
	line := " " + leadingBanner + left + strings.Repeat(" ", gap) + right + " "
	return styleStatusBar.Width(width).Render(line)
}

// visibleWidth is a tiny shim so tests can swap in fake width logic if needed.
func visibleWidth(s string) int {
	// lipgloss's Width handles ANSI escapes.
	return lipglossWidth(s)
}
