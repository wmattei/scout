# better-aws-cli — Phase 2: Drill-In & Details View

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The user can Tab through S3 buckets into their contents, navigate folder prefixes via the input bar as a breadcrumb, and press Enter on any row to see a Details view with the resource's Name and ARN plus a stubbed Actions list.

**Architecture:** Keep the input bar as the single source of truth for scope — presence of `/` in the input switches from top-level fuzzy mode to scoped S3 mode. Scoped mode does a synchronous SQLite cache read for first paint and dispatches a `ListObjectsV2` live call in parallel; live results stream back, replace the cache results in the UI, and get opportunistically upserted to `bucket_contents`. A new `Mode` enum on the model routes `Update`/`View` between the existing search screen and a new Details screen. Actions are declared as per-type slices and always resolve to a "not yet implemented — Phase 3" toast for now. A small toast component replaces nothing — it's the first piece of the eventual error-overlay infrastructure.

**Tech Stack:** Same as Phase 1 — Go 1.22+, `aws-sdk-go-v2`, `bubbletea`, `lipgloss`, `modernc.org/sqlite`, `sahilm/fuzzy`. Nothing new.

**Scope boundary (what Phase 2 does NOT include):**
- Real action execution (Open in Browser, Copy URI, Copy ARN, Force Deploy, Tail Logs, Download, Preview) → **Phase 3**
- `Ctrl+P` profile/region switcher → **Phase 4**
- Panic recovery, debug log, cache-clear subcommand → **Phase 4**
- Error toast wired to AWS failures — the toast component exists but only fires from action stubs → **Phase 4**
- Multi-container log group picker, revision pinning → **Phase 3**
- Bulk-crawl (`Ctrl+R` inside a scoped view) → out-of-scope for Phase 2; can be added anytime once the DB helpers exist

**Reference spec:** `docs/superpowers/specs/2026-04-13-better-aws-cli-v0-design.md`

**Working directory:** `/Users/wagnermattei/www/pied-piper/better-aws-cli`. Every shell command assumes this CWD.

**Testing policy:** No automated tests in v0. Every task ends with `go build ./...` followed by `git commit`.

**Branch:** Work on `phase-2/drill-in-and-details-view`. All commits below are expected on this branch.

---

## File map

**New files**

| Path | Responsibility |
|---|---|
| `internal/search/scope.go` | Parse input into `(bucket, prefix, leaf)`, expose `Scope.IsTopLevel()` |
| `internal/search/prefix.go` | Prefix-match helper returning `search.Result`s with leading-prefix highlight spans |
| `internal/awsctx/s3/objects.go` | `ListAtPrefix(bucket, prefix)` wrapping `ListObjectsV2` with `Delimiter="/"` |
| `internal/index/bucket_contents.go` | `UpsertBucketContents`, `QueryBucketContents` methods on `*DB` |
| `internal/tui/mode.go` | `Mode` enum and its `String()` helper |
| `internal/tui/actions.go` | `Action` struct + `ActionsFor(ResourceType)` returning per-type action slices |
| `internal/tui/details.go` | `renderDetails(m Model) string` for the Details screen |
| `internal/tui/toast.go` | `Toast` struct + `renderToast` overlay helper |

**Modified files**

| Path | What changes |
|---|---|
| `internal/tui/model.go` | Add `mode`, `detailsResource`, `actionSel`, `toast`, `scopedResults`, `scopedQueryAt` fields; add default `mode: modeSearch` in `NewModel` |
| `internal/tui/update.go` | Key routing by mode, Tab drill-in, scope-aware `computeResults`, new message types (`msgScopedResults`, `msgToastDismiss`) |
| `internal/tui/view.go` | Dispatch to `renderDetails` when `mode == modeDetails`; overlay `renderToast` when active |
| `internal/tui/commands.go` | Add `scopedSearchCmd` for cache-read + live-fetch + persist |
| `internal/tui/results.go` | Tolerate folder/object rows (meta for folders = mtime, for objects = size + mtime) |

---

## Task 1: Scope parser

**Files:**
- Create: `internal/search/scope.go`

- [ ] **Step 1: Create the file**

Create `internal/search/scope.go` with EXACTLY this content:

```go
package search

import "strings"

// Scope describes how the TUI should interpret the current input value.
//
// A top-level scope (Bucket == "") means the search engine should run
// fuzzy against the in-memory index of cached S3 buckets, ECS services,
// and ECS task-def families.
//
// A scoped value (Bucket != "") means the search should be a prefix-based
// lookup inside that S3 bucket. Prefix is the full key prefix passed to
// ListObjectsV2 (always ending on a `/` boundary or at the very start).
// Leaf is the characters after the last `/` in the input — the part that
// gets highlighted in result rows.
type Scope struct {
	Raw    string // the original input string, verbatim
	Bucket string
	Prefix string
	Leaf   string
}

// IsTopLevel reports whether the scope has no bucket selected yet.
func (s Scope) IsTopLevel() bool { return s.Bucket == "" }

// ParseScope converts an input string into a Scope.
//
// Examples:
//
//	""                          -> top-level, Leaf=""
//	"my-bu"                     -> top-level, Leaf="my-bu"
//	"my-bucket/"                -> bucket=my-bucket, Prefix="", Leaf=""
//	"my-bucket/logs/"           -> bucket=my-bucket, Prefix="logs/", Leaf=""
//	"my-bucket/logs/2026"       -> bucket=my-bucket, Prefix="logs/", Leaf="2026"
//	"my-bucket/logs/2026/file"  -> bucket=my-bucket, Prefix="logs/2026/", Leaf="file"
func ParseScope(input string) Scope {
	s := Scope{Raw: input}

	slash := strings.IndexByte(input, '/')
	if slash < 0 {
		// No slash means we are still at the top level. The whole input is
		// the leaf used for fuzzy matching.
		s.Leaf = input
		return s
	}

	s.Bucket = input[:slash]
	rest := input[slash+1:]

	lastSlash := strings.LastIndexByte(rest, '/')
	if lastSlash < 0 {
		// No additional slash past the bucket name. The entire remainder
		// is a leaf search under the bucket root.
		s.Prefix = ""
		s.Leaf = rest
		return s
	}

	// Prefix includes the trailing '/' so it is safe to hand directly to
	// ListObjectsV2 as a prefix.
	s.Prefix = rest[:lastSlash+1]
	s.Leaf = rest[lastSlash+1:]
	return s
}
```

- [ ] **Step 2: Build**

Run:
```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/search/scope.go
git commit -m "feat(search): add scope parser for breadcrumb-driven input"
```

---

## Task 2: Prefix matcher

**Files:**
- Create: `internal/search/prefix.go`

- [ ] **Step 1: Create the file**

Create `internal/search/prefix.go` with EXACTLY this content:

```go
package search

import (
	"sort"
	"strings"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// Prefix runs a case-sensitive prefix match of `query` against each
// resource in `all`, returning up to `limit` results sorted with folders
// before objects and otherwise lexicographically. MatchedRunes on each
// result spans the leading prefix positions so the TUI can render the
// matching chars in the highlight style.
//
// An empty query returns everything up to `limit` unranked — useful
// when the user has just drilled into a bucket and hasn't typed anything
// beyond the trailing `/`.
func Prefix(query string, all []core.Resource, limit int) []Result {
	var matched []core.Resource
	if query == "" {
		matched = make([]core.Resource, len(all))
		copy(matched, all)
	} else {
		matched = make([]core.Resource, 0, len(all))
		for _, r := range all {
			if strings.HasPrefix(r.DisplayName, query) {
				matched = append(matched, r)
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		// Folders before objects.
		ti := folderFirst(matched[i].Type)
		tj := folderFirst(matched[j].Type)
		if ti != tj {
			return ti < tj
		}
		return matched[i].DisplayName < matched[j].DisplayName
	})

	// Precompute the leading-prefix highlight span once; every match shares
	// the same [0..len(query)) byte positions.
	var matchPositions []int
	if query != "" {
		matchPositions = make([]int, 0, len(query))
		for i := 0; i < len(query); i++ {
			matchPositions = append(matchPositions, i)
		}
	}

	upto := minInt(limit, len(matched))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		out = append(out, Result{
			Resource:     matched[i],
			MatchedRunes: matchPositions,
			Score:        0,
		})
	}
	return out
}

// folderFirst gives folders priority 0 and everything else priority 1.
func folderFirst(t core.ResourceType) int {
	if t == core.RTypeFolder {
		return 0
	}
	return 1
}
```

- [ ] **Step 2: Build**

Run:
```bash
go build ./...
```

Expected: no output. `minInt` is already defined in `internal/search/fuzzy.go`.

- [ ] **Step 3: Commit**

```bash
git add internal/search/prefix.go
git commit -m "feat(search): add prefix matcher for scoped S3 mode"
```

---

## Task 3: S3 ListAtPrefix adapter

**Files:**
- Create: `internal/awsctx/s3/objects.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/s3/objects.go` with EXACTLY this content:

```go
package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListAtPrefix lists folders (virtual, via CommonPrefixes) and objects
// directly under `prefix` in the given bucket. The call uses delimiter "/"
// so we only walk one level at a time, matching the TUI's breadcrumb
// navigation model.
//
// Returned resources are paginated in full; the caller gets a flat slice.
// DisplayName for folders is the trailing segment including the `/`
// (e.g. "logs/"); for objects, it's the trailing segment without a slash
// (e.g. "2026-04-13.csv"). Key for folders is the full key relative to
// the bucket root (e.g. "app/logs/"); for objects, it's the full key
// (e.g. "app/logs/2026-04-13.csv"). Meta carries bucket, plus size/mtime
// for objects.
func ListAtPrefix(ctx context.Context, ac *awsctx.Context, bucket, prefix string) ([]core.Resource, error) {
	client := awss3.NewFromConfig(ac.Cfg)

	var out []core.Resource
	var token *string
	for {
		page, err := client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3:ListObjectsV2 (bucket=%s prefix=%s): %w", bucket, prefix, err)
		}

		for _, p := range page.CommonPrefixes {
			if p.Prefix == nil {
				continue
			}
			full := *p.Prefix
			out = append(out, core.Resource{
				Type:        core.RTypeFolder,
				Key:         full,
				DisplayName: lastSegmentWithSlash(full),
				Meta: map[string]string{
					"bucket": bucket,
				},
			})
		}
		for _, o := range page.Contents {
			if o.Key == nil {
				continue
			}
			full := *o.Key
			// Skip the "placeholder" row that equals the prefix itself —
			// some tools create a zero-byte marker at the folder key.
			if full == prefix {
				continue
			}
			meta := map[string]string{"bucket": bucket}
			if o.Size != nil {
				meta["size"] = fmt.Sprintf("%d", *o.Size)
			}
			if o.LastModified != nil {
				meta["mtime"] = fmt.Sprintf("%d", o.LastModified.Unix())
			}
			out = append(out, core.Resource{
				Type:        core.RTypeObject,
				Key:         full,
				DisplayName: lastSegment(full),
				Meta:        meta,
			})
		}

		if page.IsTruncated == nil || !*page.IsTruncated {
			break
		}
		token = page.NextContinuationToken
	}
	return out, nil
}

// lastSegmentWithSlash returns the final path segment of a CommonPrefix,
// preserving the trailing slash. "a/b/c/" -> "c/".
func lastSegmentWithSlash(s string) string {
	trimmed := strings.TrimSuffix(s, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[i+1:] + "/"
	}
	return trimmed + "/"
}

// lastSegment returns the final path segment of an object key. "a/b/c.txt"
// -> "c.txt". If the key has no slash, it returns the whole key.
func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
```

- [ ] **Step 2: Build**

Run:
```bash
go build ./...
```

Expected: no output. The `aws-sdk-go-v2/aws` package is already a transitive dep via s3; `go mod tidy` may want to promote it to direct — run it just in case:

```bash
go mod tidy
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum internal/awsctx/s3/objects.go
git commit -m "feat(aws/s3): add ListAtPrefix for scoped folder/object search"
```

---

## Task 4: DB bucket_contents methods

**Files:**
- Create: `internal/index/bucket_contents.go`

- [ ] **Step 1: Create the file**

Create `internal/index/bucket_contents.go` with EXACTLY this content:

```go
package index

import (
	"context"
	"fmt"

	"github.com/wagnermattei/better-aws-cli/internal/core"
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
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/index/bucket_contents.go
git commit -m "feat(index): add bucket_contents upsert + direct-child query"
```

---

## Task 5: Mode enum

**Files:**
- Create: `internal/tui/mode.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/mode.go` with EXACTLY this content:

```go
package tui

// Mode represents which top-level screen the bubbletea program is
// currently showing. Phase 2 introduces the first mode split: search
// versus details. Phase 4 will add the profile/region switcher overlay.
type Mode int

const (
	// modeSearch is the default — input bar + result list. The input bar
	// doubles as a breadcrumb for scoped S3 navigation.
	modeSearch Mode = iota

	// modeDetails shows the Details panel + Actions list for a selected
	// resource. All actions are stubbed in Phase 2 and surface a toast
	// on activation; Phase 3 implements them for real.
	modeDetails
)

// String returns a short debug name for the mode.
func (m Mode) String() string {
	switch m {
	case modeSearch:
		return "search"
	case modeDetails:
		return "details"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: no output. This file adds constants but nothing consumes them yet; Go is happy with unused package-level consts.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/mode.go
git commit -m "feat(tui): add Mode enum (search, details)"
```

---

## Task 6: Actions declaration

**Files:**
- Create: `internal/tui/actions.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/actions.go` with EXACTLY this content:

```go
package tui

import "github.com/wagnermattei/better-aws-cli/internal/core"

// Action is a single selectable entry in the Details view's Actions list.
// Phase 2 only uses Label — execution is stubbed and uniformly produces a
// "not yet implemented — Phase 3" toast. Phase 3 will add an Execute
// function field (or swap this struct for an interface).
type Action struct {
	Label string
}

// ActionsFor returns the ordered action list for a resource type. The
// ordering matches the spec's actions matrix and determines the number
// hotkey assigned to each action (first item is `1`, second is `2`, etc.).
//
// Unknown types return an empty slice; the Details view handles that
// gracefully by showing only the Details panel.
func ActionsFor(t core.ResourceType) []Action {
	switch t {
	case core.RTypeBucket:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
		}
	case core.RTypeFolder:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
		}
	case core.RTypeObject:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy URI"},
			{Label: "Copy ARN"},
			{Label: "Download"},
			{Label: "Preview"},
		}
	case core.RTypeEcsService:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Force new Deployment"},
			{Label: "Tail Logs"},
		}
	case core.RTypeEcsTaskDefFamily:
		return []Action{
			{Label: "Open in Browser"},
			{Label: "Copy ARN"},
			{Label: "Tail Logs"},
		}
	default:
		return nil
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/actions.go
git commit -m "feat(tui): declare per-resource-type action lists"
```

---

## Task 7: Toast component

**Files:**
- Create: `internal/tui/toast.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/toast.go` with EXACTLY this content:

```go
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Toast is a transient bottom-centered overlay displayed over whatever
// screen is currently rendered. Phase 2 only shows toasts for action
// stubs; Phase 4 re-uses the same machinery for AWS errors and credential
// failures.
//
// A zero-valued Toast is "inactive": renderToast returns "" and the view
// layer skips the overlay.
type Toast struct {
	Message   string
	ExpiresAt time.Time
}

// newToast returns a Toast that expires after `dur` from now.
func newToast(message string, dur time.Duration) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(dur),
	}
}

// isActive reports whether the toast should currently render.
func (t Toast) isActive() bool {
	return t.Message != "" && time.Now().Before(t.ExpiresAt)
}

// renderToast returns a single-line overlay string centered horizontally,
// or "" if the toast is inactive. The caller is responsible for composing
// this into the final frame (replacing the bottom divider for the toast's
// lifetime). width is the full frame width.
func renderToast(t Toast, width int) string {
	if !t.isActive() {
		return ""
	}
	const padding = 2
	msg := t.Message
	inner := " " + msg + " "
	if lipglossWidth(inner) > width-padding {
		inner = inner[:width-padding-1] + "…"
	}
	boxed := styleToast.Render(inner)
	left := (width - lipglossWidth(boxed)) / 2
	if left < 0 {
		left = 0
	}
	return strings.Repeat(" ", left) + boxed
}

// styleToast is defined here rather than in styles.go to keep the toast
// component fully self-contained — it is the only consumer. Phase 4 can
// promote it to styles.go once error-surface toasts share the look.
var styleToast = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
	Background(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#5F005F"}).
	Padding(0, 1)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/toast.go
git commit -m "feat(tui): add transient toast overlay component"
```

---

## Task 8: Add scope + details state to Model

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Replace the Model struct and `NewModel`**

Overwrite `internal/tui/model.go` to read EXACTLY as follows:

```go
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Model is the bubbletea model for the search + details views. Phase 2
// introduces a Mode split: modeSearch runs the input bar + result list,
// modeDetails runs the Details panel + Actions list for a chosen row.
type Model struct {
	// Injected dependencies.
	memory   *index.Memory
	db       *index.DB
	awsCtx   *awsctx.Context
	activity *awsctx.Activity

	// Shared UI state.
	input    textinput.Model
	width    int
	height   int
	account  string
	spinTick int
	toast    Toast
	mode     Mode

	// Search-mode state.
	selected      int
	results       []search.Result
	scopedResults []search.Result // populated in scoped mode from cache + live
	scopedQuery   string          // the input value that produced scopedResults

	// Details-mode state.
	detailsResource core.Resource
	actionSel       int

	// Unused in Phase 2; reserved for Phase 4's refresh progress tracking.
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
		mode:     modeSearch,
	}
}

// Init is called once when the program starts. Phase 1 left the initial
// result list empty on purpose; Phase 2 preserves that behavior.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
		spinTickCmd(),
		resolveAccountCmd(m.awsCtx),
	)
}
```

- [ ] **Step 2: Build (expect failure)**

```bash
go build ./...
```

Expected: the build will likely fail with references to `Toast`, `Mode`, `modeSearch` (which exist from earlier tasks) all resolving, but may fail on fields `m.results` / `m.selected` that are now shared with new fields the rest of `update.go` and `view.go` haven't been updated to know about. Any such errors are fine — we'll chase them in Task 9.

If the build succeeds, even better. Either way, **do not commit yet** — this task's changes are wired together with Task 9's update.go rewrite.

- [ ] **Step 3: Stage but do not commit**

```bash
git add internal/tui/model.go
git status
```

Leave the staged change. Task 9 will make a single combined commit for the two files.

---

## Task 9: Rewrite `update.go` for scope + modes + Tab drill-in

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Overwrite `internal/tui/update.go`**

Replace the file with EXACTLY:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Custom messages emitted by commands.
type (
	msgResourcesUpdated struct{}
	msgAccount          struct{ account string }
	msgSpinTick         struct{}

	// msgScopedResults carries the merged cache+live result set for a
	// scoped (bucket/prefix) search. `query` is the exact input value
	// that produced these results — the handler drops the message if
	// the input has moved on since, so stale results can't clobber
	// fresher ones.
	msgScopedResults struct {
		query   string
		results []search.Result
	}
)

// Update routes messages to state mutations and side-effect commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		default:
			return m.updateSearch(msg)
		}

	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current top-level list against the updated snapshot.
		m.results = computeResults(m.input.Value(), m.memory)
		m.clampSelected()
		return m, nil

	case msgScopedResults:
		// Drop the message if the input has moved on since the command
		// was issued. This prevents stale ListObjectsV2 responses from
		// clobbering the results for a query the user has already typed
		// past.
		if msg.query != m.input.Value() {
			return m, nil
		}
		m.scopedResults = msg.results
		m.scopedQuery = msg.query
		m.clampSelected()
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

// updateSearch handles key events while in modeSearch.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down":
		visible := m.visibleSearchResults()
		if m.selected < len(visible)-1 {
			m.selected++
		}
		return m, nil
	case "enter":
		// Enter the Details view for the currently selected row.
		visible := m.visibleSearchResults()
		if len(visible) == 0 {
			return m, nil
		}
		if m.selected < 0 || m.selected >= len(visible) {
			return m, nil
		}
		m.detailsResource = visible[m.selected].Resource
		m.actionSel = 0
		m.mode = modeDetails
		return m, nil
	case "tab":
		return m.handleTab()
	case "ctrl+p", "ctrl+r", "esc":
		// Reserved for later phases. No-op in Phase 2.
		return m, nil
	}

	// Let the textinput consume the keystroke, then recompute.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m.recomputeResults(cmd)
}

// updateDetails handles key events while in modeDetails.
func (m Model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	actions := ActionsFor(m.detailsResource.Type)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeSearch
		m.actionSel = 0
		return m, nil
	case "up":
		if m.actionSel > 0 {
			m.actionSel--
		}
		return m, nil
	case "down":
		if m.actionSel < len(actions)-1 {
			m.actionSel++
		}
		return m, nil
	case "enter":
		if len(actions) == 0 {
			return m, nil
		}
		m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
		return m, nil
	}
	// Number hotkeys 1..9 for direct selection + execution.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(actions) {
				m.actionSel = idx
				m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
				return m, nil
			}
		}
	}
	return m, nil
}

// handleTab implements Tab drill-in. When a bucket or folder row is
// selected, Tab replaces the input value with that row's full path and
// appends a trailing `/` so the scope advances on the next recompute.
// For leaf rows (objects, ECS services, ECS task-def families) Tab
// replaces the input with the row's name without a trailing separator.
func (m Model) handleTab() (tea.Model, tea.Cmd) {
	visible := m.visibleSearchResults()
	if len(visible) == 0 {
		return m, nil
	}
	if m.selected < 0 || m.selected >= len(visible) {
		return m, nil
	}
	row := visible[m.selected].Resource

	scope := search.ParseScope(m.input.Value())
	var newInput string
	switch row.Type {
	case core.RTypeBucket:
		newInput = row.Key + "/"
	case core.RTypeFolder:
		// row.Key is the full relative key under the bucket
		// (e.g. "logs/2026/"). Reconstruct "bucket/logs/2026/".
		newInput = scope.Bucket + "/" + row.Key
	case core.RTypeObject:
		// Object keys don't get a trailing slash.
		newInput = scope.Bucket + "/" + row.Key
	default:
		// Top-level leaves (ECS service, ECS task-def family) — replace
		// the input with the display name so subsequent text matches the
		// current selection.
		newInput = row.DisplayName
	}
	m.input.SetValue(newInput)
	m.input.CursorEnd()
	return m.recomputeResults(nil)
}

// recomputeResults recomputes the result list based on the current input
// and returns the combined tea.Cmd for text-input update and any
// follow-up scoped-search command. `cmd` is the command already produced
// by the text-input update (or nil if none).
func (m Model) recomputeResults(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	scope := search.ParseScope(m.input.Value())

	if scope.IsTopLevel() {
		m.results = computeResults(m.input.Value(), m.memory)
		m.scopedResults = nil
		m.scopedQuery = ""
		m.clampSelected()
		return m, cmd
	}

	// Scoped mode: clear top-level results, trigger the scoped search.
	m.results = nil
	m.clampSelected()
	scoped := scopedSearchCmd(m.awsCtx, m.db, m.input.Value())
	if cmd != nil {
		return m, tea.Batch(cmd, scoped)
	}
	return m, scoped
}

// visibleSearchResults returns whichever result list is currently active
// (scoped or top-level) so arrow keys and Enter operate on the same set
// the user is seeing.
func (m Model) visibleSearchResults() []search.Result {
	scope := search.ParseScope(m.input.Value())
	if !scope.IsTopLevel() {
		return m.scopedResults
	}
	return m.results
}

// clampSelected keeps the selected index within the visible list bounds.
func (m *Model) clampSelected() {
	n := len(m.visibleSearchResults())
	if n == 0 {
		m.selected = 0
		return
	}
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// computeResults returns the fuzzy match results for a TOP-LEVEL query,
// or an empty slice if the query is empty.
func computeResults(query string, mem *index.Memory) []search.Result {
	if query == "" {
		return nil
	}
	return search.Fuzzy(query, mem.All(), MaxDisplayedResults)
}

// spinTickCmd schedules the next spinner frame.
func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}
```

**Note:** `tea.Batch(cmd, scoped)` is safe even when `cmd` is a wrapped textinput command — bubbletea handles nil commands inside a Batch.

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: build will FAIL with `undefined: scopedSearchCmd` — that function is added in Task 10. This is expected; leave the files staged.

If any OTHER error appears (typo, missing field, wrong arg count), fix it before proceeding.

- [ ] **Step 3: Stage update.go alongside model.go from Task 8**

```bash
git add internal/tui/update.go
git status
```

Do not commit yet. Task 10 will add `scopedSearchCmd` and make a single combined commit.

---

## Task 10: Scoped search command

**Files:**
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Add `scopedSearchCmd` at the end of `internal/tui/commands.go`**

Append to `internal/tui/commands.go` (do not modify existing functions):

```go

// scopedSearchCmd runs the scoped (bucket/prefix) search behind modeSearch
// when the input contains a `/`. It reads the SQLite cache for first
// paint, fires a live ListObjectsV2 in parallel, persists every live
// result to bucket_contents, merges cache + live into a single slice, and
// returns the whole thing via msgScopedResults.
//
// The merge rule: live results win per (bucket, key) because they are
// authoritative for size/mtime. Results are ordered by the search.Prefix
// helper to match the TUI's display expectations.
func scopedSearchCmd(ac *awsctx.Context, db *index.DB, query string) tea.Cmd {
	return func() tea.Msg {
		scope := search.ParseScope(query)
		if scope.Bucket == "" {
			return msgScopedResults{query: query, results: nil}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 1. Cache read — fast, authoritative for first paint.
		cached, _ := db.QueryBucketContents(ctx, scope.Bucket, scope.Prefix)

		// 2. Live ListObjectsV2 at the exact prefix.
		live, err := awss3.ListAtPrefix(ctx, ac, scope.Bucket, scope.Prefix)
		if err != nil {
			// On live failure, return whatever was in the cache so the
			// UI still shows something. Phase 4's error toast will
			// surface the error itself. search.Prefix both filters by
			// the leaf and attaches the highlight span.
			return msgScopedResults{
				query:   query,
				results: search.Prefix(scope.Leaf, cached, MaxDisplayedResults),
			}
		}

		// 3. Persist the live results opportunistically.
		_ = db.UpsertBucketContents(ctx, scope.Bucket, live)

		// 4. Merge: live keys overwrite cache keys, then prefix-match
		//    against the leaf in a single pass.
		merged := mergeByKey(cached, live)
		results := search.Prefix(scope.Leaf, merged, MaxDisplayedResults)
		return msgScopedResults{query: query, results: results}
	}
}

// mergeByKey merges two resource slices, preferring entries from `b` when
// both slices contain the same Key. Returns a new slice; inputs are not
// mutated.
func mergeByKey(a, b []core.Resource) []core.Resource {
	index := make(map[string]int, len(a)+len(b))
	out := make([]core.Resource, 0, len(a)+len(b))
	for _, r := range a {
		index[r.Key] = len(out)
		out = append(out, r)
	}
	for _, r := range b {
		if i, ok := index[r.Key]; ok {
			out[i] = r
			continue
		}
		index[r.Key] = len(out)
		out = append(out, r)
	}
	return out
}
```

Also ensure the `search` package is imported again at the top of `commands.go` (it was removed in the Phase 1 cleanup). Add it back to the existing import block:

```go
import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean. All symbols (`scopedSearchCmd`, `mergeByKey`, `filterByLeaf`) are now defined; the Task 9 failure goes away.

- [ ] **Step 3: Commit (combined Task 8 + Task 9 + Task 10)**

```bash
git add internal/tui/commands.go
git status
git commit -m "feat(tui): wire scope state, mode routing, and scoped search command"
```

The single commit picks up all three files (`model.go`, `update.go`, `commands.go`) that were staged across Tasks 8, 9, and 10.

---

## Task 11: Details view rendering

**Files:**
- Create: `internal/tui/details.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/details.go` with EXACTLY this content:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// renderDetails produces the full Details screen for the current
// detailsResource and actionSel. width/height are the frame bounds.
//
// Layout:
//
//	┌──────────────────────────────────┐
//	│ Details                          │
//	│                                  │
//	│   Name   <name>                  │
//	│   ARN    <arn>                   │
//	│                                  │
//	│ Actions                          │
//	│                                  │
//	│ ▸ 1. Open in Browser             │
//	│   2. Copy URI                    │
//	│   ...                            │
//	│                                  │
//	└──────────────────────────────────┘
//
// The function returns just the body rows — the caller composes them
// alongside the input bar, dividers, and status line in view.go.
func renderDetails(r core.Resource, actionSel int, width int) string {
	var b strings.Builder

	// Header row for the Details section.
	b.WriteString(styleDetailsHeader.Render("Details"))
	b.WriteString("\n\n")

	// Field rows.
	writeField(&b, "Name", r.DisplayName)
	writeField(&b, "ARN", r.ARN())
	b.WriteString("\n")

	// Actions header.
	b.WriteString(styleDetailsHeader.Render("Actions"))
	b.WriteString("\n\n")

	actions := ActionsFor(r.Type)
	if len(actions) == 0 {
		b.WriteString(styleRowDim.Render("  (no actions available)"))
		return b.String()
	}

	for i, a := range actions {
		indi := "  "
		if i == actionSel {
			indi = styleSelIndi.Render("▸ ")
		}
		line := fmt.Sprintf("%s%d. %s", indi, i+1, a.Label)
		if i == actionSel {
			b.WriteString(styleRowSel.Width(width).Render(line))
		} else {
			b.WriteString(line)
		}
		if i < len(actions)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// writeField appends a single "  Label    Value" row to b.
func writeField(b *strings.Builder, label, value string) {
	b.WriteString("  ")
	b.WriteString(styleDetailsLabel.Render(padRightPlain(label, 6)))
	b.WriteString(" ")
	b.WriteString(value)
	b.WriteString("\n")
}

// padRightPlain right-pads a string to n runes with ASCII spaces. Kept
// separate from padRight in results.go because that one operates on
// already-styled strings via lipgloss.Width.
func padRightPlain(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// styleDetailsHeader styles the "Details" / "Actions" section headers.
var styleDetailsHeader = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})

// styleDetailsLabel dims the field label so values read brighter.
var styleDetailsLabel = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean. `renderDetails` is declared but not called yet; Task 12 wires it into `view.go`.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/details.go
git commit -m "feat(tui): render details screen with actions list"
```

---

## Task 12: Wire mode dispatch + toast overlay into `view.go`

**Files:**
- Modify: `internal/tui/view.go`

- [ ] **Step 1: Overwrite `internal/tui/view.go`**

Replace the file with EXACTLY:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full frame. The input bar, dividers, and status bar
// are shared across all modes; the middle zone is mode-specific.
func (m Model) View() string {
	// Minimum usable width check (per spec §7).
	if m.width < 60 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleError.Render(fmt.Sprintf("terminal too narrow — resize ≥60 columns (current: %d)", m.width)))
	}

	input := m.input.View()
	inputLine := fmt.Sprintf("%s%s", padRight(input, m.width-3), " 🔍")

	divider := styleDivider.Render(strings.Repeat("─", m.width))

	status := renderStatus(m.width, m.awsCtx.Profile, m.awsCtx.Region, m.account, m.activity.Snapshot(), m.spinTick)

	bodyHeight := m.height - 4
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch m.mode {
	case modeDetails:
		body = renderDetails(m.detailsResource, m.actionSel, m.width)
		body = padBlock(body, bodyHeight)
	default:
		body = m.renderSearchBody(bodyHeight)
	}

	// Optional toast overlay replaces the bottom divider + status with a
	// centered box + shorter status, keeping total height the same.
	if m.toast.isActive() {
		toastLine := renderToast(m.toast, m.width)
		return strings.Join([]string{
			inputLine,
			divider,
			body,
			divider,
			toastLine,
		}, "\n")
	}

	return strings.Join([]string{
		inputLine,
		divider,
		body,
		divider,
		status,
	}, "\n")
}

// renderSearchBody produces the middle zone for modeSearch — either the
// top-level fuzzy list or the scoped prefix list, with the right empty
// state when nothing is active.
func (m Model) renderSearchBody(height int) string {
	visible := m.visibleSearchResults()

	emptyMsg := "no results"
	inputValue := m.input.Value()
	switch {
	case inputValue == "" && m.memory.Len() == 0:
		emptyMsg = "cache is empty — fetching…"
	case inputValue == "":
		emptyMsg = "start typing to search cached resources"
	case len(visible) == 0:
		emptyMsg = fmt.Sprintf("no matches for %q", inputValue)
	}
	return renderResults(visible, m.selected, m.width, height, emptyMsg)
}

// padBlock appends blank lines to `body` until it has exactly `height`
// lines. If it already has more, it's returned unchanged.
func padBlock(body string, height int) string {
	lines := strings.Count(body, "\n") + 1
	if lines >= height {
		return body
	}
	return body + strings.Repeat("\n", height-lines)
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/view.go
git commit -m "feat(tui): dispatch view by mode and overlay toasts"
```

---

## Task 13: Update result row rendering for folders and objects

**Files:**
- Modify: `internal/tui/results.go`

- [ ] **Step 1: Replace `renderMeta` in `internal/tui/results.go`**

Locate the `renderMeta` function and replace it with EXACTLY:

```go
// renderMeta produces the right-aligned meta column for a resource.
// Phase 1 shows region for buckets and cluster name for ecs services.
// Phase 2 adds mtime for folders and size + mtime for objects. Task def
// families have no meta yet.
func renderMeta(r core.Resource) string {
	switch r.Type {
	case core.RTypeBucket:
		return styleRowDim.Render(r.Meta["region"])
	case core.RTypeEcsService:
		return styleRowDim.Render(r.Meta["cluster"])
	case core.RTypeFolder:
		if ts, ok := r.Meta["mtime"]; ok && ts != "" {
			return styleRowDim.Render(formatUnixTime(ts))
		}
		return ""
	case core.RTypeObject:
		var parts []string
		if s, ok := r.Meta["size"]; ok && s != "" {
			parts = append(parts, formatBytes(s))
		}
		if ts, ok := r.Meta["mtime"]; ok && ts != "" {
			parts = append(parts, formatUnixTime(ts))
		}
		return styleRowDim.Render(strings.Join(parts, "  "))
	default:
		return ""
	}
}

// formatBytes turns a decimal byte-count string into a human-readable
// suffix ("12.4 MB"). Empty or unparseable input returns "".
func formatBytes(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n < 0 {
		return ""
	}
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// formatUnixTime turns a decimal Unix seconds string into a short
// "YYYY-MM-DD HH:MM" timestamp in the local timezone. Empty or
// unparseable input returns "".
func formatUnixTime(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return ""
	}
	return time.Unix(n, 0).Local().Format("2006-01-02 15:04")
}
```

Note: this adds two new imports to `results.go`. Update the import block at the top of the file to include `"time"`:

```go
import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/results.go
git commit -m "feat(tui): render folder mtime + object size/mtime meta columns"
```

---

## Task 14: Toast tick — expiry check

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Add toast expiry handling on each spin tick**

In `internal/tui/update.go`, locate the `case msgSpinTick:` branch. Replace it with EXACTLY:

```go
	case msgSpinTick:
		m.spinTick++
		// Clear an expired toast so the view falls back to the normal
		// status bar. Only the spinner ticker is reliably called often
		// enough to do this without a dedicated timer.
		if !m.toast.isActive() {
			m.toast = Toast{}
		}
		return m, spinTickCmd()
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/update.go
git commit -m "feat(tui): clear expired toasts on spin tick"
```

---

## Task 15: Build the binary

**Files:** none.

- [ ] **Step 1: Produce `bin/better-aws`**

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: binary exists, no stdout, exit 0.

- [ ] **Step 2: Confirm git state is clean**

```bash
git status
```

Expected: working tree clean. The built binary is gitignored.

No commit — this task is purely a build checkpoint.

---

## Task 16: Phase 2 smoke-test checklist

**Files:** none — verification only.

- [ ] **Step 1: Cold start + top-level search (sanity)**

```bash
rm -rf ~/.cache/better-aws
./bin/better-aws
```

Verify:
- Frame opens, "cache is empty — fetching…" shows.
- Spinner animates, results populate.
- Ctrl+C quits.

- [ ] **Step 2: Drill into a bucket with Tab**

Relaunch the binary (cache is now warm). Type part of a real bucket name, observe the list. Press **Tab** on a selected bucket row.

Verify:
- Input becomes `<bucket-name>/` with the cursor at the end.
- Spinner fires (scoped search command).
- Result list updates to show folders + objects under the bucket root.
- Folders render as `[DIR ] name/`, objects as `[OBJ ] name   <size>  <mtime>`.
- Folders sort above objects.

- [ ] **Step 3: Drill deeper with Tab**

With a folder row selected, press **Tab**. Verify input becomes `<bucket>/<folder>/` and the list shows contents one level deeper.

- [ ] **Step 4: Drill back out with Backspace**

Hold/press Backspace until the trailing `/` disappears. Verify:
- As soon as the trailing `/` is removed, the scope effectively moves up (input now ends with a folder name, not `/`).
- Keep deleting past the last remaining `/`. When the `/` is gone, the list returns to the top-level fuzzy results.

- [ ] **Step 5: Prefix filtering inside a scope**

With the input at `<bucket>/<folder>/`, type a few characters. Verify:
- The list filters in place to rows whose names start with those characters.
- The leading characters of matching names are highlighted.
- Adding characters narrows the list; deleting characters widens it.

- [ ] **Step 6: Open the Details view**

Select a bucket row (top-level) and press **Enter**. Verify:
- The middle zone switches to the Details layout.
- `Name` and `ARN` rows show the bucket's values.
- Actions list shows `1. Open in Browser`, `2. Copy URI`, `3. Copy ARN` with `▸` on the first.
- `↑`/`↓` moves the `▸` indicator.
- Pressing `1`, `2`, or `3` shows a purple toast `not yet implemented — Phase 3` at the bottom for ~3 seconds.
- Pressing Enter on a selected action also shows the toast.
- `Esc` returns to the search screen with the input and scroll position preserved.

- [ ] **Step 7: Details for every resource type**

Repeat Step 6 with:
- an ECS service row (expect 3 actions: Open in Browser, Force new Deployment, Tail Logs)
- an ECS task def row (expect 3 actions: Open in Browser, Copy ARN, Tail Logs)
- an S3 folder row (inside a bucket) — expect 3 actions: Open in Browser, Copy URI, Copy ARN
- an S3 object row — expect 5 actions: Open in Browser, Copy URI, Copy ARN, Download, Preview

Every action must toast `not yet implemented — Phase 3`.

- [ ] **Step 8: Fix anything that broke**

If any step surfaced an issue, fix it inline, `git add` the fix, commit with `fix(tui): <what>`, and re-run the affected steps.

- [ ] **Step 9: Tag and report**

If everything works:

```bash
git tag phase-2-complete
git log --oneline phase-1-complete..phase-2-complete
```

Report to the user that Phase 2 is complete, summarize what Phase 3 still owes (real action implementations), and ask how to proceed.

---

## Phase 2 complete — what ships

At this point the TUI:
- Drills into S3 buckets and their folder hierarchy via Tab completion (bucket → folder → deeper folder → object).
- Lives-backed prefix filtering within a scope, with cache-first rendering and opportunistic persistence of every visited key.
- Handles backspace-driven scope pop naturally through the input buffer.
- Shows a Details view with Name + ARN for any selected resource plus a stubbed Actions list.
- Displays a transient toast whenever an action is activated (currently just the "not yet implemented" stub).
- Mode-routed key handling (`modeSearch` / `modeDetails`) so future phases can add more screens without touching the top-level `Update` switch.

**What Phase 3 picks up:** Actually executing each action — building AWS console URLs, running `UpdateService(ForceNewDeployment=true)`, wiring `StartLiveTail`, streaming downloads, opening previews via `$TMPDIR + open`. Every action already has a row in the Details view and a hotkey; Phase 3 just swaps the stub toast for real behavior.
