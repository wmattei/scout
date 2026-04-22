// Package version exposes the running binary's version string. It lives
// in its own package (rather than in package main) so any package — the
// TUI, crash-log writer, debug log — can read it without importing
// main.
//
// The value is overridden at build time by GoReleaser via
//
//	-ldflags "-X github.com/wmattei/scout/internal/version.Current=v1.2.0"
//
// Unreleased local builds keep the "dev" default.
package version

import "strings"

// Current is the running binary's version. Do not mutate at runtime;
// treat as read-only after program start.
var Current = "dev"

// IsDev reports whether the running binary is a non-stable build. True
// when Current is "dev" (local build), the empty string (build that
// forgot to inject), or a semver string with a pre-release suffix
// (e.g. "v1.2.0-beta", "v1.2.0-rc.1").
func IsDev() bool {
	if Current == "" || Current == "dev" {
		return true
	}
	return strings.Contains(Current, "-")
}

// BannerText returns the label the TUI should render in the status-bar
// warning pill, or "" for stable releases. Format:
//
//	"DEV BUILD"                 — local unreleased build
//	"PRE-RELEASE v1.2.0-beta"   — tagged beta/rc/etc.
//	""                          — stable release (no banner)
func BannerText() string {
	switch {
	case Current == "" || Current == "dev":
		return "DEV BUILD"
	case strings.Contains(Current, "-"):
		return "PRE-RELEASE " + Current
	}
	return ""
}
