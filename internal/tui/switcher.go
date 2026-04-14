package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// switcherPane identifies which pane currently has keyboard focus
// inside the profile/region overlay.
type switcherPane int

const (
	switcherPaneProfile switcherPane = iota
	switcherPaneRegion
)

// Switcher holds all state for the profile/region overlay. When
// Visible is false the overlay doesn't render and key events fall
// through to the previous mode.
type Switcher struct {
	Visible bool

	// Data sources.
	profiles []string
	regions  []string

	// Filters (substring, case-insensitive). The overlay re-applies
	// the filter on every keystroke so the visible index is always
	// computed against the filtered slice.
	profileFilter string
	regionFilter  string

	// Selection indices are into the currently filtered slices.
	profileSel int
	regionSel  int

	// Focused pane.
	focused switcherPane
}

// newSwitcher constructs a hidden Switcher seeded with data sources.
// currentProfile and currentRegion pre-select the user's current
// context so Enter without moving commits a no-op (and cancels the
// overlay rather than triggering a costly refresh).
func newSwitcher(currentProfile, currentRegion string) Switcher {
	profiles := awsctx.ListProfiles()
	if len(profiles) == 0 {
		// Fall back to whatever the current context is so the user at
		// least sees one row.
		profiles = []string{currentProfile}
	}
	regions := append([]string{}, awsctx.CommonRegions...)
	if !containsString(regions, currentRegion) {
		regions = append([]string{currentRegion}, regions...)
	}

	s := Switcher{
		profiles: profiles,
		regions:  regions,
		focused:  switcherPaneProfile,
	}
	s.profileSel = indexOf(profiles, currentProfile)
	s.regionSel = indexOf(regions, currentRegion)
	if s.profileSel < 0 {
		s.profileSel = 0
	}
	if s.regionSel < 0 {
		s.regionSel = 0
	}
	return s
}

// Show makes the switcher visible without resetting filters.
func (s *Switcher) Show() { s.Visible = true }

// Hide closes the overlay without committing.
func (s *Switcher) Hide() { s.Visible = false }

// filteredProfiles applies profileFilter and returns the visible slice
// plus a parallel slice of original indices into s.profiles so the
// caller can resolve the selection back to a real profile name.
func (s Switcher) filteredProfiles() ([]string, []int) {
	return applyFilter(s.profiles, s.profileFilter)
}

// filteredRegions mirrors filteredProfiles.
func (s Switcher) filteredRegions() ([]string, []int) {
	return applyFilter(s.regions, s.regionFilter)
}

// selectedProfile returns the profile name currently under the cursor,
// or "" if the filter matches nothing.
func (s Switcher) selectedProfile() string {
	vals, _ := s.filteredProfiles()
	if s.profileSel < 0 || s.profileSel >= len(vals) {
		return ""
	}
	return vals[s.profileSel]
}

// selectedRegion returns the region currently under the cursor, or ""
// if the filter matches nothing.
func (s Switcher) selectedRegion() string {
	vals, _ := s.filteredRegions()
	if s.regionSel < 0 || s.regionSel >= len(vals) {
		return ""
	}
	return vals[s.regionSel]
}

// applyFilter returns the subset of values whose lowercased form
// contains the lowercased filter, plus each match's original index.
func applyFilter(values []string, filter string) ([]string, []int) {
	if filter == "" {
		idxs := make([]int, len(values))
		for i := range values {
			idxs[i] = i
		}
		return values, idxs
	}
	low := strings.ToLower(filter)
	out := make([]string, 0, len(values))
	idxs := make([]int, 0, len(values))
	for i, v := range values {
		if strings.Contains(strings.ToLower(v), low) {
			out = append(out, v)
			idxs = append(idxs, i)
		}
	}
	return out, idxs
}

// renderSwitcher draws the overlay body. Called from view.go when
// m.switcher.Visible is true. The returned string is exactly `height`
// lines tall so it slots into the frame in place of the normal body.
func renderSwitcher(s Switcher, width, height int) string {
	if width < 50 {
		return centerEmptyState(width, height, "terminal too narrow for switcher")
	}

	header := styleDetailsHeader.Render("Switch AWS context")
	help := styleRowDim.Render("Tab switch pane    ↑/↓ select    Enter commit    Esc cancel")

	profileTitle := "Profile"
	regionTitle := "Region"
	if s.focused == switcherPaneProfile {
		profileTitle = "▸ " + profileTitle
	} else {
		regionTitle = "▸ " + regionTitle
	}

	paneWidth := (width - 6) / 2
	profileList := renderSwitcherPane(profileTitle, s.profileFilter, s.profiles, s.profileFilter, s.profileSel, s.focused == switcherPaneProfile, paneWidth)
	regionList := renderSwitcherPane(regionTitle, s.regionFilter, s.regions, s.regionFilter, s.regionSel, s.focused == switcherPaneRegion, paneWidth)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, profileList, "  ", regionList)

	body := strings.Join([]string{
		header,
		"",
		panes,
		"",
		help,
	}, "\n")

	return padBlock(body, height)
}

// renderSwitcherPane builds one pane of the overlay. Shows the pane
// title, the filter input (with a live caret when focused), and up to
// 12 visible rows of the filtered slice with the current selection
// highlighted.
func renderSwitcherPane(title, _ string, values []string, filter string, sel int, focused bool, width int) string {
	const maxRows = 12

	vals, _ := applyFilter(values, filter)

	var b strings.Builder
	b.WriteString(styleDetailsHeader.Render(title))
	b.WriteString("\n")

	filterLine := "filter: " + filter
	if focused {
		filterLine += "█"
	}
	b.WriteString(styleRowDim.Render(filterLine))
	b.WriteString("\n\n")

	if len(vals) == 0 {
		b.WriteString(styleRowDim.Render("  (no matches)"))
		return padPaneToHeight(b.String(), width, maxRows+4)
	}

	start := 0
	if sel >= maxRows {
		start = sel - maxRows + 1
	}
	end := start + maxRows
	if end > len(vals) {
		end = len(vals)
	}

	for i := start; i < end; i++ {
		indi := "  "
		line := vals[i]
		if i == sel {
			indi = styleSelIndi.Render("▸ ")
			line = styleRowSel.Width(width).Render(indi + line)
		} else {
			line = indi + line
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return padPaneToHeight(b.String(), width, maxRows+4)
}

// padPaneToHeight pads a pane string with blank lines until it has
// exactly `rows` lines so both panes align vertically in
// JoinHorizontal regardless of how many rows each filter matched.
func padPaneToHeight(s string, _, rows int) string {
	lines := strings.Count(s, "\n") + 1
	if lines >= rows {
		return s
	}
	return s + strings.Repeat("\n", rows-lines)
}

// containsString is a small helper for the region-list seeding logic.
func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

// indexOf returns the first index of target in xs, or -1.
func indexOf(xs []string, target string) int {
	for i, x := range xs {
		if x == target {
			return i
		}
	}
	return -1
}
