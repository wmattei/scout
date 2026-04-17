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
		"SELECT type, key, name, created_at FROM favorites")
	if err != nil {
		return fmt.Errorf("loading favorites: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var typeStr, key, name string
		var createdAt int64
		if err := rows.Scan(&typeStr, &key, &name, &createdAt); err != nil {
			return fmt.Errorf("scanning favorite row: %w", err)
		}
		s.setFavorite(FavoriteRow{
			Type:      parseType(typeStr),
			Key:       key,
			Name:      name,
			CreatedAt: time.Unix(createdAt, 0),
		})
	}
	return rows.Err()
}

// SetFavorite inserts a new favorite entry for the given resource. If
// it is already favorited, this is a no-op (both in SQLite via the
// ON CONFLICT clause, and in memory because setFavorite overwrites).
// Safe to call with the same resource multiple times.
func (d *DB) SetFavorite(s *State, r core.Resource) error {
	now := time.Now()
	_, err := d.sql.ExecContext(context.Background(), `
		INSERT INTO favorites (type, key, name, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(type, key) DO UPDATE SET
			name = excluded.name
	`, r.Type.String(), r.Key, r.DisplayName, now.Unix())
	if err != nil {
		return fmt.Errorf("set favorite: %w", err)
	}
	s.setFavorite(FavoriteRow{
		Type:      r.Type,
		Key:       r.Key,
		Name:      r.DisplayName,
		CreatedAt: now,
	})
	return nil
}

// UnsetFavorite removes the favorite entry for (t, key). No-op if it
// isn't currently favorited.
func (d *DB) UnsetFavorite(s *State, t core.ResourceType, key string) error {
	_, err := d.sql.ExecContext(context.Background(),
		"DELETE FROM favorites WHERE type = ? AND key = ?",
		t.String(), key)
	if err != nil {
		return fmt.Errorf("unset favorite: %w", err)
	}
	s.unsetFavorite(t, key)
	return nil
}
