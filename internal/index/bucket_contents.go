package index

import (
	"context"
	"fmt"

	"github.com/wmattei/scout/internal/core"
)

// UpsertBucketContents writes every resource in `rs` to the bucket_contents
// table in a single transaction. The caller is responsible for ensuring
// all resources belong to the same bucket — this function does not check.
//
// The schema stores full keys (no trimming). `is_folder` is derived from
// resource type. Size/mtime are pulled from Meta when present.
//
// This is the opportunistic caching sink for Phase 2 scoped search: every
// result that surfaces in the UI is upserted here so the next visit hits
// the cache.
func (d *DB) UpsertBucketContents(ctx context.Context, bucket string, rs []core.Resource) error {
	if len(rs) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO bucket_contents (bucket, key, is_folder, size, mtime)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(bucket, key) DO UPDATE SET
			is_folder = excluded.is_folder,
			size      = excluded.size,
			mtime     = excluded.mtime
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range rs {
		isFolder := 0
		if r.Type == core.RTypeFolder {
			isFolder = 1
		}
		var size, mtime interface{}
		if s, ok := r.Meta["size"]; ok {
			size = s
		}
		if m, ok := r.Meta["mtime"]; ok {
			mtime = m
		}
		if _, err := stmt.ExecContext(ctx, bucket, r.Key, isFolder, size, mtime); err != nil {
			return fmt.Errorf("upserting %s/%s: %w", bucket, r.Key, err)
		}
	}
	return tx.Commit()
}

// QueryBucketContents returns every bucket_contents row for the given
// bucket whose key begins with `prefix` AND whose relative path past
// `prefix` has no additional `/`. In other words: just the direct
// children at that prefix level, matching the behavior of a
// ListObjectsV2 call with Delimiter="/".
//
// DisplayName is reconstructed from the stored key so the caller does
// not need to do that work. Meta carries bucket and the stored size/mtime
// for objects.
func (d *DB) QueryBucketContents(ctx context.Context, bucket, prefix string) ([]core.Resource, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT key, is_folder, COALESCE(size, ''), COALESCE(mtime, '')
		 FROM bucket_contents
		 WHERE bucket = ? AND key LIKE ? || '%'`,
		bucket, prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("querying bucket_contents (bucket=%s prefix=%s): %w", bucket, prefix, err)
	}
	defer rows.Close()

	out := make([]core.Resource, 0)
	for rows.Next() {
		var key string
		var isFolder int
		var size, mtime string
		if err := rows.Scan(&key, &isFolder, &size, &mtime); err != nil {
			return nil, fmt.Errorf("scanning bucket_contents row: %w", err)
		}
		// Enforce single-level filtering here rather than in SQL — the
		// LIKE above does a whole-subtree match.
		rel := key[len(prefix):]
		if !isDirectChild(rel) {
			continue
		}
		r := core.Resource{
			Key:  key,
			Meta: map[string]string{"bucket": bucket},
		}
		if isFolder == 1 {
			r.Type = core.RTypeFolder
			r.DisplayName = lastSegmentWithSlash(key)
		} else {
			r.Type = core.RTypeObject
			r.DisplayName = lastSegment(key)
			if size != "" {
				r.Meta["size"] = size
			}
			if mtime != "" {
				r.Meta["mtime"] = mtime
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// isDirectChild reports whether `rel` (a key minus the prefix) is a direct
// child of the prefix level — i.e., it has no `/` except possibly a
// trailing one (for folders).
func isDirectChild(rel string) bool {
	if rel == "" {
		return false
	}
	// Trim a single trailing slash (folder entries) before checking.
	s := rel
	if len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return false
		}
	}
	return true
}

// lastSegmentWithSlash is a package-local mirror of the helper in the
// s3 adapter — the DB layer doesn't import that package so we re-declare
// it here.
func lastSegmentWithSlash(s string) string {
	trimmed := s
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '/' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i] == '/' {
			return trimmed[i+1:] + "/"
		}
	}
	return trimmed + "/"
}

// lastSegment returns the final `/`-separated segment of an object key.
func lastSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}
