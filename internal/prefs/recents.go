package prefs

import (
	"context"
	"fmt"
	"time"

	"github.com/wmattei/scout/internal/core"
)

// loadRecents reads every row in the recents table into s, ordered
// newest-first and capped at recentsCap.
func (d *DB) loadRecents(ctx context.Context, s *State) error {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT type, key, name, visited_at
		FROM recents
		ORDER BY visited_at DESC
		LIMIT ?`,
		recentsCap,
	)
	if err != nil {
		return fmt.Errorf("loading recents: %w", err)
	}
	defer rows.Close()

	var loaded []RecentRow
	for rows.Next() {
		var typeStr, key, name string
		var visitedAt int64
		if err := rows.Scan(&typeStr, &key, &name, &visitedAt); err != nil {
			return fmt.Errorf("scanning recent row: %w", err)
		}
		loaded = append(loaded, RecentRow{
			Type:      parseType(typeStr),
			Key:       key,
			Name:      name,
			VisitedAt: time.Unix(visitedAt, 0),
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.recents = loaded
	s.mu.Unlock()
	return nil
}

// MarkVisited upserts the resource into the recents table with
// visited_at=now, then deletes any rows outside the top recentsCap so
// the table never grows beyond the visible cap. Updates the in-memory
// State on success.
func (d *DB) MarkVisited(s *State, r core.Resource) error {
	now := time.Now()

	tx, err := d.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin mark visited: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO recents (type, key, name, visited_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(type, key) DO UPDATE SET
			name = excluded.name,
			visited_at = excluded.visited_at
	`, r.Type.String(), r.Key, r.DisplayName, now.Unix()); err != nil {
		return fmt.Errorf("upsert recent: %w", err)
	}

	// Prune: keep only the top recentsCap rows by visited_at.
	if _, err := tx.ExecContext(context.Background(), `
		DELETE FROM recents
		WHERE rowid NOT IN (
			SELECT rowid FROM recents ORDER BY visited_at DESC LIMIT ?
		)
	`, recentsCap); err != nil {
		return fmt.Errorf("prune recents: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark visited: %w", err)
	}

	s.markVisited(RecentRow{
		Type:      r.Type,
		Key:       r.Key,
		Name:      r.DisplayName,
		VisitedAt: now,
	})
	return nil
}
