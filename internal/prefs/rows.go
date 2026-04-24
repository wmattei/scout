// Package prefs owns the per-(profile, region) user preferences store:
// favorites (rows the user pinned with `f`) and recents (the last-10
// rows whose Details view was entered). Stored in a separate SQLite
// file from the module resource cache so `scout cache clear` doesn't
// wipe user state.
package prefs

import "time"

// rowKey is the primary-key composite used internally by State maps
// so different modules can't collide on the same row Key.
type rowKey struct {
	PackageID string
	RowKey    string
}

// FavoriteRow is a single row in the favorites list returned to the
// TUI. Display is the row.Name snapshot taken at insert time so the
// UI can render a row even when the live module cache hasn't yet
// populated that row (e.g. cache not yet populated, or resource
// deleted from AWS).
type FavoriteRow struct {
	PackageID string
	RowKey    string
	Display   string
	CreatedAt time.Time
}

// RecentRow is a single row in the recents list. Same fields as
// FavoriteRow plus the visit timestamp; the TUI orders recents by
// VisitedAt descending.
type RecentRow struct {
	PackageID string
	RowKey    string
	Display   string
	VisitedAt time.Time
}
