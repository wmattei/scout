package prefs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// schemaVersion is bumped whenever the DDL changes. Like the module
// cache, prefs is recreated (drop+create) on mismatch because v0 has
// no user data anywhere yet. Future bumps with real user data will
// introduce a proper migration.
//
// v2 rekeys favorites + recents by (package_id, row_key) — the legacy
// (type, key) pair tied to the retired core.ResourceType enum is gone.
const schemaVersion = 2

const schemaSQL = `
CREATE TABLE IF NOT EXISTS favorites (
  package_id  TEXT    NOT NULL,
  row_key     TEXT    NOT NULL,
  display     TEXT    NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (package_id, row_key)
);

CREATE TABLE IF NOT EXISTS recents (
  package_id  TEXT    NOT NULL,
  row_key     TEXT    NOT NULL,
  display     TEXT    NOT NULL,
  visited_at  INTEGER NOT NULL,
  PRIMARY KEY (package_id, row_key)
);
CREATE INDEX IF NOT EXISTS recents_visited_at ON recents(visited_at DESC);

CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
`

// DB wraps the SQLite handle for one (profile, region) pair's prefs
// file.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the prefs DB for the given profile/region
// pair, ensures the schema is at the expected version (recreating on
// mismatch), and returns both the handle and a freshly loaded
// in-memory State.
func Open(profile, region string) (*DB, *State, error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating cache dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s__%s__prefs.db", profile, region))

	db, err := openAt(path)
	if err != nil {
		return nil, nil, err
	}

	current, err := readSchemaVersion(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if current != schemaVersion {
		_ = db.Close()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("dropping stale prefs %s: %w", path, err)
		}
		db, err = openAt(path)
		if err != nil {
			return nil, nil, err
		}
	}

	if err := writeSchemaVersion(db); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	d := &DB{sql: db}
	state := newState()

	if err := d.loadFavorites(context.Background(), state); err != nil {
		_ = d.Close()
		return nil, nil, err
	}
	if err := d.loadRecents(context.Background(), state); err != nil {
		_ = d.Close()
		return nil, nil, err
	}
	return d, state, nil
}

// Close releases the underlying sqlite handle. Safe to call on nil.
func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

func openAt(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying prefs schema: %w", err)
	}
	return db, nil
}

func readSchemaVersion(db *sql.DB) (int, error) {
	var v string
	err := db.QueryRowContext(context.Background(), "SELECT v FROM meta WHERE k = 'schema_version'").Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading prefs schema_version: %w", err)
	}
	var n int
	_, _ = fmt.Sscanf(v, "%d", &n)
	return n, nil
}

func writeSchemaVersion(db *sql.DB) error {
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO meta(k, v) VALUES('schema_version', ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v",
		fmt.Sprintf("%d", schemaVersion),
	)
	if err != nil {
		return fmt.Errorf("writing prefs schema_version: %w", err)
	}
	return nil
}

// cacheDir mirrors internal/cache.DBPath's directory logic.
// Duplicated to keep the prefs package dependency-free.
func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "scout"), nil
}
