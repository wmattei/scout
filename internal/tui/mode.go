package tui

// Mode represents which top-level screen the bubbletea program is
// currently showing.
type Mode int

const (
	// modeSearch is the default — input bar + result list. The input bar
	// doubles as a breadcrumb for scoped S3 navigation.
	modeSearch Mode = iota

	// modeDetails shows the Details panel + Actions list for a selected
	// resource. All actions have real implementations.
	modeDetails

	// modeTailLogs shows the full-screen streaming log viewport backed
	// by CloudWatch Logs Live Tail.
	modeTailLogs

	// modeSwitcher shows the profile/region overlay. Key events are
	// routed to updateSwitcher; Esc restores the previous mode.
	modeSwitcher

	// modeOnboarding is the fallback screen shown when scout starts
	// without a resolvable AWS context. If the user has profiles
	// configured we invite them to press Ctrl+P and pick one; if not,
	// we show setup instructions so they can finish configuring AWS
	// without leaving the TUI.
	modeOnboarding
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
	case modeSwitcher:
		return "switcher"
	case modeOnboarding:
		return "onboarding"
	default:
		return "unknown"
	}
}
