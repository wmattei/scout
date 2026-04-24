// Package cache is the shared SQLite cache every module reads from
// and writes to. Keyed by (package_id, key). Orphan rows (package_id
// not in the live module set) are purged on startup.
package cache

import (
	"os"
	"path/filepath"
)

// DBPath returns the absolute path of the cache DB for the given
// (profile, region). One file per context under $XDG_CACHE_HOME/scout
// (fallback ~/.cache/scout).
func DBPath(profile, region string) (string, error) {
	dir, err := baseDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, profile+"__"+region+".db"), nil
}

func baseDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "scout"), nil
}
