# scout — Phase 1: Foundation & Top-Level Search

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Running `scout` opens a TUI showing a fuzzy-searchable list of the caller's S3 buckets, ECS services, and ECS task-definition families. Results come from a local SQLite cache for instant first paint, and are refreshed in the background via stale-while-revalidate. An activity spinner animates whenever AWS API calls are in flight. `↑`/`↓` moves selection, `Ctrl+C` quits.

**Architecture:** A single foreground `charmbracelet/bubbletea` program. Pure-Go SQLite (`modernc.org/sqlite`) for persistence, one DB file per `(profile, region)` pair under `~/.cache/scout/`. An in-memory index built on load; result lists are computed via `sahilm/fuzzy`. AWS calls use `aws-sdk-go-v2` with an activity-counting middleware that drives the spinner. Results flow from background goroutines to the UI via `tea.Msg`.

**Tech Stack:** Go 1.22+, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`, `aws-sdk-go-v2` (s3, ecs, sts, config), `modernc.org/sqlite`, `sahilm/fuzzy`.

**Scope boundary (what Phase 1 does NOT include):**
- S3 folder / object drill-in (Phase 2)
- Details mode + action execution (Phases 2 & 3)
- Profile / region switcher overlay (Phase 4)
- Preview / Download / Tail Logs (Phase 3)
- Panic recovery, debug log, cache-clear subcommand, error toast overlay (Phase 4)

Enter, Tab, `Ctrl+P`, `Ctrl+R`, and `Esc` are intentionally no-ops in this phase (or wired to minimal placeholder behavior called out per task).

**Reference spec:** `docs/superpowers/specs/2026-04-13-scout-v0-design.md`.

**Working directory for all tasks:** `/Users/wmattei/www/pied-piper/scout`. Every shell command below assumes this CWD.

**Testing policy for this phase:** Per project convention, no automated tests at v0. Every task ends with a manual verification step (usually `go build ./... && ./bin/scout` or a quick terminal run) followed by `git commit`.

---

## Task 1: Project scaffolding

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `cmd/scout/main.go`
- Create: `README.md`

- [ ] **Step 1: Initialize the Go module**

Run:
```bash
go mod init github.com/wmattei/scout
```

Expected: creates `go.mod` with the module path and a Go directive (`go 1.22` or later — the exact version string matches the installed toolchain).

- [ ] **Step 2: Create `.gitignore`**

Create `.gitignore` with:
```gitignore
# Build artifacts
/bin/
/dist/

# Go
*.test
*.out

# Editors
.vscode/
.idea/
*.swp
.DS_Store

# Local cache (for ad-hoc runs against a scratch HOME)
/tmp/
```

- [ ] **Step 3: Create a stub `cmd/scout/main.go`**

Create `cmd/scout/main.go`:
```go
package main

import "fmt"

// Version is set at build time via -ldflags (Phase 4). For Phase 1 it is a constant.
const Version = "0.0.0-phase1"

func main() {
	fmt.Printf("scout %s (scaffold — Phase 1 in progress)\n", Version)
}
```

- [ ] **Step 4: Create a minimal `README.md`**

Create `README.md`:
```markdown
# scout

Interactive terminal CLI for navigating AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services, and task definitions; live prefix search into S3 bucket contents.

**Status:** v0 in development — see `docs/superpowers/specs/2026-04-13-scout-v0-design.md` for the spec and `docs/superpowers/plans/` for the phase plans.

## Build

```bash
go build -o bin/scout ./cmd/scout
./bin/scout
```

## Install

```bash
go install ./cmd/scout
```
```

- [ ] **Step 5: Verify the binary compiles and runs**

Run:
```bash
go build -o bin/scout ./cmd/scout
./bin/scout
```

Expected: prints `scout 0.0.0-phase1 (scaffold — Phase 1 in progress)` and exits 0.

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore cmd/scout/main.go README.md
git commit -m "chore: scaffold Go module, binary entry point, gitignore"
```

---

## Task 2: Core resource types

**Files:**
- Create: `internal/core/resource.go`

- [ ] **Step 1: Create the `core` package**

Create `internal/core/resource.go`:
```go
// Package core defines the data types shared across the indexer, search,
// AWS adapters, and TUI layers. Nothing in this package imports from other
// internal packages — it is the root of the internal dependency graph.
package core

import "fmt"

// ResourceType enumerates the kinds of AWS resources scout knows about.
// Phase 1 only uses RTypeBucket, RTypeEcsService, and RTypeEcsTaskDefFamily.
// RTypeFolder and RTypeObject exist for later phases and are declared here so
// the TUI and index layers can pattern-match on the complete set.
type ResourceType int

const (
	RTypeBucket ResourceType = iota
	RTypeFolder
	RTypeObject
	RTypeEcsService
	RTypeEcsTaskDefFamily
)

// String returns a short machine name used in the SQLite schema's `type`
// column and in debug output. Stable — do not change without a migration.
func (r ResourceType) String() string {
	switch r {
	case RTypeBucket:
		return "bucket"
	case RTypeFolder:
		return "folder"
	case RTypeObject:
		return "object"
	case RTypeEcsService:
		return "ecs_service"
	case RTypeEcsTaskDefFamily:
		return "ecs_taskdef"
	default:
		return "unknown"
	}
}

// Tag is the short label shown as a colored chip in the TUI.
func (r ResourceType) Tag() string {
	switch r {
	case RTypeBucket:
		return "S3"
	case RTypeFolder:
		return "DIR"
	case RTypeObject:
		return "OBJ"
	case RTypeEcsService:
		return "ECS"
	case RTypeEcsTaskDefFamily:
		return "TASK"
	default:
		return "???"
	}
}

// Resource is the unified record for anything browsable in the TUI.
//
// Key uniquely identifies the resource within (profile, region, type). For
// buckets it is the bucket name; for ecs services it is the service ARN; for
// task def families it is the family name; for folders/objects it is the
// key path (with trailing '/' for folders).
//
// DisplayName is what the TUI renders and what the fuzzy matcher searches
// against. For most resources it equals Name; for ECS services we strip the
// ARN and keep the bare service name.
//
// Meta is a free-form bag carrying render hints (region, cluster, size,
// mtime). Values are strings to keep serialization trivial. Callers that
// need typed access parse on read.
type Resource struct {
	Type        ResourceType
	Key         string
	DisplayName string
	Meta        map[string]string
}

// ARN returns a canonical AWS ARN for the resource. For folders and objects
// a pseudo-ARN of the form arn:aws:s3:::<bucket>/<key> is used so the
// details panel can always show an "ARN" row. Phase 1 only calls this for
// buckets, services, and task def families — the folder/object branches are
// pre-wired for Phase 2.
func (r Resource) ARN() string {
	switch r.Type {
	case RTypeBucket:
		return fmt.Sprintf("arn:aws:s3:::%s", r.Key)
	case RTypeFolder, RTypeObject:
		bucket := r.Meta["bucket"]
		return fmt.Sprintf("arn:aws:s3:::%s/%s", bucket, r.Key)
	case RTypeEcsService:
		// Key is the full service ARN for ecs services.
		return r.Key
	case RTypeEcsTaskDefFamily:
		// Latest revision is resolved lazily in later phases; for Phase 1
		// we surface the family name so the details panel (when added) can
		// show "…resolving" until DescribeTaskDefinition returns.
		return fmt.Sprintf("arn:aws:ecs:*:*:task-definition/%s", r.Key)
	default:
		return ""
	}
}
```

- [ ] **Step 2: Verify the package compiles**

Run:
```bash
go build ./...
```

Expected: no output, exit 0. An unused-import error here means a typo — fix and re-run.

- [ ] **Step 3: Commit**

```bash
git add internal/core/resource.go
git commit -m "feat(core): add Resource and ResourceType"
```

---

## Task 3: SQLite persistence layer

**Files:**
- Create: `internal/index/db.go`

- [ ] **Step 1: Add the SQLite driver dependency**

Run:
```bash
go get modernc.org/sqlite
```

Expected: `go.sum` updated with `modernc.org/sqlite` and its transitive deps.

- [ ] **Step 2: Create `internal/index/db.go`**

Create `internal/index/db.go`:
```go
// Package index owns the on-disk SQLite cache and the in-memory index that
// serves the TUI. DBs are one file per (profile, region) pair, living under
// $XDG_CACHE_HOME/scout (fallback: $HOME/.cache/scout).
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/wmattei/scout/internal/core"
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
		return filepath.Join(xdg, "scout"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "scout"), nil
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output. If `sqlite` driver registration fails at compile time, re-check the blank import line `_ "modernc.org/sqlite"`.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/index/db.go
git commit -m "feat(index): add sqlite-backed resource cache"
```

---

## Task 4: In-memory index

**Files:**
- Create: `internal/index/memory.go`

- [ ] **Step 1: Create the in-memory index**

Create `internal/index/memory.go`:
```go
package index

import (
	"sort"
	"sync"

	"github.com/wmattei/scout/internal/core"
)

// Memory is the in-RAM, read-mostly view of the cache that the TUI searches
// against. It is rebuilt on startup from a DB.LoadAll() call and mutated by
// SWR refresh commands via Upsert / DeleteMissing.
//
// The search layer iterates All() for fuzzy matching; this is intentionally
// simple (a linear scan over a slice) and fast enough for tens of thousands
// of resources. If that stops being true we'll swap in a smarter index —
// but YAGNI for Phase 1.
type Memory struct {
	mu sync.RWMutex
	// byTypeKey is the canonical map; All() derives a snapshot slice.
	byTypeKey map[typeKey]core.Resource
}

type typeKey struct {
	Type core.ResourceType
	Key  string
}

// NewMemory returns an empty in-memory index.
func NewMemory() *Memory {
	return &Memory{byTypeKey: make(map[typeKey]core.Resource)}
}

// Load replaces the entire index contents with rs. Used on startup.
func (m *Memory) Load(rs []core.Resource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byTypeKey = make(map[typeKey]core.Resource, len(rs))
	for _, r := range rs {
		m.byTypeKey[typeKey{r.Type, r.Key}] = r
	}
}

// Upsert inserts or updates resources. Safe for concurrent use.
func (m *Memory) Upsert(rs []core.Resource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rs {
		m.byTypeKey[typeKey{r.Type, r.Key}] = r
	}
}

// DeleteMissing removes all resources of type t whose keys are not in keep.
// Mirrors DB.DeleteMissing so SWR can keep both stores in sync.
func (m *Memory) DeleteMissing(t core.ResourceType, keep map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.byTypeKey {
		if k.Type != t {
			continue
		}
		if _, ok := keep[k.Key]; !ok {
			delete(m.byTypeKey, k)
		}
	}
}

// All returns a snapshot slice of all top-level resources the TUI should
// search against. Top-level in Phase 1 means: buckets, ecs services, and
// ecs task def families. Folders and objects are excluded here — they are
// searched in the scoped mode, which is Phase 2 territory.
//
// The slice is freshly allocated on every call so callers may sort, filter,
// or otherwise mutate it without locking.
func (m *Memory) All() []core.Resource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]core.Resource, 0, len(m.byTypeKey))
	for _, r := range m.byTypeKey {
		switch r.Type {
		case core.RTypeBucket, core.RTypeEcsService, core.RTypeEcsTaskDefFamily:
			out = append(out, r)
		}
	}
	// Stable order helps the TUI render deterministically when the query is
	// empty. Sort primarily by type priority, then lexicographically by name.
	sort.Slice(out, func(i, j int) bool {
		if pri(out[i].Type) != pri(out[j].Type) {
			return pri(out[i].Type) < pri(out[j].Type)
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// pri returns a ranking priority for stable sort. Lower = earlier in the list.
func pri(t core.ResourceType) int {
	switch t {
	case core.RTypeBucket:
		return 0
	case core.RTypeEcsService:
		return 1
	case core.RTypeEcsTaskDefFamily:
		return 2
	default:
		return 99
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/index/memory.go
git commit -m "feat(index): add in-memory resource index"
```

---

## Task 5: AWS SDK config resolver

**Files:**
- Create: `internal/awsctx/config.go`

(The package is named `awsctx` rather than `aws` to avoid shadowing the `aws-sdk-go-v2` `aws` package, which we import from this file.)

- [ ] **Step 1: Add SDK dependencies**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/aws
go get github.com/aws/aws-sdk-go-v2/service/sts
```

Expected: `go.sum` grows with `aws-sdk-go-v2` packages.

- [ ] **Step 2: Create `internal/awsctx/config.go`**

Create `internal/awsctx/config.go`:
```go
// Package awsctx wraps aws-sdk-go-v2 configuration loading and exposes a
// single "Context" value that carries everything downstream AWS adapters
// need: the loaded aws.Config, the resolved profile name, the resolved
// region, and (lazily) the caller-identity account ID.
package awsctx

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Context is the resolved AWS environment for the current session. Phase 1
// creates exactly one on startup; Phase 4's profile switcher will rebuild it.
type Context struct {
	Profile string
	Region  string
	Cfg     aws.Config
}

// Resolve loads an aws.Config using the default SDK chain with the following
// precedence for profile and region:
//
//	profile: AWS_PROFILE > AWS_DEFAULT_PROFILE > "default"
//	region:  AWS_REGION  > AWS_DEFAULT_REGION  > profile's configured region
//
// If none of the above yield a region, Resolve returns an error — Phase 1
// surfaces this to stderr and exits; Phase 4 adds a modal fallback picker.
func Resolve(ctx context.Context) (*Context, error) {
	profile := firstNonEmpty(os.Getenv("AWS_PROFILE"), os.Getenv("AWS_DEFAULT_PROFILE"), "default")

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithSharedConfigProfile(profile),
	}
	if region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")); region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config (profile=%s): %w", profile, err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("no region resolved for profile %q — set AWS_REGION or configure 'region' in ~/.aws/config", profile)
	}

	return &Context{
		Profile: profile,
		Region:  cfg.Region,
		Cfg:     cfg,
	}, nil
}

// CallerIdentity fetches the account ID via sts:GetCallerIdentity. Called at
// most once per session in Phase 1 to render the status bar; cached by the
// caller.
func (c *Context) CallerIdentity(ctx context.Context) (string, error) {
	out, err := sts.NewFromConfig(c.Cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("sts:GetCallerIdentity: %w", err)
	}
	if out.Account == nil {
		return "", fmt.Errorf("sts:GetCallerIdentity returned no account")
	}
	return *out.Account, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/awsctx/config.go
git commit -m "feat(awsctx): resolve profile, region, and aws.Config from SDK chain"
```

---

## Task 6: Activity middleware

**Files:**
- Create: `internal/awsctx/activity.go`

- [ ] **Step 1: Add middleware import**

Run:
```bash
go get github.com/aws/smithy-go/middleware
```

Expected: `go.sum` updated (smithy-go is already a transitive dep, this makes it direct).

- [ ] **Step 2: Create `internal/awsctx/activity.go`**

Create `internal/awsctx/activity.go`:
```go
package awsctx

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/middleware"
)

// Activity is a process-global counter of in-flight AWS API calls. The TUI
// reads Snapshot() on every spinner tick to decide whether to animate and
// which op name to show. Callers attach it to their aws.Config via Attach().
//
// A single instance is sufficient because the binary runs one bubbletea
// program at a time. Phase 4's profile switcher rebuilds the Context but
// shares the same Activity.
type Activity struct {
	inflight int64 // atomic

	mu    sync.Mutex
	lastOp string
}

// ActivitySnapshot captures the counter state at a single instant.
type ActivitySnapshot struct {
	InFlight int64
	LastOp   string
}

// NewActivity returns a fresh counter.
func NewActivity() *Activity { return &Activity{} }

// Snapshot returns a consistent view of inflight + last op.
func (a *Activity) Snapshot() ActivitySnapshot {
	a.mu.Lock()
	op := a.lastOp
	a.mu.Unlock()
	return ActivitySnapshot{
		InFlight: atomic.LoadInt64(&a.inflight),
		LastOp:   op,
	}
}

func (a *Activity) start(op string) {
	atomic.AddInt64(&a.inflight, 1)
	a.mu.Lock()
	a.lastOp = op
	a.mu.Unlock()
}

func (a *Activity) finish() {
	atomic.AddInt64(&a.inflight, -1)
}

// Attach installs a Smithy middleware on cfg so every AWS API call
// increments Activity.inflight on request-start and decrements it on
// response (success or failure). The operation name shown in the TUI is the
// most recent one to start.
func (a *Activity) Attach(cfg *aws.Config) {
	cfg.APIOptions = append(cfg.APIOptions, func(stack *middleware.Stack) error {
		return stack.Initialize.Add(
			middleware.InitializeMiddlewareFunc("scout/activity",
				func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
					op := middleware.GetOperationName(ctx)
					a.start(op)
					defer a.finish()
					return next.HandleInitialize(ctx, in)
				},
			),
			middleware.Before,
		)
	})
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/awsctx/activity.go
git commit -m "feat(awsctx): add in-flight activity counter middleware"
```

---

## Task 7: S3 buckets adapter

**Files:**
- Create: `internal/awsctx/s3/buckets.go`

- [ ] **Step 1: Add the S3 service dependency**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/service/s3
```

- [ ] **Step 2: Create `internal/awsctx/s3/buckets.go`**

Create `internal/awsctx/s3/buckets.go`:
```go
// Package s3 contains scout's thin wrappers around the AWS S3 SDK.
// Each function returns []core.Resource ready to hand to the index layer.
package s3

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListBuckets returns every bucket visible to the current caller. One call,
// no pagination. The Region meta field is populated from the session's
// region rather than from GetBucketLocation — GetBucketLocation costs one
// extra call per bucket and Phase 1 doesn't render per-bucket region yet.
func ListBuckets(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("s3:ListBuckets: %w", err)
	}
	resources := make([]core.Resource, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		if b.Name == nil {
			continue
		}
		resources = append(resources, core.Resource{
			Type:        core.RTypeBucket,
			Key:         *b.Name,
			DisplayName: *b.Name,
			Meta: map[string]string{
				"region": ac.Region,
			},
		})
	}
	return resources, nil
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/awsctx/s3/buckets.go
git commit -m "feat(aws/s3): add ListBuckets adapter"
```

---

## Task 8: ECS services adapter

**Files:**
- Create: `internal/awsctx/ecs/services.go`

- [ ] **Step 1: Add the ECS service dependency**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/service/ecs
```

- [ ] **Step 2: Create `internal/awsctx/ecs/services.go`**

Create `internal/awsctx/ecs/services.go`:
```go
// Package ecs contains scout's thin wrappers around the AWS ECS SDK.
package ecs

import (
	"context"
	"fmt"
	"strings"

	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListServices walks every cluster in the region and returns one Resource
// per service. Implementation steps:
//
//  1. ListClusters (paginated) — gives cluster ARNs.
//  2. For each cluster, ListServices (paginated) — gives service ARNs.
//  3. DescribeServices in batches of 10 (the hard limit for that API) —
//     gives launch type, desired count, and the user-facing service name.
//
// The Key is the full service ARN so actions (Phase 3) can use it directly.
// DisplayName is the bare service name (last segment of the ARN path).
// Meta includes the cluster ARN so the Tail Logs action can resolve tasks.
func ListServices(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	// Step 1: clusters.
	var clusterArns []string
	var clusterNext *string
	for {
		out, err := client.ListClusters(ctx, &awsecs.ListClustersInput{NextToken: clusterNext})
		if err != nil {
			return nil, fmt.Errorf("ecs:ListClusters: %w", err)
		}
		clusterArns = append(clusterArns, out.ClusterArns...)
		if out.NextToken == nil {
			break
		}
		clusterNext = out.NextToken
	}

	var resources []core.Resource
	for _, cluster := range clusterArns {
		// Step 2: services within this cluster.
		var serviceArns []string
		var svcNext *string
		for {
			out, err := client.ListServices(ctx, &awsecs.ListServicesInput{
				Cluster:   stringPtr(cluster),
				NextToken: svcNext,
			})
			if err != nil {
				return nil, fmt.Errorf("ecs:ListServices (cluster=%s): %w", cluster, err)
			}
			serviceArns = append(serviceArns, out.ServiceArns...)
			if out.NextToken == nil {
				break
			}
			svcNext = out.NextToken
		}

		// Step 3: describe in batches of 10.
		for i := 0; i < len(serviceArns); i += 10 {
			end := i + 10
			if end > len(serviceArns) {
				end = len(serviceArns)
			}
			batch := serviceArns[i:end]
			out, err := client.DescribeServices(ctx, &awsecs.DescribeServicesInput{
				Cluster:  stringPtr(cluster),
				Services: batch,
			})
			if err != nil {
				return nil, fmt.Errorf("ecs:DescribeServices (cluster=%s): %w", cluster, err)
			}
			for _, svc := range out.Services {
				if svc.ServiceArn == nil || svc.ServiceName == nil {
					continue
				}
				resources = append(resources, core.Resource{
					Type:        core.RTypeEcsService,
					Key:         *svc.ServiceArn,
					DisplayName: *svc.ServiceName,
					Meta: map[string]string{
						"cluster":     clusterShortName(cluster),
						"clusterArn":  cluster,
						"launchType":  string(svc.LaunchType),
						"desired":     fmt.Sprintf("%d", svc.DesiredCount),
					},
				})
			}
		}
	}
	return resources, nil
}

// clusterShortName extracts the cluster name from its ARN.
// arn:aws:ecs:us-east-1:123:cluster/prod-cluster -> prod-cluster
func clusterShortName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func stringPtr(s string) *string { return &s }
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/awsctx/ecs/services.go
git commit -m "feat(aws/ecs): add ListServices adapter"
```

---

## Task 9: ECS task definition families adapter

**Files:**
- Create: `internal/awsctx/ecs/taskdefs.go`

- [ ] **Step 1: Create `internal/awsctx/ecs/taskdefs.go`**

Create `internal/awsctx/ecs/taskdefs.go`:
```go
package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListTaskDefFamilies returns one Resource per active task-definition family.
// The family name is the Key and DisplayName; revision is resolved lazily
// via DescribeTaskDefinition when actions need it (Phase 3).
//
// Families are listed with status=ACTIVE so retired families don't clutter
// the results.
func ListTaskDefFamilies(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	var families []string
	var next *string
	for {
		out, err := client.ListTaskDefinitionFamilies(ctx, &awsecs.ListTaskDefinitionFamiliesInput{
			Status:    ecstypes.TaskDefinitionFamilyStatusActive,
			NextToken: next,
			MaxResults: aws.Int32(100),
		})
		if err != nil {
			return nil, fmt.Errorf("ecs:ListTaskDefinitionFamilies: %w", err)
		}
		families = append(families, out.Families...)
		if out.NextToken == nil {
			break
		}
		next = out.NextToken
	}

	resources := make([]core.Resource, 0, len(families))
	for _, fam := range families {
		resources = append(resources, core.Resource{
			Type:        core.RTypeEcsTaskDefFamily,
			Key:         fam,
			DisplayName: fam,
			Meta:        map[string]string{}, // revision + containers resolved lazily
		})
	}
	return resources, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/ecs/taskdefs.go
git commit -m "feat(aws/ecs): add ListTaskDefFamilies adapter"
```

---

## Task 10: Fuzzy matcher wrapper

**Files:**
- Create: `internal/search/fuzzy.go`

- [ ] **Step 1: Add fuzzy match dependency**

Run:
```bash
go get github.com/sahilm/fuzzy
```

- [ ] **Step 2: Create `internal/search/fuzzy.go`**

Create `internal/search/fuzzy.go`:
```go
// Package search houses the fuzzy and prefix match engines used by the TUI
// to turn a query string into a ranked, highlight-annotated result list.
//
// Phase 1 only uses the fuzzy engine (top-level mode). Phase 2 adds the
// prefix engine for scoped mode. They share the Result type so the result
// list renderer doesn't need to know which engine produced a row.
package search

import (
	"github.com/sahilm/fuzzy"

	"github.com/wmattei/scout/internal/core"
)

// Result is one row in a search result list, with enough metadata for the
// TUI to render per-character highlight spans.
type Result struct {
	Resource   core.Resource
	MatchedRunes []int // byte positions in DisplayName; empty for "no query"
	Score      int   // higher is better; 0 for "no query" baseline
}

// Fuzzy runs a fuzzy match against every resource in `all` and returns the
// top `limit` results ordered by score (descending). An empty query returns
// the input unchanged (already sorted by the caller) and no highlight spans.
func Fuzzy(query string, all []core.Resource, limit int) []Result {
	if query == "" {
		out := make([]Result, 0, minInt(limit, len(all)))
		upto := minInt(limit, len(all))
		for i := 0; i < upto; i++ {
			out = append(out, Result{Resource: all[i]})
		}
		return out
	}

	// sahilm/fuzzy wants a Source interface. Adapt the resource slice.
	src := resSource(all)
	matches := fuzzy.FindFrom(query, src)

	upto := minInt(limit, len(matches))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		m := matches[i]
		out = append(out, Result{
			Resource:     all[m.Index],
			MatchedRunes: m.MatchedIndexes,
			Score:        m.Score,
		})
	}
	return out
}

// resSource adapts []core.Resource to fuzzy.Source so the library can read
// DisplayName for each entry.
type resSource []core.Resource

func (r resSource) String(i int) string { return r[i].DisplayName }
func (r resSource) Len() int             { return len(r) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/search/fuzzy.go
git commit -m "feat(search): add fuzzy matcher with match-position output"
```

---

## Task 11: TUI styles

**Files:**
- Create: `internal/tui/styles.go`

- [ ] **Step 1: Add charm dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles
go get github.com/charmbracelet/lipgloss
```

- [ ] **Step 2: Create `internal/tui/styles.go`**

Create `internal/tui/styles.go`:
```go
// Package tui renders and drives the bubbletea program. Styles for the
// whole TUI live in this file so colors and borders can be tweaked in one
// place.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
)

// adaptive pair helper.
func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	// Input bar.
	styleInputPrompt = lipgloss.NewStyle().Bold(true).Foreground(ac("#005FAF", "#5FD7FF"))
	styleInputText   = lipgloss.NewStyle().Foreground(ac("#000000", "#FFFFFF"))

	// Result rows.
	styleRowBase    = lipgloss.NewStyle()
	styleRowSel     = lipgloss.NewStyle().Background(ac("#D0D0FF", "#2A2A5A"))
	styleRowDim     = lipgloss.NewStyle().Foreground(ac("#767676", "#8A8A8A"))
	styleHighlight  = lipgloss.NewStyle().Bold(true).Foreground(ac("#000000", "#FFFFFF"))
	styleSelIndi    = lipgloss.NewStyle().Bold(true).Foreground(ac("#005FAF", "#5FD7FF"))

	// Tag styles per resource type. Keys are ResourceType.Tag() strings.
	styleTagS3   = tagStyle("#005FAF", "#5FD7FF")
	styleTagDir  = tagStyle("#008787", "#5FFFFF")
	styleTagObj  = tagStyle("#585858", "#A8A8A8")
	styleTagEcs  = tagStyle("#AF5F00", "#FFAF5F")
	styleTagTask = tagStyle("#AF8700", "#FFD75F")

	// Status bar.
	styleStatusBar = lipgloss.NewStyle().Padding(0, 1).Background(ac("#D0D0E0", "#1A1A2E")).Foreground(ac("#000000", "#D0D0D0"))
	styleSpinner   = lipgloss.NewStyle().Foreground(ac("#005F87", "#5FAFD7"))
	styleError     = lipgloss.NewStyle().Bold(true).Foreground(ac("#870000", "#FF5F5F"))
	styleDivider   = lipgloss.NewStyle().Foreground(ac("#A8A8A8", "#303030"))
)

func tagStyle(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(ac(light, dark))
}

// tagStyleFor returns the colored tag style for a resource type.
func tagStyleFor(t core.ResourceType) lipgloss.Style {
	switch t {
	case core.RTypeBucket:
		return styleTagS3
	case core.RTypeFolder:
		return styleTagDir
	case core.RTypeObject:
		return styleTagObj
	case core.RTypeEcsService:
		return styleTagEcs
	case core.RTypeEcsTaskDefFamily:
		return styleTagTask
	default:
		return styleRowDim
	}
}

// padTag right-pads a tag label to a fixed width so names align.
// Example: "S3" -> "[S3  ]", "TASK" -> "[TASK]".
func padTag(label string) string {
	const width = 4
	out := label
	for len(out) < width {
		out += " "
	}
	return "[" + out + "]"
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/tui/styles.go
git commit -m "feat(tui): add lipgloss styles and per-type tag styles"
```

---

## Task 12: Result list rendering

**Files:**
- Create: `internal/tui/results.go`

- [ ] **Step 1: Create `internal/tui/results.go`**

Create `internal/tui/results.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/search"
)

// renderResults returns a string containing every visible row, one per line.
// selected is the index into results that is currently highlighted; if
// selected is out of range (e.g. empty list), the function returns an
// "empty state" message instead.
//
// width is the total rendered width (the frame width). The row layout is:
//
//   ▸ [TAG ] <highlighted name>     <right-aligned meta>
//
// The name segment takes whatever horizontal space is left after the
// indicator, tag, spacing, and meta columns.
func renderResults(results []search.Result, selected, width, height int, emptyMsg string) string {
	if len(results) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			styleRowDim.Render(emptyMsg))
	}

	const (
		indiWidth = 2  // "▸ " or "  "
		tagWidth  = 6  // "[S3  ]"
		gap       = 1  // space between columns
		metaMin   = 0  // meta shrinks before name does
	)

	var b strings.Builder
	rows := height
	if rows > len(results) {
		rows = len(results)
	}
	// Scroll window: keep selection visible.
	start := 0
	if selected >= rows {
		start = selected - rows + 1
	}
	end := start + rows
	if end > len(results) {
		end = len(results)
	}

	for i := start; i < end; i++ {
		r := results[i]
		isSelected := i == selected

		// 1. Indicator.
		indi := "  "
		if isSelected {
			indi = styleSelIndi.Render("▸ ")
		}

		// 2. Tag.
		tag := tagStyleFor(r.Resource.Type).Render(padTag(r.Resource.Type.Tag()))

		// 3. Meta (right-aligned).
		meta := renderMeta(r.Resource)

		// 4. Name (flex, with highlight spans).
		nameBudget := width - indiWidth - tagWidth - gap*2 - lipgloss.Width(meta)
		if nameBudget < 4 {
			nameBudget = 4
		}
		name := renderNameWithHighlights(r.Resource.DisplayName, r.MatchedRunes, nameBudget)

		line := fmt.Sprintf("%s%s %s %s", indi, tag, padRight(name, nameBudget), meta)
		if isSelected {
			line = styleRowSel.Width(width).Render(line)
		} else {
			line = styleRowBase.Width(width).Render(line)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	// Pad the remaining lines so the result area has a consistent height.
	for i := end - start; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderNameWithHighlights breaks name into matched / unmatched runs and
// applies styleHighlight to the matched positions. matchIdx is a sorted list
// of byte positions into DisplayName (from the fuzzy matcher).
func renderNameWithHighlights(name string, matchIdx []int, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	// Truncate to maxWidth runes with an ellipsis if needed. Tracking runes
	// not bytes so multi-byte characters render correctly in the terminal.
	runes := []rune(name)
	if len(runes) > maxWidth {
		runes = append(runes[:maxWidth-1], '…')
	}

	if len(matchIdx) == 0 {
		return string(runes)
	}

	// Build a set for O(1) lookup. Positions come from the fuzzy lib as
	// byte indexes, but sahilm/fuzzy is ASCII-friendly in practice; convert
	// byte positions to rune positions by walking the original string.
	byteToRune := make(map[int]int, len(name))
	runeIdx := 0
	for i := range name {
		byteToRune[i] = runeIdx
		runeIdx++
	}
	matched := make(map[int]bool, len(matchIdx))
	for _, bi := range matchIdx {
		if ri, ok := byteToRune[bi]; ok {
			matched[ri] = true
		}
	}

	var b strings.Builder
	for i, r := range runes {
		ch := string(r)
		if matched[i] {
			b.WriteString(styleHighlight.Render(ch))
		} else {
			b.WriteString(ch)
		}
	}
	return b.String()
}

// renderMeta produces the right-aligned meta column for a resource. Phase 1
// shows region for buckets and cluster name for ecs services. Task def
// families have no meta yet.
func renderMeta(r core.Resource) string {
	switch r.Type {
	case core.RTypeBucket:
		return styleRowDim.Render(r.Meta["region"])
	case core.RTypeEcsService:
		return styleRowDim.Render(r.Meta["cluster"])
	default:
		return ""
	}
}

// padRight pads s with spaces on the right so its visual width equals n.
// Uses lipgloss.Width so ANSI sequences don't break the count.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/results.go
git commit -m "feat(tui): render result list with tag + highlight + meta columns"
```

---

## Task 13: Status bar rendering

**Files:**
- Create: `internal/tui/status.go`

- [ ] **Step 1: Create `internal/tui/status.go`**

Create `internal/tui/status.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/wmattei/scout/internal/awsctx"
)

// spinnerFrames is a simple braille-dot spinner. Index % len picks a frame.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame(tick int) string { return spinnerFrames[tick%len(spinnerFrames)] }

// renderStatus composes the bottom status bar: profile, region, account,
// and activity indicator. width is the full frame width; the returned
// string is exactly one line tall and exactly `width` columns wide.
func renderStatus(width int, profile, region, account string, activity awsctx.ActivitySnapshot, tick int) string {
	left := fmt.Sprintf("profile=%s  region=%s", profile, region)
	if account != "" {
		left += fmt.Sprintf("  acct=%s", account)
	}

	right := ""
	switch {
	case activity.InFlight > 1:
		right = fmt.Sprintf("%s %d calls…", styleSpinner.Render(spinnerFrame(tick)), activity.InFlight)
	case activity.InFlight == 1:
		op := activity.LastOp
		if op == "" {
			op = "…"
		}
		right = fmt.Sprintf("%s %s", styleSpinner.Render(spinnerFrame(tick)), op)
	}

	gap := width - visibleWidth(left) - visibleWidth(right) - 2 // -2 for padding
	if gap < 1 {
		gap = 1
	}
	line := " " + left + strings.Repeat(" ", gap) + right + " "
	return styleStatusBar.Width(width).Render(line)
}

// visibleWidth is a tiny shim so tests can swap in fake width logic if needed.
func visibleWidth(s string) int {
	// lipgloss's Width handles ANSI escapes.
	return lipglossWidth(s)
}
```

- [ ] **Step 2: Add the lipgloss width helper**

Create `internal/tui/width.go`:
```go
package tui

import "github.com/charmbracelet/lipgloss"

// lipglossWidth wraps lipgloss.Width so other files can import the function
// without pulling in the full package. Keeping it in its own file makes the
// import surface for status.go / results.go uniform.
func lipglossWidth(s string) int { return lipgloss.Width(s) }
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/status.go internal/tui/width.go
git commit -m "feat(tui): render status bar with profile, region, and activity spinner"
```

---

## Task 14: Model, update, view skeleton

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/update.go`
- Create: `internal/tui/view.go`

- [ ] **Step 1: Create `internal/tui/model.go`**

Create `internal/tui/model.go`:
```go
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/search"
)

// Model is the bubbletea model for the search view. Phase 1 only has the
// search mode — Phase 2 will introduce a Mode enum and additional sub-models.
type Model struct {
	// Injected dependencies.
	memory   *index.Memory
	db       *index.DB
	awsCtx   *awsctx.Context
	activity *awsctx.Activity

	// UI state.
	input    textinput.Model
	width    int
	height   int
	selected int
	results  []search.Result
	account  string
	spinTick int

	// Derived: the last snapshot we cached resources into memory from.
	lastTopLevel []core.Resource
}

// NewModel constructs the initial model for the bubbletea program.
func NewModel(memory *index.Memory, db *index.DB, awsCtx *awsctx.Context, activity *awsctx.Activity) Model {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 512

	return Model{
		memory:   memory,
		db:       db,
		awsCtx:   awsCtx,
		activity: activity,
		input:    ti,
		width:    80,
		height:   24,
	}
}

// Init is called once when the program starts.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		initialResultsCmd(m.memory, ""),
		refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}
```

- [ ] **Step 2: Create `internal/tui/update.go`**

Create `internal/tui/update.go`:
```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/search"
)

// Custom messages emitted by commands.
type (
	msgResults    struct{ results []search.Result }
	msgResourcesUpdated struct{}
	msgAccount    struct{ account string }
	msgSpinTick   struct{}
)

// Update routes messages to state mutations and side-effect commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil
		case "enter", "tab", "ctrl+p", "ctrl+r", "esc":
			// Reserved for later phases. No-op in Phase 1.
			return m, nil
		}

		// Let the textinput consume the keystroke.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Recompute results from the current cache + new query.
		results := search.Fuzzy(m.input.Value(), m.memory.All(), max(1, m.height-3))
		m.results = results
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, cmd

	case msgResults:
		m.results = msg.results
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, nil

	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current result list against the updated snapshot.
		results := search.Fuzzy(m.input.Value(), m.memory.All(), max(1, m.height-3))
		m.results = results
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, nil

	case msgAccount:
		m.account = msg.account
		return m, nil

	case msgSpinTick:
		m.spinTick++
		return m, spinTickCmd()
	}

	return m, nil
}

// spinTickCmd schedules the next spinner frame. 100ms gives ~10fps which is
// plenty for a braille spinner and costs almost nothing.
func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Create `internal/tui/view.go`**

Create `internal/tui/view.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full frame: input row, divider, result list, divider,
// status row.
func (m Model) View() string {
	// Minimum usable width check (per spec §7).
	if m.width < 60 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleError.Render(fmt.Sprintf("terminal too narrow — resize ≥60 columns (current: %d)", m.width)))
	}

	input := m.input.View()
	// Right-aligned glyph so the bar looks intentional.
	inputLine := fmt.Sprintf("%s%s", padRight(input, m.width-3), " 🔍")

	// Divider.
	divider := styleDivider.Render(strings.Repeat("─", m.width))

	// Status.
	status := renderStatus(m.width, m.awsCtx.Profile, m.awsCtx.Region, m.account, m.activity.Snapshot(), m.spinTick)

	// Result list height = terminal height - input(1) - divider(1) - divider(1) - status(1).
	resultsHeight := m.height - 4
	if resultsHeight < 1 {
		resultsHeight = 1
	}

	emptyMsg := "no results"
	switch {
	case m.input.Value() == "" && len(m.results) == 0:
		emptyMsg = "cache is empty — fetching…"
	case m.input.Value() != "" && len(m.results) == 0:
		emptyMsg = fmt.Sprintf("no matches for %q", m.input.Value())
	}
	results := renderResults(m.results, m.selected, m.width, resultsHeight, emptyMsg)

	return strings.Join([]string{
		inputLine,
		divider,
		results,
		divider,
		status,
	}, "\n")
}
```

- [ ] **Step 4: Verify it compiles (expected: errors about missing commands)**

Run:
```bash
go build ./...
```

Expected: build errors naming `initialResultsCmd`, `refreshTopLevelCmd`, `resolveAccountCmd` — these are implemented in the next task. This is fine; do not commit yet.

---

## Task 15: Background commands — refresh, initial load, account

**Files:**
- Create: `internal/tui/commands.go`

- [ ] **Step 1: Create `internal/tui/commands.go`**

Create `internal/tui/commands.go`:
```go
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awss3 "github.com/wmattei/scout/internal/awsctx/s3"
	awsecs "github.com/wmattei/scout/internal/awsctx/ecs"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/search"
)

// initialResultsCmd produces the first render's results from whatever the
// in-memory index currently holds. It does not hit AWS.
func initialResultsCmd(mem *index.Memory, query string) tea.Cmd {
	return func() tea.Msg {
		results := search.Fuzzy(query, mem.All(), 200)
		return msgResults{results: results}
	}
}

// refreshTopLevelCmd kicks off SWR refresh for buckets + ecs services +
// ecs task-def families concurrently. Each subtask writes its results to
// both the in-memory index and the SQLite cache, then emits
// msgResourcesUpdated so the UI can re-render.
//
// Errors are swallowed for Phase 1 — the user sees a stale cache with no
// indication of what went wrong. Phase 4 adds an error toast.
func refreshTopLevelCmd(ac *awsctx.Context, db *index.DB, mem *index.Memory) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		type subtaskResult struct {
			typ core.ResourceType
			rs  []core.Resource
			err error
		}
		done := make(chan subtaskResult, 3)

		go func() {
			rs, err := awss3.ListBuckets(ctx, ac)
			done <- subtaskResult{core.RTypeBucket, rs, err}
		}()
		go func() {
			rs, err := awsecs.ListServices(ctx, ac)
			done <- subtaskResult{core.RTypeEcsService, rs, err}
		}()
		go func() {
			rs, err := awsecs.ListTaskDefFamilies(ctx, ac)
			done <- subtaskResult{core.RTypeEcsTaskDefFamily, rs, err}
		}()

		for i := 0; i < 3; i++ {
			res := <-done
			if res.err != nil {
				// Phase 4: forward to error toast. For now, drop.
				continue
			}
			persist(ctx, db, mem, res.typ, res.rs)
		}
		return msgResourcesUpdated{}
	}
}

// persist applies a diff-patch: upsert all received resources, then delete
// any resources of this type that were NOT in the fresh set. Writes go to
// the in-memory index first (instant UI snap) and then to SQLite.
func persist(ctx context.Context, db *index.DB, mem *index.Memory, t core.ResourceType, rs []core.Resource) {
	// 1. In-memory: upsert + delete-missing for this type.
	keep := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		keep[r.Key] = struct{}{}
	}
	mem.Upsert(rs)
	mem.DeleteMissing(t, keep)

	// 2. Persist to SQLite.
	_ = db.UpsertResources(ctx, rs)
	_ = db.DeleteMissing(ctx, t, keep)
}

// resolveAccountCmd calls sts:GetCallerIdentity once and reports the account
// ID (or a blank on error) to the TUI.
func resolveAccountCmd(ac *awsctx.Context) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		acct, _ := ac.CallerIdentity(ctx)
		return msgAccount{account: acct}
	}
}
```

- [ ] **Step 2: Verify the TUI package compiles**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum internal/tui/model.go internal/tui/update.go internal/tui/view.go internal/tui/commands.go
git commit -m "feat(tui): add bubbletea model, update, view, and background commands"
```

---

## Task 16: Wire `main.go` to the TUI

**Files:**
- Modify: `cmd/scout/main.go`

- [ ] **Step 1: Replace `cmd/scout/main.go`**

Overwrite `cmd/scout/main.go`:
```go
// Command scout is an interactive TUI for navigating AWS resources.
// Phase 1 covers foundation + top-level search only; later phases add
// drill-in navigation, detail views, actions, and a profile switcher.
package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/tui"
)

const Version = "0.0.0-phase1"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "scout: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// 1. Resolve the AWS environment up front so we fail fast on bad creds.
	awsCtx, err := awsctx.Resolve(ctx)
	if err != nil {
		return err
	}

	// 2. Attach the activity counter so every subsequent SDK call is tracked.
	activity := awsctx.NewActivity()
	activity.Attach(&awsCtx.Cfg)

	// 3. Open the cache DB scoped to (profile, region).
	db, err := index.Open(awsCtx.Profile, awsCtx.Region)
	if err != nil {
		return err
	}
	defer db.Close()

	// 4. Build the in-memory index from whatever's cached on disk.
	memory := index.NewMemory()
	cached, err := db.LoadAll(ctx)
	if err != nil {
		return err
	}
	memory.Load(cached)

	// 5. Launch the bubbletea program.
	model := tui.NewModel(memory, db, awsCtx, activity)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build -o bin/scout ./cmd/scout
```

Expected: produces `bin/scout`.

- [ ] **Step 3: Smoke-run the binary**

Run:
```bash
./bin/scout
```

**Expected UI** (assuming valid AWS credentials in the shell):
- Launches into an alt-screen TUI with an empty input bar labeled `> ` and a 🔍 glyph.
- Status bar at the bottom shows `profile=<your profile>  region=<your region>  acct=<resolving>`.
- Within a few seconds, the activity spinner animates next to the op name (`ListBuckets`, `ListServices`, etc).
- Results stream in as SWR subtasks complete.
- Typing filters the visible list in real time with highlighted match chars.
- `↑`/`↓` moves selection.
- `Enter`, `Tab`, `Ctrl+P`, `Ctrl+R`, `Esc` do nothing (reserved).
- `Ctrl+C` quits cleanly, restoring the terminal.

**Troubleshooting:**
- If it errors out immediately on "no region resolved", `export AWS_REGION=us-east-1` (or wherever) and retry.
- If `ListServices` fails, the error is silently swallowed in Phase 1. Run against a profile with `ecs:ListClusters` permission to see services in the list.

- [ ] **Step 4: Commit**

```bash
git add cmd/scout/main.go
git commit -m "feat(cmd): wire bubbletea program to AWS, SQLite, and search layers"
```

---

## Task 17: Ensure the result list recomputes when SWR lands

**Files:**
- Modify: `internal/tui/commands.go`

Note: this is a follow-up verification task for a subtle behavior. After Task 15 the `msgResourcesUpdated` handler already recomputes results, but only triggers on a completed refresh. The goal of this task is to verify it visibly works end-to-end and fix any oversight.

- [ ] **Step 1: Add per-subtask updates instead of a single batched message**

Modify `internal/tui/commands.go`. Change the `for i := 0; i < 3; i++` loop in `refreshTopLevelCmd` to emit a message per subtask so the UI updates as each list returns, rather than waiting for all three.

Replace the body of the returned function in `refreshTopLevelCmd` with:
```go
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

		type subtaskResult struct {
			typ core.ResourceType
			rs  []core.Resource
			err error
		}

		// Run the three subtasks sequentially for Phase 1. Interleaved
		// updates are a Phase 2 improvement; Phase 1 prioritizes
		// simplicity and determinism.
		subtasks := []func() subtaskResult{
			func() subtaskResult {
				rs, err := awss3.ListBuckets(ctx, ac)
				return subtaskResult{core.RTypeBucket, rs, err}
			},
			func() subtaskResult {
				rs, err := awsecs.ListServices(ctx, ac)
				return subtaskResult{core.RTypeEcsService, rs, err}
			},
			func() subtaskResult {
				rs, err := awsecs.ListTaskDefFamilies(ctx, ac)
				return subtaskResult{core.RTypeEcsTaskDefFamily, rs, err}
			},
		}
		for _, run := range subtasks {
			res := run()
			if res.err == nil {
				persist(ctx, db, mem, res.typ, res.rs)
			}
		}
		cancel()
		return msgResourcesUpdated{}
	}
```

(This is still a single-message return; a true per-subtask stream requires `tea.Program.Send` or a tea.Cmd sequence — the simpler sequential version above is good enough for Phase 1. The parallel version is revisited in Phase 4.)

- [ ] **Step 2: Verify it still builds**

Run:
```bash
go build -o bin/scout ./cmd/scout
```

Expected: no output, binary produced.

- [ ] **Step 3: Smoke-run and verify the fuzzy match + refresh behavior**

Run:
```bash
./bin/scout
```

Expected behavior:
1. On launch, the list is either empty (first run, cache is cold) or populated from the last run's SQLite cache (subsequent runs).
2. The activity spinner animates as refresh runs.
3. When refresh completes, `msgResourcesUpdated` fires, results are recomputed against the in-memory index, and the list updates in place without flicker.
4. Second launch is instant — the cache loads from `~/.cache/scout/<profile>__<region>.db` before any AWS calls happen.

If refresh is not updating the list: check that `persist` is being called (add a temporary `fmt.Fprintln(os.Stderr, …)` under it, re-run, remove).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go
git commit -m "fix(tui): run refresh subtasks sequentially for deterministic SWR"
```

---

## Task 18: Minimum width + small-terminal safety

**Files:**
- Modify: `internal/tui/update.go`

Already partially handled in Task 14's `View()`. This task adds an explicit key handler so the UI doesn't process typing when the terminal is too narrow.

- [ ] **Step 1: Early-return in Update when width < 60**

Modify `Update` in `internal/tui/update.go`. Immediately after the `tea.KeyMsg` case opens, add:
```go
	case tea.KeyMsg:
		// Don't process anything except quit when the terminal is too narrow.
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
```

The complete `tea.KeyMsg` block should read:
```go
	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil
		case "enter", "tab", "ctrl+p", "ctrl+r", "esc":
			return m, nil
		}

		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		results := search.Fuzzy(m.input.Value(), m.memory.All(), max(1, m.height-3))
		m.results = results
		if m.selected >= len(m.results) {
			m.selected = len(m.results) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		return m, cmd
```

- [ ] **Step 2: Verify it builds**

Run:
```bash
go build -o bin/scout ./cmd/scout
```

Expected: no output.

- [ ] **Step 3: Verify the "too narrow" screen**

Run:
```bash
./bin/scout
```

Resize the terminal to fewer than 60 columns. The entire frame should collapse to a centered red error message. Resize back and the normal UI should reappear. Ctrl+C still quits.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/update.go
git commit -m "feat(tui): guard input handling when terminal is too narrow"
```

---

## Task 19: Phase 1 smoke-test checklist

**Files:** none — verification only.

This task confirms Phase 1 is done. Run each of the following and fix any issues found before moving to Phase 2.

- [ ] **Step 1: First-launch (cold cache) scenario**

Run:
```bash
rm -rf ~/.cache/scout
go build -o bin/scout ./cmd/scout
./bin/scout
```

Verify:
- Frame draws immediately with an empty list and `cache is empty — fetching…` placeholder.
- Spinner animates with live op names (`ListBuckets`, `ListClusters`, `ListServices`, `ListTaskDefinitionFamilies`).
- Results populate progressively as each subtask completes (buckets first is common because it's the cheapest).
- Ctrl+C quits cleanly, terminal is restored.

- [ ] **Step 2: Second-launch (warm cache) scenario**

Run:
```bash
./bin/scout
```

Verify:
- Results are visible within one render frame (no waiting on network).
- Spinner still animates while the background refresh runs.
- Ctrl+C quits cleanly.

- [ ] **Step 3: Fuzzy search scenario**

Run the binary, type a few characters matching one of your real resource names. Verify:
- Rows re-rank as you type.
- Matched characters in the name are bolded/brightened.
- Tag color identifies the resource type ([S3], [ECS], [TASK]).
- `↑`/`↓` moves selection with a visible highlight bar.
- Backspace removes characters and the list broadens again.

- [ ] **Step 4: Terminal resize scenario**

Resize the terminal wider / narrower while the tool is running. Verify:
- Columns re-flow cleanly.
- At < 60 cols, the "terminal too narrow" screen shows.
- Resizing back restores normal operation.

- [ ] **Step 5: Bad credentials scenario**

Run:
```bash
AWS_PROFILE=nonexistent ./bin/scout
```

Verify:
- Exits 1 to the shell with a message like `scout: loading AWS config (profile=nonexistent): …` on stderr. The TUI must not launch with invalid credentials — this is the Phase 1 equivalent of the "no silent failures" rule.

- [ ] **Step 6: Reserved-key scenario**

Launch the binary. Press Enter, Tab, Esc, Ctrl+P, Ctrl+R. Verify none of them do anything visible (they're reserved for later phases). Typing printable characters should still filter the list.

- [ ] **Step 7: Commit anything that got fixed up**

If any of the above revealed a bug and you edited a file to fix it, commit the fix:

```bash
git status
git add <files>
git commit -m "fix: <one-line description of what smoke test revealed>"
```

If nothing needed fixing, tag Phase 1 as complete:

```bash
git tag phase-1-complete
```

---

## Phase 1 complete — next up

At this point the project has:
- A working TUI that launches instantly from a local SQLite cache.
- Background SWR refresh for buckets, ECS services, and ECS task-def families with opportunistic persistence.
- Fuzzy matching with per-character highlight spans and color-coded type tags.
- An activity spinner driven by an SDK middleware that covers every AWS call automatically.
- Clean credential-failure handling and a minimum-width guard.

**Phase 2 plan** will cover: Tab-driven drill-in into S3 buckets, scoped prefix search with opportunistic caching of folders/objects, backspace-driven scope pop, and the Details mode view (read-only Name + ARN, stub actions list). It builds directly on the packages created here — no refactoring required.
