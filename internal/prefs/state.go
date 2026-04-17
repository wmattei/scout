package prefs

import (
	"sort"
	"sync"

	"github.com/wmattei/scout/internal/core"
)

// State is the TUI's read view of the prefs DB. It is populated by
// Open() and mutated in-place by DB.SetFavorite / UnsetFavorite /
// MarkVisited alongside the backing SQLite writes.
//
// All accessors are safe for concurrent reads; mutations are single-
// writer through the DB type (the TUI's Update loop is single-
// goroutine, so contention is rare — the RWMutex is a belt-and-
// suspenders measure).
type State struct {
	mu        sync.RWMutex
	favorites map[typeKey]FavoriteRow
	recents   []RecentRow // sorted newest-first, capped at recentsCap
}

const recentsCap = 10

// newState returns an empty, zero-value State.
func newState() *State {
	return &State{favorites: make(map[typeKey]FavoriteRow)}
}

// IsFavorite reports whether the (type, key) pair is in the favorites
// set. Lock-free for callers; takes an RLock internally.
func (s *State) IsFavorite(t core.ResourceType, key string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.favorites[typeKey{Type: t, Key: key}]
	return ok
}

// Favorites returns a freshly allocated slice of favorite rows
// sorted by CreatedAt descending (newest first). Callers may sort,
// filter, or mutate the returned slice freely.
func (s *State) Favorites() []FavoriteRow {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FavoriteRow, 0, len(s.favorites))
	for _, r := range s.favorites {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Key < out[j].Key
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Recents returns a copy of the recents slice (already sorted newest-
// first). Nil-safe.
func (s *State) Recents() []RecentRow {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecentRow, len(s.recents))
	copy(out, s.recents)
	return out
}

// setFavorite writes to the map under the write lock.
func (s *State) setFavorite(row FavoriteRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.favorites[typeKey{Type: row.Type, Key: row.Key}] = row
}

// unsetFavorite removes the entry under the write lock.
func (s *State) unsetFavorite(t core.ResourceType, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.favorites, typeKey{Type: t, Key: key})
}

// markVisited moves the given row to position 0 of the recents slice
// (or inserts it there) and trims to recentsCap.
func (s *State) markVisited(row RecentRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Remove any existing entry for the same (type, key).
	key := typeKey{Type: row.Type, Key: row.Key}
	next := s.recents[:0]
	for _, r := range s.recents {
		if (typeKey{Type: r.Type, Key: r.Key}) == key {
			continue
		}
		next = append(next, r)
	}
	// Prepend the new row and cap.
	s.recents = append([]RecentRow{row}, next...)
	if len(s.recents) > recentsCap {
		s.recents = s.recents[:recentsCap]
	}
}
