package prefs

import (
	"context"
	"fmt"
	"time"

	"github.com/wmattei/scout/internal/core"
)

// loadFavorites reads every row in the favorites table into s.
func (d *DB) loadFavorites(ctx context.Context, s *State) error {
	rows, err := d.sql.QueryContext(ctx,
		"SELECT package_id, row_key, display, created_at FROM favorites")
	if err != nil {
		return fmt.Errorf("loading favorites: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var packageID, rKey, display string
		var createdAt int64
		if err := rows.Scan(&packageID, &rKey, &display, &createdAt); err != nil {
			return fmt.Errorf("scanning favorite row: %w", err)
		}
		s.setFavorite(FavoriteRow{
			PackageID: packageID,
			RowKey:    rKey,
			Display:   display,
			CreatedAt: time.Unix(createdAt, 0),
		})
	}
	return rows.Err()
}

// SetFavorite inserts a new favorite entry for the given row. If it is
// already favorited, this is a no-op (both in SQLite via the ON
// CONFLICT clause, and in memory because setFavorite overwrites). Safe
// to call with the same row multiple times.
func (d *DB) SetFavorite(s *State, r core.Row) error {
	now := time.Now()
	_, err := d.sql.ExecContext(context.Background(), `
		INSERT INTO favorites (package_id, row_key, display, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(package_id, row_key) DO UPDATE SET
			display = excluded.display
	`, r.PackageID, r.Key, r.Name, now.Unix())
	if err != nil {
		return fmt.Errorf("set favorite: %w", err)
	}
	s.setFavorite(FavoriteRow{
		PackageID: r.PackageID,
		RowKey:    r.Key,
		Display:   r.Name,
		CreatedAt: now,
	})
	return nil
}

// UnsetFavorite removes the favorite entry for (packageID, rowKey).
// No-op if it isn't currently favorited.
func (d *DB) UnsetFavorite(s *State, packageID, rowK string) error {
	_, err := d.sql.ExecContext(context.Background(),
		"DELETE FROM favorites WHERE package_id = ? AND row_key = ?",
		packageID, rowK)
	if err != nil {
		return fmt.Errorf("unset favorite: %w", err)
	}
	s.unsetFavorite(packageID, rowK)
	return nil
}
