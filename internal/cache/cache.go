package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/wmattei/scout/internal/core"
)

// DB wraps the SQLite handle with the Reader+Writer API modules use.
type DB struct {
	sql *sql.DB
}

// Open initializes (or creates) the cache DB for (profile, region).
// Applies schema migration if the on-disk version mismatches.
func Open(profile, region string) (*DB, error) {
	path, err := DBPath(profile, region)
	if err != nil {
		return nil, err
	}
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening cache %s: %w", path, err)
	}
	db := &DB{sql: handle}
	if err := db.migrate(); err != nil {
		handle.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

// PurgeOrphans deletes rows whose package_id is not in live. Run at
// startup once module.Registry is populated.
func (d *DB) PurgeOrphans(ctx context.Context, live []string) error {
	if len(live) == 0 {
		_, err := d.sql.ExecContext(ctx, "DELETE FROM rows")
		return err
	}
	placeholders := ""
	args := make([]interface{}, 0, len(live))
	for i, id := range live {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	_, err := d.sql.ExecContext(ctx,
		"DELETE FROM rows WHERE package_id NOT IN ("+placeholders+")", args...)
	return err
}

// AllRows returns every cached row. Used by the top-level unified
// fuzzy search.
func (d *DB) AllRows(ctx context.Context) ([]core.Row, error) {
	return d.query(ctx, "SELECT package_id, key, name, meta_json FROM rows", nil)
}

// RowsByPackage returns every cached row for one module.
func (d *DB) RowsByPackage(ctx context.Context, packageID string) ([]core.Row, error) {
	return d.query(ctx,
		"SELECT package_id, key, name, meta_json FROM rows WHERE package_id = ?",
		[]interface{}{packageID})
}

// Query returns rows in a module whose Key begins with prefix. S3
// drill-in uses this.
func (d *DB) Query(ctx context.Context, packageID, prefix string) ([]core.Row, error) {
	return d.query(ctx,
		"SELECT package_id, key, name, meta_json FROM rows WHERE package_id = ? AND key LIKE ? || '%'",
		[]interface{}{packageID, prefix})
}

// Upsert writes rows into the cache. All-or-nothing via a transaction.
func (d *DB) Upsert(ctx context.Context, rows []core.Row) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO rows (package_id, key, name, meta_json)
         VALUES (?, ?, ?, ?)
         ON CONFLICT(package_id, key) DO UPDATE SET
             name = excluded.name,
             meta_json = excluded.meta_json`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		metaJSON, mErr := json.Marshal(r.Meta)
		if mErr != nil {
			return fmt.Errorf("marshal meta for %s/%s: %w", r.PackageID, r.Key, mErr)
		}
		if _, err := stmt.ExecContext(ctx, r.PackageID, r.Key, r.Name, string(metaJSON)); err != nil {
			return fmt.Errorf("upsert %s/%s: %w", r.PackageID, r.Key, err)
		}
	}
	return tx.Commit()
}

// DeleteByPackage removes every row for one module. Used by the
// switcher commit flow and by `scout cache clear`.
func (d *DB) DeleteByPackage(ctx context.Context, packageID string) error {
	_, err := d.sql.ExecContext(ctx, "DELETE FROM rows WHERE package_id = ?", packageID)
	return err
}

func (d *DB) query(ctx context.Context, q string, args []interface{}) ([]core.Row, error) {
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]core.Row, 0)
	for rows.Next() {
		var r core.Row
		var metaJSON string
		if err := rows.Scan(&r.PackageID, &r.Key, &r.Name, &metaJSON); err != nil {
			return nil, err
		}
		if metaJSON == "" || metaJSON == "{}" {
			r.Meta = map[string]string{}
		} else if err := json.Unmarshal([]byte(metaJSON), &r.Meta); err != nil {
			return nil, fmt.Errorf("unmarshal meta for %s/%s: %w", r.PackageID, r.Key, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) migrate() error {
	var current int
	err := d.sql.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&current)
	if err != nil {
		if _, err := d.sql.Exec(schemaDDL); err != nil {
			return fmt.Errorf("applying schema: %w", err)
		}
		_, err := d.sql.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion)
		return err
	}
	if current != schemaVersion {
		if _, err := d.sql.Exec("DROP TABLE IF EXISTS rows; DROP TABLE IF EXISTS schema_version; DROP TABLE IF EXISTS bucket_contents; DROP TABLE IF EXISTS resources"); err != nil {
			return err
		}
		if _, err := d.sql.Exec(schemaDDL); err != nil {
			return err
		}
		_, err := d.sql.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion)
		return err
	}
	return nil
}
