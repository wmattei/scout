package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

// renderResults returns a string containing every visible row, one per line.
// selected is the index into results that is currently highlighted; if
// selected is out of range (e.g. empty list), the function returns an
// "empty state" message instead.
//
// width is the total rendered width (the frame width). The row layout is:
//
//   ▸ [TAG ] <highlighted name>     <right-aligned meta>
//
// The name segment takes whatever horizontal space is left after the
// indicator, tag, spacing, and meta columns.
func renderResults(results []search.Result, selected, width, height int, emptyMsg string) string {
	if len(results) == 0 {
		return centerEmptyState(width, height, emptyMsg)
	}

	const (
		indiWidth = 2 // "▸ " or "  "
		tagWidth  = 6 // "[S3  ]"
		gap       = 1 // space between columns
	)

	var b strings.Builder
	rows := height
	if rows > len(results) {
		rows = len(results)
	}
	// Scroll window: keep selection visible.
	start := 0
	if selected >= rows {
		start = selected - rows + 1
	}
	end := start + rows
	if end > len(results) {
		end = len(results)
	}

	for i := start; i < end; i++ {
		r := results[i]
		isSelected := i == selected

		// 1. Indicator.
		indi := "  "
		if isSelected {
			indi = styleSelIndi.Render("▸ ")
		}

		// 2. Tag — pulled from the per-type Provider so styles.go
		// no longer needs to know which colors belong to which
		// resource.
		tag := ""
		if p, ok := services.Get(r.Resource.Type); ok {
			tag = p.TagStyle().Render(padTag(p.TagLabel()))
		} else {
			tag = padTag(r.Resource.Type.Tag())
		}

		// 3. Meta (right-aligned).
		meta := renderMeta(r.Resource)

		// 4. Name (flex, with highlight spans).
		nameBudget := width - indiWidth - tagWidth - gap*2 - lipgloss.Width(meta)
		if nameBudget < 4 {
			nameBudget = 4
		}
		name := renderNameWithHighlights(r.Resource.DisplayName, r.MatchedRunes, nameBudget)

		line := fmt.Sprintf("%s%s %s %s", indi, tag, padRight(name, nameBudget), meta)
		if isSelected {
			line = styleRowSel.Width(width).Render(line)
		} else {
			line = styleRowBase.Width(width).Render(line)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	// Pad the remaining lines so the result area has a consistent height.
	for i := end - start; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderNameWithHighlights breaks name into matched / unmatched runs and
// applies styleHighlight to the matched positions. matchIdx is a sorted list
// of byte positions into DisplayName (from the fuzzy matcher).
func renderNameWithHighlights(name string, matchIdx []int, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	// Truncate to maxWidth runes with an ellipsis if needed. Tracking runes
	// not bytes so multi-byte characters render correctly in the terminal.
	runes := []rune(name)
	if len(runes) > maxWidth {
		runes = append(runes[:maxWidth-1], '…')
	}

	if len(matchIdx) == 0 {
		return string(runes)
	}

	// Build a set for O(1) lookup. Positions come from the fuzzy lib as
	// byte indexes, but sahilm/fuzzy is ASCII-friendly in practice; convert
	// byte positions to rune positions by walking the original string.
	byteToRune := make(map[int]int, len(name))
	runeIdx := 0
	for i := range name {
		byteToRune[i] = runeIdx
		runeIdx++
	}
	matched := make(map[int]bool, len(matchIdx))
	for _, bi := range matchIdx {
		if ri, ok := byteToRune[bi]; ok {
			matched[ri] = true
		}
	}

	var b strings.Builder
	for i, r := range runes {
		ch := string(r)
		if matched[i] {
			b.WriteString(styleHighlight.Render(ch))
		} else {
			b.WriteString(ch)
		}
	}
	return b.String()
}

// renderMeta produces the right-aligned meta column for a resource.
// Phase 1 shows region for buckets and cluster name for ecs services.
// Phase 2 adds mtime for folders and size + mtime for objects. Task def
// families have no meta yet.
func renderMeta(r core.Resource) string {
	switch r.Type {
	case core.RTypeBucket:
		return styleRowDim.Render(r.Meta["region"])
	case core.RTypeEcsService:
		return styleRowDim.Render(r.Meta["cluster"])
	case core.RTypeFolder:
		if ts, ok := r.Meta["mtime"]; ok && ts != "" {
			return styleRowDim.Render(formatUnixTime(ts))
		}
		return ""
	case core.RTypeObject:
		var parts []string
		if s, ok := r.Meta["size"]; ok && s != "" {
			parts = append(parts, formatBytes(s))
		}
		if ts, ok := r.Meta["mtime"]; ok && ts != "" {
			parts = append(parts, formatUnixTime(ts))
		}
		return styleRowDim.Render(strings.Join(parts, "  "))
	default:
		return ""
	}
}

// formatBytes turns a decimal byte-count string into a human-readable
// suffix ("12.4 MB"). Empty or unparseable input returns "".
func formatBytes(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n < 0 {
		return ""
	}
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// formatUnixTime turns a decimal Unix seconds string into a short
// "YYYY-MM-DD HH:MM" timestamp in the local timezone. Empty or
// unparseable input returns "".
func formatUnixTime(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return ""
	}
	return time.Unix(n, 0).Local().Format("2006-01-02 15:04")
}

// padRight pads s with spaces on the right so its visual width equals n.
// Uses lipgloss.Width so ANSI sequences don't break the count.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// centerEmptyState returns a body exactly `height` lines tall with the
// given message centered both vertically and horizontally. Used for all
// empty-list states in the search view.
//
// This replaces an earlier lipgloss.Place call that could produce an
// off-by-one line count when the message contained characters with
// ambiguous display width (e.g. braille spinners), pushing the surrounding
// frame out of alignment. Building the string by hand keeps line count
// deterministic regardless of the content.
func centerEmptyState(width, height int, msg string) string {
	if height <= 0 {
		return ""
	}
	styled := styleRowDim.Render(msg)
	// lipgloss.Width handles ANSI escapes; use it to compute visible width.
	msgWidth := lipgloss.Width(styled)
	leftPad := (width - msgWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	line := strings.Repeat(" ", leftPad) + styled

	// Split vertical padding: top gets the floor, bottom gets the rest so
	// odd-height blocks lean one row toward the top (matches how list
	// selectors usually look).
	top := (height - 1) / 2
	if top < 0 {
		top = 0
	}
	bottom := height - 1 - top
	if bottom < 0 {
		bottom = 0
	}

	var b strings.Builder
	for i := 0; i < top; i++ {
		b.WriteString("\n")
	}
	b.WriteString(line)
	for i := 0; i < bottom; i++ {
		b.WriteString("\n")
	}
	return b.String()
}
