package tui

import (
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
)

// partitionByFavorites splits `results` into two buckets — favorites
// first, non-favorites second — while preserving the relative order
// inside each bucket. Given that search.Fuzzy returns results sorted
// by score descending, this produces a "favorites-first, otherwise
// fuzzy-score" ranking.
//
// When state is nil (prefs unavailable), the input slice is returned
// unchanged. Allocates a fresh slice only when at least one result
// matches a favorite so the common-case is cheap.
func partitionByFavorites(results []search.Result, state *prefs.State) []search.Result {
	if state == nil || len(results) == 0 {
		return results
	}

	// Fast path: count favorites. If there are none, return as-is.
	favCount := 0
	for _, r := range results {
		if state.IsFavorite(r.Resource.Type, r.Resource.Key) {
			favCount++
		}
	}
	if favCount == 0 || favCount == len(results) {
		return results
	}

	favs := make([]search.Result, 0, favCount)
	rest := make([]search.Result, 0, len(results)-favCount)
	for _, r := range results {
		if state.IsFavorite(r.Resource.Type, r.Resource.Key) {
			favs = append(favs, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(favs, rest...)
}
