// Package version exposes the running binary's version string. It lives
// in its own package (rather than in package main) so any package — the
// TUI, crash-log writer, debug log — can read it without importing
// main.
//
// The value is overridden at build time by GoReleaser via
//
//	-ldflags "-X github.com/wmattei/scout/internal/version.Current=v0.2.0"
//
// Unreleased local builds keep the "dev" default so IsDev reports true.
package version

// Current is the running binary's version. Do not mutate at runtime;
// treat as read-only after program start.
var Current = "dev"

// IsDev reports whether the running binary is a non-release build.
// True when Current is the default "dev" or the empty string (which
// would indicate a build that forgot to inject a version).
func IsDev() bool {
	return Current == "dev" || Current == ""
}
