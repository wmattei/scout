package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
	"github.com/wmattei/scout/internal/services"
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
func renderResults(results []search.Result, selected, width, height int, emptyMsg string, favs *prefs.State, registry *module.Registry) string {
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

		// 2-4. Tag / meta / display name — module rows go through the
		// module registry; legacy rows go through the services
		// provider registry.
		var (
			tag        string
			meta       string
			displayRaw string
		)
		if r.ModuleRow != nil {
			tag, meta, displayRaw = moduleRowDisplay(r.ModuleRow, registry)
		} else {
			if p, ok := services.Get(r.Resource.Type); ok {
				tag = p.TagStyle().Render(padTag(p.TagLabel()))
			} else {
				tag = padTag("???")
			}
			meta = renderMeta(r.Resource)
			displayRaw = r.Resource.DisplayName
		}

		nameBudget := width - indiWidth - tagWidth - gap*2 - lipgloss.Width(meta)
		if nameBudget < 4 {
			nameBudget = 4
		}
		starPrefix := ""
		isFav := false
		if favs != nil {
			if r.ModuleRow != nil {
				syn := resourceFromRow(*r.ModuleRow)
				isFav = favs.IsFavorite(syn.Type, syn.Key)
			} else {
				isFav = favs.IsFavorite(r.Resource.Type, r.Resource.Key)
			}
		}
		if isFav {
			starPrefix = "★ "
			nameBudget -= lipgloss.Width(starPrefix)
			if nameBudget < 4 {
				nameBudget = 4
			}
		}
		name := renderNameWithHighlights(displayRaw, r.MatchedRunes, nameBudget)

		line := fmt.Sprintf("%s%s %s%s %s", indi, tag, starPrefix, padRight(name, nameBudget), meta)
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

// moduleRowDisplay builds (tag, meta, displayName) for a module row.
// Tag is styled via the module Manifest's TagStyle when the module
// is registered; falls back to an unstyled uppercase PackageID if not.
func moduleRowDisplay(r *core.Row, registry *module.Registry) (tag, meta, displayName string) {
	displayName = r.Name
	if registry != nil {
		if mod, ok := registry.Get(r.PackageID); ok {
			mani := mod.Manifest()
			return mani.TagStyle.Render(padTag(mani.Tag)), "", displayName
		}
	}
	return padTag(strings.ToUpper(r.PackageID)), "", displayName
}

// renderMeta returns the dim-styled meta column for a result row,
// pulled from the per-type Provider. Providers return plain strings
// — this function owns the styleRowDim wrapping so colors stay
// centralized in the styles file.
func renderMeta(r core.Resource) string {
	p, ok := services.Get(r.Type)
	if !ok {
		return ""
	}
	plain := p.RenderMeta(r)
	if plain == "" {
		return ""
	}
	return styleRowDim.Render(plain)
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
