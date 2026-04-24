// Package core defines the shared vocabulary used by every scout
// package. Row is the unified cache-row shape; ResourceType is being
// phased out in favor of PackageID (a plain string).
package core

// Row is a single cached record belonging to one module. Keyed by
// (PackageID, Key). Fuzzy search matches on Name. Meta is an opaque
// per-module bag — core never reads it.
type Row struct {
	PackageID string            // module manifest ID, e.g. "s3"
	Key       string            // unique within the module
	Name      string            // fuzzy-match target
	Meta      map[string]string // module-owned; core stores as JSON
}
