package tui

// Mode represents which top-level screen the bubbletea program is
// currently showing. Phase 2 introduces the first mode split: search
// versus details. Phase 4 will add the profile/region switcher overlay.
type Mode int

const (
	// modeSearch is the default — input bar + result list. The input bar
	// doubles as a breadcrumb for scoped S3 navigation.
	modeSearch Mode = iota

	// modeDetails shows the Details panel + Actions list for a selected
	// resource. All actions are stubbed in Phase 2 and surface a toast
	// on activation; Phase 3 implements them for real.
	modeDetails

	// modeTailLogs shows the full-screen streaming log viewport backed
	// by CloudWatch Logs Live Tail.
	modeTailLogs
)

// String returns a short debug name for the mode.
func (m Mode) String() string {
	switch m {
	case modeSearch:
		return "search"
	case modeDetails:
		return "details"
	case modeTailLogs:
		return "tail-logs"
	default:
		return "unknown"
	}
}
