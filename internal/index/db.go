// Package index owns the on-disk SQLite cache and the in-memory index that
// serves the TUI. DBs are one file per (profile, region) pair, living under
// $XDG_CACHE_HOME/better-aws (fallback: $HOME/.cache/better-aws).
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// schemaVersion is bumped whenever the DDL changes. The cache is rebuildable,
// so mismatches trigger a drop+recreate rather than a migration.
const schemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS resources (
  type        TEXT NOT NULL,
  key         TEXT NOT NULL,
  name        TEXT NOT NULL,
  meta_json   TEXT NOT NULL,
  indexed_at  INTEGER NOT NULL,
  PRIMARY KEY (type, key)
);
CREATE INDEX IF NOT EXISTS resources_type ON resources(type);

CREATE TABLE IF NOT EXISTS bucket_contents (
  bucket     TEXT NOT NULL,
  key        TEXT NOT NULL,
  is_folder  INTEGER NOT NULL,
  size       INTEGER,
  mtime      INTEGER,
  PRIMARY KEY (bucket, key)
);
CREATE INDEX IF NOT EXISTS bucket_contents_bucket_key ON bucket_contents(bucket, key);

CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
`

// DB wraps a SQLite handle scoped to a single (profile, region) pair.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the cache DB for the given profile/region pair.
// It ensures the schema exists and matches the current version, recreating
// the file from scratch if the version is out of date.
func Open(profile, region string) (*DB, error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s__%s.db", profile, region))

	db, err := openAt(path)
	if err != nil {
		return nil, err
	}

	current, err := readSchemaVersion(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if current != schemaVersion {
		_ = db.Close()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("dropping stale cache %s: %w", path, err)
		}
		db, err = openAt(path)
		if err != nil {
			return nil, err
		}
	}

	if err := writeSchemaVersion(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{sql: db}, nil
}

func openAt(path string) (*sql.DB, error) {
	// modernc.org/sqlite accepts query params via ?_pragma=...
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single-writer model keeps things simple
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
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
		return 0, fmt.Errorf("reading schema_version: %w", err)
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
	return err
}

// Close releases the underlying sqlite handle.
func (d *DB) Close() error { return d.sql.Close() }

// LoadAll returns every row in the resources table. Used on startup to build
// the in-memory index for instant first paint.
func (d *DB) LoadAll(ctx context.Context) ([]core.Resource, error) {
	rows, err := d.sql.QueryContext(ctx, "SELECT type, key, name, meta_json FROM resources")
	if err != nil {
		return nil, fmt.Errorf("loading resources: %w", err)
	}
	defer rows.Close()

	var out []core.Resource
	for rows.Next() {
		var typeStr, key, name, metaJSON string
		if err := rows.Scan(&typeStr, &key, &name, &metaJSON); err != nil {
			return nil, fmt.Errorf("scanning resource row: %w", err)
		}
		r := core.Resource{
			Type:        parseType(typeStr),
			Key:         key,
			DisplayName: name,
		}
		if metaJSON != "" {
			_ = json.Unmarshal([]byte(metaJSON), &r.Meta)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertResources writes the given resources in a single transaction. Existing
// rows with the same (type, key) are replaced; missing rows are left alone.
// Callers that need delete semantics use DeleteMissing in addition.
func (d *DB) UpsertResources(ctx context.Context, rs []core.Resource) error {
	if len(rs) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO resources (type, key, name, meta_json, indexed_at)
		VALUES (?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(type, key) DO UPDATE SET
			name = excluded.name,
			meta_json = excluded.meta_json,
			indexed_at = excluded.indexed_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range rs {
		metaJSON := "{}"
		if len(r.Meta) > 0 {
			b, _ := json.Marshal(r.Meta)
			metaJSON = string(b)
		}
		if _, err := stmt.ExecContext(ctx, r.Type.String(), r.Key, r.DisplayName, metaJSON); err != nil {
			return fmt.Errorf("upserting %s/%s: %w", r.Type, r.Key, err)
		}
	}
	return tx.Commit()
}

// DeleteMissing removes rows of the given type whose keys are NOT in keepKeys.
// This is how SWR prunes resources that no longer exist in AWS.
func (d *DB) DeleteMissing(ctx context.Context, t core.ResourceType, keepKeys map[string]struct{}) error {
	rows, err := d.sql.QueryContext(ctx, "SELECT key FROM resources WHERE type = ?", t.String())
	if err != nil {
		return err
	}
	var toDelete []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		if _, ok := keepKeys[k]; !ok {
			toDelete = append(toDelete, k)
		}
	}
	rows.Close()
	if len(toDelete) == 0 {
		return nil
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "DELETE FROM resources WHERE type = ? AND key = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, k := range toDelete {
		if _, err := stmt.ExecContext(ctx, t.String(), k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func parseType(s string) core.ResourceType {
	switch s {
	case "bucket":
		return core.RTypeBucket
	case "folder":
		return core.RTypeFolder
	case "object":
		return core.RTypeObject
	case "ecs_service":
		return core.RTypeEcsService
	case "ecs_taskdef":
		return core.RTypeEcsTaskDefFamily
	default:
		return core.RTypeBucket // defensive default; should never happen
	}
}

func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "better-aws"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "better-aws"), nil
}
