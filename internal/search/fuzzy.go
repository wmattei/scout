// Package search houses the fuzzy match engine used by the TUI to
// turn a query string into a ranked, highlight-annotated list of
// module rows.
package search

import (
	"github.com/sahilm/fuzzy"

	"github.com/wmattei/scout/internal/core"
)

// Result is one row in a search result list, with enough metadata for
// the TUI to render per-character highlight spans.
type Result struct {
	Row          core.Row
	MatchedRunes []int // byte positions in the name; empty for "no query"
	Score        int   // higher is better; 0 for "no query" baseline
}

// Fuzzy runs a fuzzy match against every row in `all` and returns the
// top `limit` results ordered by score (descending). An empty query
// returns the first `limit` rows with no highlight spans.
func Fuzzy(query string, all []core.Row, limit int) []Result {
	if query == "" {
		upto := minInt(limit, len(all))
		out := make([]Result, 0, upto)
		for i := 0; i < upto; i++ {
			out = append(out, Result{Row: all[i]})
		}
		return out
	}

	src := rowSource(all)
	matches := fuzzy.FindFrom(query, src)
	upto := minInt(limit, len(matches))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		m := matches[i]
		out = append(out, Result{
			Row:          all[m.Index],
			MatchedRunes: m.MatchedIndexes,
			Score:        m.Score,
		})
	}
	return out
}

type rowSource []core.Row

func (r rowSource) String(i int) string { return r[i].Name }
func (r rowSource) Len() int            { return len(r) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
