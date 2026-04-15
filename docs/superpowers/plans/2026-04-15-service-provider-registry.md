# better-aws-cli — Service Provider Registry Refactor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse the ~13 `switch t { case core.RTypeX: }` statements scattered across the codebase into a single `services.Provider` interface plus registry, so adding a new AWS resource type becomes "drop a new file under `internal/awsctx/<svc>/` that implements `Provider` and `init()`-registers itself; blank-import its package from `cmd/better-aws/main.go`."

**Architecture:** A new `internal/services` package owns the `Provider` interface and a process-global registry. Each existing per-type concern (tag color, alias list, console URL builder, meta column rendering, Tab drill-in semantics, lazy-detail resolution, log-group lookup, list adapter, etc.) becomes a method on `Provider`. Every existing type gets a small `provider_<name>.go` file inside its existing `internal/awsctx/<svc>/` package; each file's `init()` calls `services.Register`. `cmd/better-aws/main.go` blank-imports the awsctx subpackages so all providers register on startup. After the registry is in place, every type-switch call site is rewritten as a `services.Get(t).Method(...)` call. Lazy task-def resolution gets generalized to a typed `lazyDetailKey{Type, Key}` map so future providers can opt in without touching the message-handler switch. Action dispatch (`tui/actions.go::ActionsFor`) remains a tui-internal switch — moving it would create an import cycle because actions hold closures that mutate the bubbletea Model. That's the one remaining intentional type-switch.

**Tech Stack:** Go 1.22+, no new external dependencies. Reuses `aws-sdk-go-v2`, `charmbracelet/lipgloss` (for `Provider.TagStyle()`), and the existing `internal/awsctx`, `internal/core`, and `internal/search` packages.

**Scope boundary (what this refactor does NOT include):**

- Moving `Action`/`ActionExecute` into providers (would require pulling `Model` into a shared package or a patch-result type — out of scope).
- Adding any new AWS service. The refactor's purpose is to make adding the *next* service easy; this plan doesn't add one.
- Changing the SQLite schema, the cache lifecycle, or the search engines.
- Touching the profile/region switcher overlay or the bubbletea `Mode` enum.
- Typing the `core.Resource.Meta` map. (Worth a follow-up, but not coupled to this refactor.)

**Reference review:** the original review that motivated this is in the conversation log; the 13 pain-point inventory there maps 1:1 to the call-site rewrites in Tasks 13–22 below.

**Working directory:** `/Users/wagnermattei/www/pied-piper/better-aws-cli`. Every shell command assumes this CWD.

**Testing policy:** No automated tests at v0. Each task ends with `go build ./...` and `git commit`. The refactor is structured so the build passes at every commit — new code is added before old code is deleted, and call sites switch over one at a time.

**Branch:** All work happens on `refactor/service-provider-registry`. The branch is already created and is currently checked out.

---

## File map

### New files

| Path | Responsibility |
|---|---|
| `internal/services/provider.go` | `Provider` interface + `BaseProvider` zero-default embeddable |
| `internal/services/registry.go` | `Register`, `Get`, `Lookup` (alias→Provider), `All`, `TopLevel` accessors |
| `internal/awsctx/s3/provider_buckets.go` | `bucketProvider` implementing `services.Provider` for `RTypeBucket` |
| `internal/awsctx/s3/provider_folders.go` | `folderProvider` for `RTypeFolder` (S3 directory virtual prefix) |
| `internal/awsctx/s3/provider_objects.go` | `objectProvider` for `RTypeObject` |
| `internal/awsctx/ecs/provider_services.go` | `ecsServiceProvider` for `RTypeEcsService` |
| `internal/awsctx/ecs/provider_taskdefs.go` | `ecsTaskDefProvider` for `RTypeEcsTaskDefFamily` |

### Modified files

| Path | What changes |
|---|---|
| `cmd/better-aws/main.go` | Blank-import each awsctx subpackage so providers register on startup |
| `cmd/better-aws/preload.go` | `preloadOne` switch → `services.Get(t).ListAll(...)` |
| `internal/core/resource.go` | `Resource.ARN()` delegates to provider; `serviceAliases` map deleted (aliases now live on providers); `ResourceTypeForAlias` and `AliasesFor` delegate to registry |
| `internal/index/memory.go` | `All()`, `Len()`, and `pri()` use `services.TopLevel()` + `Provider.SortPriority()` instead of the hardcoded type set |
| `internal/tui/styles.go` | `tagStyleFor` deleted; callers use `services.Get(t).TagStyle()` |
| `internal/tui/results.go` | `renderMeta` switch → `services.Get(r.Type).RenderMeta(r)` wrapped in `styleRowDim` |
| `internal/tui/browser.go` | `consoleURL` switch → `services.Get(r.Type).ConsoleURL(r, region, lazy)` |
| `internal/tui/action_copy.go` | URI shape switch → `services.Get(r.Type).URI(r)` |
| `internal/tui/action_tail.go` | `taskDefFamilyForDetails` switch deleted; replaced by `services.Get(r.Type).LogGroup(r, lazy)` |
| `internal/tui/update.go` | `handleTab` switch → `services.Get(row.Type).TabComplete(scope, row)`; Enter handler's task-def resolution becomes generic via `services.Get(r.Type).ResolveDetails`; `m.taskDefDetails map[string]*awsecs.TaskDefDetails` becomes `m.lazyDetails map[lazyDetailKey]map[string]string`; `msgTaskDefResolved` becomes `msgLazyDetailsResolved` |
| `internal/tui/details.go` | `detailsARN` and the log-group row use `services.Get(r.Type)` instead of inspecting `r.Type` directly |
| `internal/tui/commands.go` | `refreshServiceCmd` switch → `services.Get(t).ListAll(...)` |
| `internal/tui/model.go` | Replace `taskDefDetails` field with `lazyDetails`; update `NewModel` and the switcher commit reset |
| `internal/awsctx/ecs/describe.go` | `TaskDefDetails` struct stays (used by the ecsTaskDefProvider implementation); no behavioural change |

### Files to delete

| Path | Why |
|---|---|
| (none) | Everything that gets removed is sub-section of an existing file. |

---

## Task 1: `services.Provider` interface and `BaseProvider`

**Files:**
- Create: `internal/services/provider.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
// Package services owns the per-resource-type abstraction and the
// global registry that every other layer uses to look up type-specific
// behaviour. Adding a new AWS resource type is "drop a new file that
// implements Provider, blank-import the package from main.go" — no
// edits to switch statements scattered across the codebase.
package services

import (
	"context"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Provider is the per-resource-type strategy interface. Every method
// either describes the type ("what tag color does this use?", "what
// aliases trigger a manual scope?") or operates on a single Resource
// of that type ("build a console URL for this row", "what's the next
// scope after Tab on this row?").
//
// Implementations are typically tiny structs (no state) that live
// next to the SDK adapters they wrap, e.g.
// internal/awsctx/s3/provider_buckets.go. Each implementation file
// has an init() that calls services.Register.
type Provider interface {
	// --- Identity --------------------------------------------------

	// Type returns the core.ResourceType this provider is registered
	// under. The registry uses this as the lookup key.
	Type() core.ResourceType

	// Aliases returns the set of "<alias>:" strings the user can
	// type in the input bar to scope-search this resource type.
	// May be empty for types that aren't directly addressable from
	// the top level (e.g. S3 folder/object providers, which the
	// user reaches by drilling into a bucket).
	Aliases() []string

	// TagLabel is the short label rendered inside the colored chip
	// at the start of every result row (e.g. "S3", "ECS", "TASK").
	TagLabel() string

	// TagStyle returns the lipgloss style used to render TagLabel.
	// Providers own their own colors so the styles package never
	// needs a per-type switch.
	TagStyle() lipgloss.Style

	// SortPriority controls the order this type appears in mixed
	// top-level result lists (lower = earlier). Stable across runs.
	SortPriority() int

	// IsTopLevel reports whether resources of this type should be
	// included in the unified top-level fuzzy index returned by
	// index.Memory.All(). S3 buckets, ECS services, and ECS
	// task-def families are top-level today; folders and objects
	// are not (they are reached via S3 drill-in).
	IsTopLevel() bool

	// --- Per-resource accessors -----------------------------------

	// ARN returns the canonical AWS ARN for the given resource.
	// May return "" for types without a meaningful ARN.
	ARN(r core.Resource) string

	// URI returns the user-facing URI form for the Copy URI action.
	// The bool is false when the type has no URI form (e.g. ECS
	// services don't have a URI scheme).
	URI(r core.Resource) (string, bool)

	// ConsoleURL builds an AWS web-console deep link for the
	// resource. `lazy` is the per-resource lazy-details map (see
	// ResolveDetails) so providers like the ECS task-def one can
	// substitute the resolved revision into the URL.
	ConsoleURL(r core.Resource, region string, lazy map[string]string) string

	// RenderMeta returns the right-aligned meta column for a result
	// row in PLAIN string form (no styling). The caller wraps the
	// return value in styleRowDim. Provider must not embed lipgloss
	// styles in the returned string — the caller owns the dim look.
	RenderMeta(r core.Resource) string

	// TabComplete returns the new value the input bar should take
	// when the user hits Tab on a row of this type. `scope` is the
	// current parsed input so providers can reach across scopes
	// (folder/object providers prepend the bucket name from
	// scope.Bucket). The default in BaseProvider is to return
	// row.DisplayName, which works for leaf rows.
	TabComplete(scope search.Scope, r core.Resource) string

	// --- Live data ------------------------------------------------

	// ListAll fetches every resource of this type that matches the
	// optional ListOptions filters. Used by both the manual scope
	// first-entry refresh and the `better-aws preload` subcommand.
	// Providers for derived/scoped types (S3 folders, S3 objects)
	// return (nil, nil) — they have no top-level list semantics.
	ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error)

	// --- Lazy details --------------------------------------------

	// ResolveDetails is called once when the user enters the
	// Details view for a resource of this type. The returned map
	// gets stored in the model's lazyDetails store keyed by
	// (type, resource key). Return (nil, nil) if the type has no
	// extra details to resolve. The map is opaque to the TUI; it
	// only flows back into ConsoleURL / LogGroup hooks on the
	// same provider.
	ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error)

	// LogGroup returns the CloudWatch log group name to tail when
	// the user activates Tail Logs on this resource. The provider
	// reads from the same lazy map ResolveDetails populated. Return
	// "" when no log group is configured (the action shows an
	// informational toast).
	LogGroup(r core.Resource, lazy map[string]string) string
}

// BaseProvider is an embeddable zero-default that gives providers
// sensible no-op implementations of the optional methods. Embed it
// to avoid hand-writing five empty method stubs per provider:
//
//	type bucketProvider struct{ services.BaseProvider }
//
// Then override only the methods that aren't no-ops.
type BaseProvider struct{}

func (BaseProvider) URI(core.Resource) (string, bool) { return "", false }

func (BaseProvider) TabComplete(_ search.Scope, r core.Resource) string {
	// Default to dropping the selected row's display name back into
	// the input bar — works for ECS services / task defs and any
	// other leaf type.
	return r.DisplayName
}

func (BaseProvider) ResolveDetails(context.Context, *awsctx.Context, core.Resource) (map[string]string, error) {
	return nil, nil
}

func (BaseProvider) LogGroup(core.Resource, map[string]string) string { return "" }
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean. The new package has no callers yet but it compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/services/provider.go
git commit -m "feat(services): add Provider interface and BaseProvider embeddable"
```

---

## Task 2: `services` registry

**Files:**
- Create: `internal/services/registry.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package services

import (
	"sort"
	"sync"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// registry is the process-global Provider lookup table. All access
// goes through the package-level functions below so the underlying
// map can stay private.
var (
	mu        sync.RWMutex
	byType    = map[core.ResourceType]Provider{}
	byAlias   = map[string]Provider{}
	insertion []Provider
)

// Register installs p in the registry. Called from each provider
// package's init(). Panics on a duplicate Type to surface
// double-registration mistakes loudly during development. Aliases
// must also be unique; duplicates panic for the same reason.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()

	t := p.Type()
	if existing, ok := byType[t]; ok {
		panic("services: duplicate registration for type " + t.String() + " (existing=" + existing.TagLabel() + ", incoming=" + p.TagLabel() + ")")
	}
	byType[t] = p
	insertion = append(insertion, p)

	for _, alias := range p.Aliases() {
		if existing, ok := byAlias[alias]; ok {
			panic("services: duplicate alias " + alias + " (existing=" + existing.TagLabel() + ", incoming=" + p.TagLabel() + ")")
		}
		byAlias[alias] = p
	}
}

// Get returns the Provider registered under the given resource type.
// The bool is false when no provider is registered — callers should
// treat that as a programmer error and skip rendering / behavior
// rather than crashing.
func Get(t core.ResourceType) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := byType[t]
	return p, ok
}

// MustGet is the same as Get but panics when nothing is registered.
// Use this in code paths where the caller is sure the type is in
// the registry — e.g. inside a per-type rendering function that was
// itself selected by walking registered providers.
func MustGet(t core.ResourceType) Provider {
	p, ok := Get(t)
	if !ok {
		panic("services: no provider registered for type " + t.String())
	}
	return p
}

// Lookup resolves a manual-scope alias (the part before the ":" in
// "s3:prod") to its provider. The bool is false when no provider
// claims the alias.
func Lookup(alias string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := byAlias[alias]
	return p, ok
}

// All returns every registered provider in insertion order. Used by
// help / debug / future "list services" features. Callers must not
// mutate the returned slice.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, len(insertion))
	copy(out, insertion)
	return out
}

// TopLevel returns every registered provider whose IsTopLevel() is
// true, sorted by SortPriority(). Used by index.Memory.All() to
// decide which resource types belong in the unified top-level
// fuzzy index.
func TopLevel() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	var out []Provider
	for _, p := range insertion {
		if p.IsTopLevel() {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SortPriority() < out[j].SortPriority()
	})
	return out
}

// AliasesFor returns every alias registered for a given resource
// type. Convenience wrapper used by help text and the future debug
// view; the same data lives on Provider.Aliases() but this saves a
// Get() call from outside the registry.
func AliasesFor(t core.ResourceType) []string {
	p, ok := Get(t)
	if !ok {
		return nil
	}
	return p.Aliases()
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/services/registry.go
git commit -m "feat(services): add process-global Provider registry"
```

---

## Task 3: `bucketProvider` implementation

**Files:**
- Create: `internal/awsctx/s3/provider_buckets.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&bucketProvider{}) }

// bucketProvider implements services.Provider for the
// core.RTypeBucket type. Wraps the existing ListBuckets adapter and
// owns the bucket-row presentation: blue tag, region in the meta
// column, https console URL, s3:// URI for Copy URI, and a Tab
// completion that drops a trailing "/" so the user immediately
// drills into the bucket's S3 drill-in scope.
type bucketProvider struct {
	services.BaseProvider
}

func (bucketProvider) Type() core.ResourceType { return core.RTypeBucket }
func (bucketProvider) Aliases() []string       { return []string{"s3", "buckets"} }
func (bucketProvider) TagLabel() string        { return "S3" }

func (bucketProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
}

func (bucketProvider) SortPriority() int { return 0 }
func (bucketProvider) IsTopLevel() bool  { return true }

func (bucketProvider) ARN(r core.Resource) string {
	return fmt.Sprintf("arn:aws:s3:::%s", r.Key)
}

func (bucketProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s", r.Key), true
}

func (bucketProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s",
		url.PathEscape(r.Key), region)
}

func (bucketProvider) RenderMeta(r core.Resource) string {
	return r.Meta["region"]
}

func (bucketProvider) TabComplete(_ search.Scope, r core.Resource) string {
	// Drop a trailing slash so the next recompute parses the input
	// as an S3 drill-in scope rooted at this bucket.
	return r.Key + "/"
}

func (bucketProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListBuckets(ctx, ac, opts)
}
```

- [ ] **Step 2: Build (expect package import errors)**

```bash
go build ./...
```

Expected: build will FAIL with `import cycle not allowed` if anything tries to import this from somewhere that's already imported by the services package. If the build is clean, even better.

If a cycle does show up: nothing imports the awsctx/s3 package yet from outside this package, so the cycle is hypothetical. **DO NOT commit if there's a real compile error.** Report it and stop.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/s3/provider_buckets.go
git commit -m "feat(s3): add bucketProvider implementing services.Provider"
```

---

## Task 4: `folderProvider` implementation

**Files:**
- Create: `internal/awsctx/s3/provider_folders.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&folderProvider{}) }

// folderProvider implements services.Provider for core.RTypeFolder.
// Folders are virtual S3 prefixes — they only exist inside a
// drilled-in bucket scope, so this provider is NOT top-level
// (IsTopLevel returns false) and ListAll returns (nil, nil) —
// folders are populated by ListAtPrefix in the scoped search code
// path, not here.
type folderProvider struct {
	services.BaseProvider
}

func (folderProvider) Type() core.ResourceType { return core.RTypeFolder }
func (folderProvider) Aliases() []string       { return nil }
func (folderProvider) TagLabel() string        { return "DIR" }

func (folderProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#008787", Dark: "#5FFFFF"})
}

func (folderProvider) SortPriority() int { return 100 }
func (folderProvider) IsTopLevel() bool  { return false }

func (folderProvider) ARN(r core.Resource) string {
	return fmt.Sprintf("arn:aws:s3:::%s/%s", r.Meta["bucket"], r.Key)
}

func (folderProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key), true
}

func (folderProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	bucket := r.Meta["bucket"]
	prefix := r.Key
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s&prefix=%s&showversions=false",
		url.PathEscape(bucket), region, url.QueryEscape(prefix))
}

func (folderProvider) RenderMeta(r core.Resource) string {
	if ts, ok := r.Meta["mtime"]; ok && ts != "" {
		return formatUnixTimeOrEmpty(ts)
	}
	return ""
}

func (folderProvider) TabComplete(scope search.Scope, r core.Resource) string {
	// row.Key is the full key relative to the bucket (e.g.
	// "logs/2026/01/"). Reconstruct "<bucket>/<key>" so the next
	// recompute parses the input as a deeper S3 drill-in scope.
	return scope.Bucket + "/" + r.Key
}

func (folderProvider) ListAll(context.Context, *awsctx.Context, awsctx.ListOptions) ([]core.Resource, error) {
	// Folders are populated by the scoped ListAtPrefix path inside
	// the TUI. There is no top-level "list every folder under every
	// bucket" semantics, so return (nil, nil) and let the UI never
	// call this for folders.
	return nil, nil
}

// formatUnixTimeOrEmpty mirrors the helper in internal/tui/results.go.
// Duplicated here (3 lines) so the provider doesn't need to import
// the tui package, which would create a cycle. If formatting becomes
// more elaborate we promote this to a shared helper.
func formatUnixTimeOrEmpty(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return ""
	}
	return formatUnixTimeFmt(n)
}
```

- [ ] **Step 2: Add the time-format helper at the bottom of `internal/awsctx/s3/objects.go`**

This is the missing piece used by `formatUnixTimeOrEmpty`. We can't import `internal/tui/results.go`'s helper because that would create a cycle. Append the following private helper to `internal/awsctx/s3/objects.go` (just above the closing of the file):

```go
// formatUnixTimeFmt renders a Unix-second timestamp into the same
// "YYYY-MM-DD HH:MM" shape used by the TUI's results view. Lives
// here rather than in the TUI so providers can reuse it without
// pulling in the tui package.
func formatUnixTimeFmt(n int64) string {
	return time.Unix(n, 0).Local().Format("2006-01-02 15:04")
}
```

You'll also need to add `"time"` to the import block of `objects.go` if it isn't already there. (It was added earlier when we wrote `formatUnixTime` in `results.go`, but `objects.go` itself doesn't currently import `time`.)

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/awsctx/s3/provider_folders.go internal/awsctx/s3/objects.go
git commit -m "feat(s3): add folderProvider and shared formatUnixTimeFmt helper"
```

---

## Task 5: `objectProvider` implementation

**Files:**
- Create: `internal/awsctx/s3/provider_objects.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&objectProvider{}) }

// objectProvider implements services.Provider for core.RTypeObject.
// Like folderProvider, objects are scoped-only — they're discovered
// by drilling into a bucket and then a folder, so IsTopLevel is
// false and ListAll returns (nil, nil). The Tab completion drops
// the trailing slash because objects are leaves.
type objectProvider struct {
	services.BaseProvider
}

func (objectProvider) Type() core.ResourceType { return core.RTypeObject }
func (objectProvider) Aliases() []string       { return nil }
func (objectProvider) TagLabel() string        { return "OBJ" }

func (objectProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#585858", Dark: "#A8A8A8"})
}

func (objectProvider) SortPriority() int { return 200 }
func (objectProvider) IsTopLevel() bool  { return false }

func (objectProvider) ARN(r core.Resource) string {
	return fmt.Sprintf("arn:aws:s3:::%s/%s", r.Meta["bucket"], r.Key)
}

func (objectProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key), true
}

func (objectProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	bucket := r.Meta["bucket"]
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/object/%s?region=%s&prefix=%s",
		url.PathEscape(bucket), region, url.QueryEscape(r.Key))
}

func (objectProvider) RenderMeta(r core.Resource) string {
	var parts []string
	if s, ok := r.Meta["size"]; ok && s != "" {
		parts = append(parts, formatBytesOrEmpty(s))
	}
	if ts, ok := r.Meta["mtime"]; ok && ts != "" {
		parts = append(parts, formatUnixTimeOrEmpty(ts))
	}
	return joinNonEmpty(parts, "  ")
}

func (objectProvider) TabComplete(scope search.Scope, r core.Resource) string {
	// Object keys never get a trailing slash — they're leaves.
	return scope.Bucket + "/" + r.Key
}

func (objectProvider) ListAll(context.Context, *awsctx.Context, awsctx.ListOptions) ([]core.Resource, error) {
	return nil, nil
}

// formatBytesOrEmpty mirrors the helper in internal/tui/results.go.
// Duplicated for the same reason as formatUnixTimeOrEmpty in
// provider_folders.go: providers must not import the tui package.
func formatBytesOrEmpty(s string) string {
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

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/s3/provider_objects.go
git commit -m "feat(s3): add objectProvider with size+mtime meta rendering"
```

---

## Task 6: `ecsServiceProvider` implementation

**Files:**
- Create: `internal/awsctx/ecs/provider_services.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package ecs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&ecsServiceProvider{}) }

// ecsServiceProvider implements services.Provider for ECS services.
// Owns the orange tag, the cluster-name meta column, the ECS console
// service-health URL, and the lazy DescribeServices resolution that
// the Tail Logs action depends on.
type ecsServiceProvider struct {
	services.BaseProvider
}

func (ecsServiceProvider) Type() core.ResourceType { return core.RTypeEcsService }
func (ecsServiceProvider) Aliases() []string {
	return []string{"ecs", "svc", "services"}
}
func (ecsServiceProvider) TagLabel() string { return "ECS" }

func (ecsServiceProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#AF5F00", Dark: "#FFAF5F"})
}

func (ecsServiceProvider) SortPriority() int { return 1 }
func (ecsServiceProvider) IsTopLevel() bool  { return true }

func (ecsServiceProvider) ARN(r core.Resource) string {
	return r.Key
}

func (ecsServiceProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	cluster := r.Meta["cluster"]
	svcName := lastARNSegment(r.Key)
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
		region, url.PathEscape(cluster), url.PathEscape(svcName), region)
}

func (ecsServiceProvider) RenderMeta(r core.Resource) string {
	return r.Meta["cluster"]
}

func (ecsServiceProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListServices(ctx, ac, opts)
}

// ResolveDetails resolves the latest task-def revision + log groups
// for the service, by way of its task-def family. The TUI calls this
// once on entering modeDetails; the result lands in m.lazyDetails
// keyed by (RTypeEcsService, r.Key) and is consumed by LogGroup.
func (ecsServiceProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	family := r.Meta["taskDefFamily"]
	if family == "" {
		return nil, nil
	}
	d, err := DescribeFamily(ctx, ac, family)
	if err != nil || d == nil {
		return nil, err
	}
	out := map[string]string{
		"familyArn": d.ARN,
	}
	if len(d.LogGroups) > 0 {
		out["logGroup"] = d.LogGroups[0]
	}
	return out, nil
}

func (ecsServiceProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}

// lastARNSegment extracts the trailing path segment of an ARN.
// Duplicated from internal/tui/browser.go so the provider doesn't
// need to import tui. 5 lines, not worth a shared package.
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/ecs/provider_services.go
git commit -m "feat(ecs): add ecsServiceProvider with lazy DescribeServices resolution"
```

---

## Task 7: `ecsTaskDefProvider` implementation

**Files:**
- Create: `internal/awsctx/ecs/provider_taskdefs.go`

- [ ] **Step 1: Create the file with EXACTLY this content**

```go
package ecs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&ecsTaskDefProvider{}) }

// ecsTaskDefProvider implements services.Provider for the
// core.RTypeEcsTaskDefFamily type. Owns the yellow tag and the
// lazy DescribeTaskDefinition flow that resolves a family name
// into a concrete revision ARN + container log groups.
type ecsTaskDefProvider struct {
	services.BaseProvider
}

func (ecsTaskDefProvider) Type() core.ResourceType { return core.RTypeEcsTaskDefFamily }
func (ecsTaskDefProvider) Aliases() []string {
	return []string{"td", "task", "taskdef"}
}
func (ecsTaskDefProvider) TagLabel() string { return "TASK" }

func (ecsTaskDefProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#AF8700", Dark: "#FFD75F"})
}

func (ecsTaskDefProvider) SortPriority() int { return 2 }
func (ecsTaskDefProvider) IsTopLevel() bool  { return true }

// ARN returns the resolved revision ARN if available in the lazy
// map, otherwise a wildcard family-only pseudo-ARN. The Details
// view renders "…resolving" while the lazy lookup is in flight; by
// the time Copy ARN runs, the lazy map is populated.
func (p ecsTaskDefProvider) ARN(r core.Resource) string {
	// Without lazy access, fall back to the family-only pseudo-ARN.
	// The TUI's Copy ARN path goes through services.Get + lazy map
	// in a wrapper, so this fallback rarely fires.
	return fmt.Sprintf("arn:aws:ecs:*:*:task-definition/%s", r.Key)
}

func (ecsTaskDefProvider) ConsoleURL(r core.Resource, region string, lazy map[string]string) string {
	family := r.Key
	rev := ""
	if lazy != nil {
		if arn := lazy["familyArn"]; arn != "" {
			if i := strings.LastIndexByte(arn, ':'); i > 0 {
				rev = arn[i+1:]
			}
		}
	}
	if rev != "" {
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s/%s?region=%s",
			region, url.PathEscape(family), url.PathEscape(rev), region)
	}
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s?region=%s",
		region, url.PathEscape(family), region)
}

func (ecsTaskDefProvider) RenderMeta(_ core.Resource) string { return "" }

func (ecsTaskDefProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListTaskDefFamilies(ctx, ac, opts)
}

// ResolveDetails fires DescribeTaskDefinition for the family and
// returns the resolved revision ARN + log group list. The keys
// match what ConsoleURL and LogGroup read.
func (ecsTaskDefProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := DescribeFamily(ctx, ac, r.Key)
	if err != nil || d == nil {
		return nil, err
	}
	out := map[string]string{
		"familyArn": d.ARN,
	}
	if len(d.LogGroups) > 0 {
		out["logGroup"] = d.LogGroups[0]
	}
	return out, nil
}

func (ecsTaskDefProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/ecs/provider_taskdefs.go
git commit -m "feat(ecs): add ecsTaskDefProvider with lazy revision + log-group resolution"
```

---

## Task 8: Blank-import provider packages from `main.go`

**Files:**
- Modify: `cmd/better-aws/main.go`

- [ ] **Step 1: Add blank imports to the import block**

Find the `import (...)` block at the top of `cmd/better-aws/main.go` and add three blank import lines so the provider packages' `init()` registers run on startup. The full corrected import block is:

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/debuglog"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/tui"

	// Provider registrations. Each blank import triggers the
	// init() in the package, which calls services.Register for
	// every provider that package owns.
	_ "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	_ "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
)
```

- [ ] **Step 2: Build**

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: clean. With the blank imports in place, the registry is populated on every binary launch.

- [ ] **Step 3: Verify the registry is populated**

Add a temporary one-shot to confirm; this is throwaway code we will delete in the next step. Edit `cmd/better-aws/main.go` and add this debug print at the very top of `main()`, right after the `cache clear` dispatch:

```go
	// TEMPORARY DEBUG — remove before commit
	for _, p := range services.All() {
		fmt.Fprintf(os.Stderr, "registered: %s aliases=%v\n", p.TagLabel(), p.Aliases())
	}
```

You'll also need to import `"github.com/wagnermattei/better-aws-cli/internal/services"` for this throwaway block.

Run:

```bash
go build -o bin/better-aws ./cmd/better-aws
./bin/better-aws preload --help 2>&1 | head -20
```

Expected stderr lines (one per provider, in registration order):

```
registered: S3 aliases=[s3 buckets]
registered: DIR aliases=[]
registered: OBJ aliases=[]
registered: ECS aliases=[ecs svc services]
registered: TASK aliases=[td task taskdef]
```

If you don't see all five, something failed to register — investigate before continuing.

- [ ] **Step 4: Remove the temporary debug print**

Delete the `TEMPORARY DEBUG` block and the unused `services` import from `main.go`. Re-build:

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/better-aws/main.go
git commit -m "feat(cmd): blank-import awsctx provider packages on startup"
```

---

## Task 9: Replace `tagStyleFor` with the registry

**Files:**
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/results.go`

- [ ] **Step 1: Delete the old `tagStyleFor` switch**

Find this function in `internal/tui/styles.go`:

```go
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
```

Delete it. Also delete the per-type style vars that were only used by this function:

```go
	// Tag styles per resource type. Keys are ResourceType.Tag() strings.
	styleTagS3   = tagStyle("#005FAF", "#5FD7FF")
	styleTagDir  = tagStyle("#008787", "#5FFFFF")
	styleTagObj  = tagStyle("#585858", "#A8A8A8")
	styleTagEcs  = tagStyle("#AF5F00", "#FFAF5F")
	styleTagTask = tagStyle("#AF8700", "#FFD75F")
```

And delete the `tagStyle` constructor helper since nothing calls it after the registry takes over:

```go
func tagStyle(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(ac(light, dark))
}
```

You may also be able to delete the `core` import from `styles.go` if nothing else uses it. Run `go build ./...` after the deletions to confirm.

- [ ] **Step 2: Replace the `tagStyleFor` call site in `internal/tui/results.go`**

Find this line in `renderResults`:

```go
		// 2. Tag.
		tag := tagStyleFor(r.Resource.Type).Render(padTag(r.Resource.Type.Tag()))
```

Replace with:

```go
		// 2. Tag — pulled from the per-type Provider so styles.go
		// no longer needs to know which colors belong to which
		// resource.
		tag := ""
		if p, ok := services.Get(r.Resource.Type); ok {
			tag = p.TagStyle().Render(padTag(p.TagLabel()))
		} else {
			tag = padTag(r.Resource.Type.Tag())
		}
```

Add `"github.com/wagnermattei/better-aws-cli/internal/services"` to the import block of `results.go`.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean. If `go vet` complains about the now-unused `padTag` helper or any imports, follow the error and fix.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/styles.go internal/tui/results.go
git commit -m "refactor(tui): tag chips render via Provider.TagStyle"
```

---

## Task 10: Replace `renderMeta` with the registry

**Files:**
- Modify: `internal/tui/results.go`

- [ ] **Step 1: Delete the old `renderMeta` switch**

Find the existing `renderMeta` function in `internal/tui/results.go`:

```go
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
```

Replace with EXACTLY:

```go
// renderMeta returns the dim-styled meta column for a result row,
// pulled from the per-type Provider. Providers return plain strings
// — this function owns the styleRowDim wrapping so colors stay
// centralized in the styles file.
func renderMeta(r core.Resource) string {
	p, ok := services.Get(r.Type)
	if !ok {
		return ""
	}
	plain := p.RenderMeta(r)
	if plain == "" {
		return ""
	}
	return styleRowDim.Render(plain)
}
```

- [ ] **Step 2: Delete `formatBytes` and `formatUnixTime` from results.go**

These helpers moved into the s3 provider package (Tasks 4 and 5). They're no longer called from `results.go`. Delete both functions outright. If `time` and `strings` are no longer used in `results.go`, also remove them from the import block.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/results.go
git commit -m "refactor(tui): meta column renders via Provider.RenderMeta"
```

---

## Task 11: Replace `consoleURL` with the registry

**Files:**
- Modify: `internal/tui/browser.go`
- Modify: `internal/tui/action_open.go`

- [ ] **Step 1: Delete `consoleURL` and `lastARNSegment` from `browser.go`**

Find this function:

```go
func consoleURL(r core.Resource, region string, taskDefArn string) string {
	switch r.Type {
	case core.RTypeBucket:
		// ... 30 lines ...
	}
	return ""
}
```

Delete it along with the `lastARNSegment` helper directly below it. The browser.go file should now contain only `openInBrowser` (the OS shell-out) and its imports.

If `core` is no longer imported by `browser.go` after the deletion, remove it. If `net/url` and `strings` aren't used by `openInBrowser`, remove those too.

- [ ] **Step 2: Update `execOpenInBrowser` to use the registry**

Find this in `internal/tui/action_open.go`:

```go
func execOpenInBrowser(m Model) (Model, tea.Cmd) {
	arn := ""
	if d, ok := m.taskDefDetails[m.detailsResource.Key]; ok && d != nil {
		arn = d.ARN
	}
	u := consoleURL(m.detailsResource, m.awsCtx.Region, arn)
	if u == "" {
		m.toast = newToast("no console URL for this resource", 3*time.Second)
		return m, nil
	}
	if err := openInBrowser(u); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("opened in browser", 2*time.Second)
	return m, nil
}
```

Replace with EXACTLY:

```go
func execOpenInBrowser(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no console URL for this resource", 3*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	u := p.ConsoleURL(r, m.awsCtx.Region, lazy)
	if u == "" {
		m.toast = newToast("no console URL for this resource", 3*time.Second)
		return m, nil
	}
	if err := openInBrowser(u); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("opened in browser", 2*time.Second)
	return m, nil
}
```

The `m.lazyDetailsFor(r)` accessor is added in Task 14. Until then this won't compile — that's expected. Stage the file but DO NOT commit it yet:

```bash
git add internal/tui/browser.go internal/tui/action_open.go
git status
```

- [ ] **Step 3: Stage but do not commit**

The combined commit happens at the end of Task 14, when `m.lazyDetailsFor` is wired up.

---

## Task 12: Replace the URI switch in `action_copy.go`

**Files:**
- Modify: `internal/tui/action_copy.go`

- [ ] **Step 1: Delete the URI switch**

Find this function:

```go
func execCopyURI(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	var uri string
	switch r.Type {
	case core.RTypeBucket:
		uri = fmt.Sprintf("s3://%s", r.Key)
	case core.RTypeFolder, core.RTypeObject:
		uri = fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key)
	default:
		m.toast = newToast("no URI for this resource type", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(uri); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("URI copied: "+uri, 3*time.Second)
	return m, nil
}
```

Replace with EXACTLY:

```go
func execCopyURI(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no URI for this resource type", 3*time.Second)
		return m, nil
	}
	uri, supported := p.URI(r)
	if !supported {
		m.toast = newToast("no URI for this resource type", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(uri); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("URI copied: "+uri, 3*time.Second)
	return m, nil
}
```

- [ ] **Step 2: Update `execCopyARN` to read from the lazy map via the provider**

Find this function in the same file:

```go
func execCopyARN(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	arn := r.ARN()
	if r.Type == core.RTypeEcsTaskDefFamily {
		if d, ok := m.taskDefDetails[r.Key]; ok && d != nil && d.ARN != "" {
			arn = d.ARN
		}
	}
	if arn == "" {
		m.toast = newToast("no ARN for this resource", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(arn); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("ARN copied: "+arn, 3*time.Second)
	return m, nil
}
```

Replace with EXACTLY:

```go
func execCopyARN(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	arn := arnForDetails(r, m)
	if arn == "" {
		m.toast = newToast("no ARN for this resource", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(arn); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("ARN copied: "+arn, 3*time.Second)
	return m, nil
}

// arnForDetails returns the best ARN we can show for the given
// resource: the lazy-resolved revision ARN if available, otherwise
// the provider's default ARN, otherwise the core.Resource.ARN()
// fallback. The TUI's other ARN consumers (Details panel) go through
// the same helper.
func arnForDetails(r core.Resource, m Model) string {
	if lazy := m.lazyDetailsFor(r); lazy != nil {
		if a := lazy["familyArn"]; a != "" {
			return a
		}
	}
	if p, ok := services.Get(r.Type); ok {
		if a := p.ARN(r); a != "" {
			return a
		}
	}
	return r.ARN()
}
```

If the imports for `fmt` or `core` become unused after the rewrite, remove them.

- [ ] **Step 3: Stage but do not commit**

Same reason as Task 11: `m.lazyDetailsFor` is added in Task 14. Stage and continue.

```bash
git add internal/tui/action_copy.go
```

---

## Task 13: Replace `taskDefFamilyForDetails` with `LogGroup` registry call

**Files:**
- Modify: `internal/tui/action_tail.go`

- [ ] **Step 1: Delete the old per-type switch**

Find these blocks in `internal/tui/action_tail.go`:

```go
func execTailLogs(m Model) (Model, tea.Cmd) {
	family := taskDefFamilyForDetails(m)
	if family == "" {
		m.toast = newToast("no task definition linked to this resource", 4*time.Second)
		return m, nil
	}
	d, ok := m.taskDefDetails[family]
	if !ok {
		m.toast = newToast("task definition not yet resolved", 3*time.Second)
		return m, nil
	}
	if d == nil {
		m.toast = newToast("task definition still resolving — try again", 2*time.Second)
		return m, nil
	}
	if len(d.LogGroups) == 0 {
		m.toast = newToast("no CloudWatch log group configured on this task definition", 4*time.Second)
		return m, nil
	}
	group := d.LogGroups[0]
	// ... existing modeTailLogs setup ...
}

func taskDefFamilyForDetails(m Model) string {
	r := m.detailsResource
	switch r.Type {
	case core.RTypeEcsTaskDefFamily:
		return r.Key
	case core.RTypeEcsService:
		return r.Meta["taskDefFamily"]
	}
	return ""
}
```

Replace the entire `execTailLogs` function with EXACTLY:

```go
func execTailLogs(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no log group configured on this resource", 4*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	// If lazy resolution hasn't landed yet, the user can either
	// wait for the in-flight resolveDetails command (which fires
	// on entering Details) or retry. We can't distinguish "still
	// resolving" from "no log group configured" without tracking
	// in-flight per resource, so the message is unified.
	group := p.LogGroup(r, lazy)
	if group == "" {
		// Detect "in-flight" via lazyDetailsState — see Task 14.
		if m.lazyDetailsState[lazyDetailKey{Type: r.Type, Key: r.Key}] == lazyStateInFlight {
			m.toast = newToast("details still resolving — try again", 2*time.Second)
			return m, nil
		}
		m.toast = newToast("no CloudWatch log group configured on this resource", 4*time.Second)
		return m, nil
	}

	m.mode = modeTailLogs
	m.tailGroup = group
	m.tailLines = nil
	m.tailViewport.SetContent("")
	m.tailViewport.GotoTop()
	m.inFlight = true
	m.inFlightLabel = "starting tail…"
	return m, tailLogsStartCmd(m.awsCtx, group, m.account)
}
```

Delete `taskDefFamilyForDetails` outright.

If `core` is no longer imported by this file, remove it. The `time` import stays.

- [ ] **Step 2: Stage but do not commit**

The `lazyDetailKey`, `lazyStateInFlight`, `m.lazyDetailsState`, and `m.lazyDetailsFor` symbols are all added in Task 14. Stage and proceed.

```bash
git add internal/tui/action_tail.go
```

---

## Task 14: Generalize lazy details into a typed key map (combined commit for Tasks 11–14)

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/details.go`

This is the key task. All the previous "stage but do not commit" tasks fold into the single combined commit at the end of this one.

- [ ] **Step 1: Add lazy-detail types in `internal/tui/model.go`**

At the top of `model.go`, just below the imports, insert these types and constants:

```go
// lazyDetailKey identifies a single (resource type, resource key)
// pair in the m.lazyDetails store. Used by the generic
// services.Provider.ResolveDetails flow that replaced the
// task-def-only m.taskDefDetails map.
type lazyDetailKey struct {
	Type core.ResourceType
	Key  string
}

// lazyDetailState tracks whether a given lazyDetailKey has had its
// resolve fired, completed, or never been requested.
type lazyDetailState int

const (
	lazyStateNone     lazyDetailState = iota // never requested
	lazyStateInFlight                        // resolveDetails command running
	lazyStateResolved                        // command landed, m.lazyDetails populated
)
```

Replace these existing fields on the Model struct:

```go
	taskDefDetails map[string]*awsecs.TaskDefDetails
```

with:

```go
	// lazyDetails is the generic per-resource extra-data store
	// populated by services.Provider.ResolveDetails. Keyed by
	// (resource type, resource key) so different types can't
	// collide on the same string key.
	lazyDetails      map[lazyDetailKey]map[string]string
	lazyDetailsState map[lazyDetailKey]lazyDetailState
```

If `awsecs` is no longer imported by `model.go`, remove the import.

Update `NewModel` to initialize the new fields. Find the existing struct literal and replace with EXACTLY:

```go
	return Model{
		memory:              memory,
		db:                  db,
		awsCtx:              awsCtx,
		activity:            activity,
		input:               ti,
		width:               80,
		height:              24,
		mode:                modeSearch,
		lazyDetails:         make(map[lazyDetailKey]map[string]string),
		lazyDetailsState:    make(map[lazyDetailKey]lazyDetailState),
		tailViewport:        viewport.New(80, 10),
		serviceScopeFetched: make(map[string]struct{}),
	}
```

Add the helper accessor at the bottom of `model.go`:

```go
// lazyDetailsFor returns the resolved lazy detail map for the given
// resource, or nil if nothing has been resolved (or resolution is
// still in flight). Used by per-action providers via the action
// dispatcher; see services.Provider.ConsoleURL / LogGroup signatures.
func (m Model) lazyDetailsFor(r core.Resource) map[string]string {
	if m.lazyDetails == nil {
		return nil
	}
	return m.lazyDetails[lazyDetailKey{Type: r.Type, Key: r.Key}]
}
```

- [ ] **Step 2: Replace the task-def-specific resolve in `update.go`**

Find the Enter handler block in `updateSearch`:

```go
		// Lazily resolve task-definition details (latest revision +
		// log groups) so the Details view can show them and the
		// Tail Logs action has what it needs. Both ECS task-def
		// families (family == resource key) and ECS services (family
		// from Meta["taskDefFamily"], populated by the Task-17
		// services adapter extension) trigger this.
		family := ""
		switch m.detailsResource.Type {
		case core.RTypeEcsTaskDefFamily:
			family = m.detailsResource.Key
		case core.RTypeEcsService:
			family = m.detailsResource.Meta["taskDefFamily"]
		}
		if family != "" {
			if _, ok := m.taskDefDetails[family]; !ok {
				m.taskDefDetails[family] = nil
				return m, resolveTaskDefCmd(m.awsCtx, family)
			}
		}
		return m, nil
```

Replace with EXACTLY:

```go
		// Generic lazy-detail resolution. Every provider that has a
		// non-trivial ResolveDetails participates — the message
		// handler stores the result in m.lazyDetails keyed by
		// (type, resource key).
		key := lazyDetailKey{Type: m.detailsResource.Type, Key: m.detailsResource.Key}
		if m.lazyDetailsState[key] == lazyStateNone {
			if p, ok := services.Get(m.detailsResource.Type); ok {
				m.lazyDetailsState[key] = lazyStateInFlight
				return m, resolveLazyDetailsCmd(m.awsCtx, p, m.detailsResource)
			}
		}
		return m, nil
```

Find the existing `msgTaskDefResolved` handler in `Update`:

```go
	case msgTaskDefResolved:
		if msg.err != nil {
			m.toast = newErrorToast("describe task def failed: " + msg.err.Error())
			return m, nil
		}
		if m.taskDefDetails == nil {
			m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
		}
		m.taskDefDetails[msg.family] = msg.details
		return m, nil
```

Replace with EXACTLY:

```go
	case msgLazyDetailsResolved:
		if msg.err != nil {
			m.lazyDetailsState[msg.key] = lazyStateResolved
			m.toast = newErrorToast("resolve details failed: " + msg.err.Error())
			return m, nil
		}
		if m.lazyDetails == nil {
			m.lazyDetails = make(map[lazyDetailKey]map[string]string)
		}
		m.lazyDetails[msg.key] = msg.details
		m.lazyDetailsState[msg.key] = lazyStateResolved
		return m, nil
```

If `awsecs` becomes unused in update.go, remove the import.

- [ ] **Step 3: Replace the resolveTaskDefCmd in `commands.go`**

Find this in `internal/tui/commands.go`:

```go
type msgTaskDefResolved struct {
	family  string
	details *awsecs.TaskDefDetails
	err     error
}

func resolveTaskDefCmd(ac *awsctx.Context, family string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := awsecs.DescribeFamily(ctx, ac, family)
		return msgTaskDefResolved{family: family, details: d, err: err}
	}
}
```

Replace with EXACTLY:

```go
// msgLazyDetailsResolved carries the result of a generic lazy-detail
// resolution started from the Enter handler. The handler stores the
// returned map in m.lazyDetails keyed by msg.key.
type msgLazyDetailsResolved struct {
	key     lazyDetailKey
	details map[string]string
	err     error
}

// resolveLazyDetailsCmd dispatches a provider's ResolveDetails as a
// tea.Cmd. The caller is responsible for marking
// m.lazyDetailsState[key] = lazyStateInFlight before returning this
// command from Update — the message handler flips it to
// lazyStateResolved on completion.
func resolveLazyDetailsCmd(ac *awsctx.Context, p services.Provider, r core.Resource) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		details, err := p.ResolveDetails(ctx, ac, r)
		return msgLazyDetailsResolved{
			key:     lazyDetailKey{Type: r.Type, Key: r.Key},
			details: details,
			err:     err,
		}
	}
}
```

Add `"github.com/wagnermattei/better-aws-cli/internal/services"` to the import block of `commands.go`. If `awsecs` is now only used by `refreshServiceCmd`, leave it; if it becomes fully unused remove it.

- [ ] **Step 4: Update `details.go` to use the generic lazy store**

Find this in `internal/tui/details.go`:

```go
func detailsARN(r core.Resource, m Model) string {
	if r.Type != core.RTypeEcsTaskDefFamily {
		return r.ARN()
	}
	d, ok := m.taskDefDetails[r.Key]
	if !ok {
		return r.ARN()
	}
	if d == nil {
		return "…resolving"
	}
	return d.ARN
}
```

Replace with EXACTLY:

```go
func detailsARN(r core.Resource, m Model) string {
	key := lazyDetailKey{Type: r.Type, Key: r.Key}
	state := m.lazyDetailsState[key]
	switch state {
	case lazyStateInFlight:
		return "…resolving"
	case lazyStateResolved:
		if lazy := m.lazyDetails[key]; lazy != nil {
			if a := lazy["familyArn"]; a != "" {
				return a
			}
		}
	}
	if p, ok := services.Get(r.Type); ok {
		if a := p.ARN(r); a != "" {
			return a
		}
	}
	return r.ARN()
}
```

Find the log-group row in `renderDetails`:

```go
	if family := taskDefFamilyForDetails(m); family != "" {
		if d, ok := m.taskDefDetails[family]; ok && d != nil && len(d.LogGroups) > 0 {
			writeField(&b, "Log", d.LogGroups[0])
		}
	}
```

Replace with EXACTLY:

```go
	if p, ok := services.Get(r.Type); ok {
		if group := p.LogGroup(r, m.lazyDetailsFor(r)); group != "" {
			writeField(&b, "Log", group)
		}
	}
```

Add the `services` import to `details.go` if needed. Remove `core` if it's no longer used (it's probably still used by the function signature).

- [ ] **Step 5: Update the switcher commit reset**

In `internal/tui/update.go`, find the `msgSwitcherCommitted` handler block where session state is reset. Change:

```go
		m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
```

to:

```go
		m.lazyDetails = make(map[lazyDetailKey]map[string]string)
		m.lazyDetailsState = make(map[lazyDetailKey]lazyDetailState)
```

If `awsecs` is now unused in update.go, remove its import.

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: clean. All the previous staged files (Tasks 11, 12, 13) compile against the new lazy-details symbols.

If there's a build failure naming `taskDefDetails`, `msgTaskDefResolved`, `taskDefFamilyForDetails`, or `resolveTaskDefCmd`, find the call site and update it to the new generic name. Search for these strings to confirm none remain:

```bash
grep -rn 'taskDefDetails\|msgTaskDefResolved\|taskDefFamilyForDetails\|resolveTaskDefCmd' internal/ cmd/
```

Should return zero matches (or only matches inside the file you're about to commit).

- [ ] **Step 7: Commit (combined Tasks 11+12+13+14)**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/commands.go internal/tui/details.go internal/tui/action_open.go internal/tui/action_copy.go internal/tui/action_tail.go internal/tui/browser.go
git commit -m "refactor(tui): generalize lazy details and route per-type behavior through Provider"
```

Verify the commit contents:

```bash
git show HEAD --stat
```

Expected: 8 files in the commit.

---

## Task 15: Replace `handleTab` switch with the registry

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Replace `handleTab`**

Find this function in `internal/tui/update.go`:

```go
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
		newInput = scope.Bucket + "/" + row.Key
	case core.RTypeObject:
		newInput = scope.Bucket + "/" + row.Key
	default:
		newInput = row.DisplayName
	}
	m.input.SetValue(newInput)
	m.input.CursorEnd()
	return m.recomputeResults(nil)
}
```

Replace with EXACTLY:

```go
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
	newInput := row.DisplayName
	if p, ok := services.Get(row.Type); ok {
		newInput = p.TabComplete(scope, row)
	}
	m.input.SetValue(newInput)
	m.input.CursorEnd()
	return m.recomputeResults(nil)
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/update.go
git commit -m "refactor(tui): handleTab dispatches via Provider.TabComplete"
```

---

## Task 16: Replace `refreshServiceCmd` switch

**Files:**
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Replace the switch with a registry call**

Find this function:

```go
func refreshServiceCmd(ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var (
			rs  []core.Resource
			err error
		)
		switch t {
		case core.RTypeBucket:
			rs, err = awss3.ListBuckets(ctx, ac, awsctx.ListOptions{})
		case core.RTypeEcsService:
			rs, err = awsecs.ListServices(ctx, ac, awsctx.ListOptions{})
		case core.RTypeEcsTaskDefFamily:
			rs, err = awsecs.ListTaskDefFamilies(ctx, ac, awsctx.ListOptions{})
		default:
			return msgResourcesUpdated{}
		}
		if err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		if err := index.Persist(ctx, db, mem, t, rs); err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		return msgResourcesUpdated{}
	}
}
```

Replace with EXACTLY:

```go
func refreshServiceCmd(ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		p, ok := services.Get(t)
		if !ok {
			return msgResourcesUpdated{}
		}
		rs, err := p.ListAll(ctx, ac, awsctx.ListOptions{})
		if err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		if err := index.Persist(ctx, db, mem, t, rs); err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		return msgResourcesUpdated{}
	}
}
```

If `awsecs` and `awss3` are no longer used by `commands.go`, remove their imports. (They probably still have other callers in this file; check before deleting.)

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/commands.go
git commit -m "refactor(tui): refreshServiceCmd dispatches via Provider.ListAll"
```

---

## Task 17: Replace `preloadOne` switch

**Files:**
- Modify: `cmd/better-aws/preload.go`

- [ ] **Step 1: Replace the switch with a registry call**

Find this function:

```go
func preloadOne(ctx context.Context, ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType, opts awsctx.ListOptions) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var (
		rs  []core.Resource
		err error
	)
	switch t {
	case core.RTypeBucket:
		rs, err = awss3.ListBuckets(ctx, ac, opts)
	case core.RTypeEcsService:
		rs, err = awsecs.ListServices(ctx, ac, opts)
	case core.RTypeEcsTaskDefFamily:
		rs, err = awsecs.ListTaskDefFamilies(ctx, ac, opts)
	default:
		return 0, fmt.Errorf("unsupported resource type %v", t)
	}
	if err != nil {
		return 0, err
	}
	if err := index.Persist(ctx, db, mem, t, rs); err != nil {
		return 0, err
	}
	return len(rs), nil
}
```

Replace with EXACTLY:

```go
func preloadOne(ctx context.Context, ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType, opts awsctx.ListOptions) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	p, ok := services.Get(t)
	if !ok {
		return 0, fmt.Errorf("no provider registered for resource type %v", t)
	}
	rs, err := p.ListAll(ctx, ac, opts)
	if err != nil {
		return 0, err
	}
	if err := index.Persist(ctx, db, mem, t, rs); err != nil {
		return 0, err
	}
	return len(rs), nil
}
```

Add `"github.com/wagnermattei/better-aws-cli/internal/services"` to the import block. Remove the now-unused `awss3` and `awsecs` imports.

- [ ] **Step 2: Update the alias-to-type loop**

Find the `runPreload` block that resolves the user's `target` argument to a list of types. The resolution that uses `core.ResourceTypeForAlias` still works after the refactor — `core.ResourceTypeForAlias` is rewritten in Task 18 to delegate to the registry, but the call site signature is unchanged. No edits needed here.

- [ ] **Step 3: Build**

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/better-aws/preload.go
git commit -m "refactor(cmd): preloadOne dispatches via Provider.ListAll"
```

---

## Task 18: Move alias lookup into the registry; clean up `core/resource.go`

**Files:**
- Modify: `internal/core/resource.go`

- [ ] **Step 1: Delete the local alias map and lookups**

Find this block in `internal/core/resource.go`:

```go
var serviceAliases = map[string]ResourceType{
	"s3":      RTypeBucket,
	"buckets": RTypeBucket,
	"ecs":      RTypeEcsService,
	"svc":      RTypeEcsService,
	"services": RTypeEcsService,
	"td":      RTypeEcsTaskDefFamily,
	"task":    RTypeEcsTaskDefFamily,
	"taskdef": RTypeEcsTaskDefFamily,
}

func ResourceTypeForAlias(alias string) (ResourceType, bool) {
	t, ok := serviceAliases[alias]
	return t, ok
}

func AliasesFor(t ResourceType) []string {
	var out []string
	for alias, rt := range serviceAliases {
		if rt == t {
			out = append(out, alias)
		}
	}
	return out
}
```

The alias data now lives on each provider via `Provider.Aliases()`. We can't have `core.ResourceTypeForAlias` import `services` because that creates a cycle (services → core, core → services). The right move is to **delete these helpers from `core` entirely** and update every caller (which is just `cmd/better-aws/preload.go` and `internal/search/scope.go`) to call `services.Lookup(alias)` instead.

Delete the three blocks above from `internal/core/resource.go` outright.

- [ ] **Step 2: Update `internal/search/scope.go` to call `services.Lookup`**

Find this in `internal/search/scope.go`:

```go
	if colon := strings.IndexByte(input, ':'); colon >= 0 {
		prefix := input[:colon]
		if rt, ok := core.ResourceTypeForAlias(prefix); ok {
			s.HasService = true
			s.Service = rt
			s.ServiceAlias = prefix
			s.ServiceQuery = input[colon+1:]
			return s
		}
	}
```

Replace with EXACTLY:

```go
	if colon := strings.IndexByte(input, ':'); colon >= 0 {
		prefix := input[:colon]
		if p, ok := services.Lookup(prefix); ok {
			s.HasService = true
			s.Service = p.Type()
			s.ServiceAlias = prefix
			s.ServiceQuery = input[colon+1:]
			return s
		}
	}
```

Update the import block in `internal/search/scope.go` to add `services` and (if it's no longer needed) drop `core`. The `core` package is still referenced by the `Service core.ResourceType` field on the `Scope` struct, so keep it.

```go
import (
	"strings"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)
```

- [ ] **Step 3: Update `cmd/better-aws/preload.go` to call `services.Lookup`**

Find this in `runPreload`:

```go
	} else {
		t, ok := core.ResourceTypeForAlias(target)
		if !ok {
			return fmt.Errorf("unknown service %q (try one of: s3, buckets, ecs, svc, services, td, task, taskdef, all)", target)
		}
		types = []core.ResourceType{t}
	}
```

Replace with EXACTLY:

```go
	} else {
		p, ok := services.Lookup(target)
		if !ok {
			return fmt.Errorf("unknown service %q (try one of: s3, buckets, ecs, svc, services, td, task, taskdef, all)", target)
		}
		types = []core.ResourceType{p.Type()}
	}
```

`services` is already imported in this file from Task 17.

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: clean. Search for any remaining stale references:

```bash
grep -rn 'ResourceTypeForAlias\|AliasesFor\|core\.serviceAliases' internal/ cmd/
```

Should return zero matches, or matches only inside files you've already updated.

- [ ] **Step 5: Commit**

```bash
git add internal/core/resource.go internal/search/scope.go cmd/better-aws/preload.go
git commit -m "refactor(core): aliases live on providers; delete core.serviceAliases"
```

---

## Task 19: Replace memory.go's hardcoded type set with the registry

**Files:**
- Modify: `internal/index/memory.go`

- [ ] **Step 1: Replace `All`, `Len`, and `pri`**

This is delicate because `internal/index/memory.go` cannot import `internal/services` directly — that would create a cycle (`services` already imports `core`, and `index` is at the same dependency layer as `core`). We solve this by passing the set of "top-level" types in via a setter that the TUI layer calls at startup.

Add this near the top of `internal/index/memory.go`, just below the package-level `Memory` struct definition:

```go
// topLevelTypes is the set of resource types that Memory.All() should
// return. The TUI layer wires this up at startup via SetTopLevelTypes
// (see cmd/better-aws/main.go); index can't import the services
// registry directly because services depends on internal/awsctx,
// which depends on... etc — a cycle.
var topLevelTypes = []core.ResourceType{
	core.RTypeBucket,
	core.RTypeEcsService,
	core.RTypeEcsTaskDefFamily,
}

var topLevelPriority = map[core.ResourceType]int{
	core.RTypeBucket:           0,
	core.RTypeEcsService:       1,
	core.RTypeEcsTaskDefFamily: 2,
}

// SetTopLevelTypes overrides the default list of top-level resource
// types. The new list replaces the hardcoded default. Callers also
// pass a per-type priority map for stable sort ordering. Calling with
// nil for either argument restores the hardcoded defaults.
func SetTopLevelTypes(types []core.ResourceType, priority map[core.ResourceType]int) {
	if types == nil {
		topLevelTypes = []core.ResourceType{
			core.RTypeBucket,
			core.RTypeEcsService,
			core.RTypeEcsTaskDefFamily,
		}
	} else {
		topLevelTypes = append([]core.ResourceType{}, types...)
	}
	if priority == nil {
		topLevelPriority = map[core.ResourceType]int{
			core.RTypeBucket:           0,
			core.RTypeEcsService:       1,
			core.RTypeEcsTaskDefFamily: 2,
		}
	} else {
		topLevelPriority = make(map[core.ResourceType]int, len(priority))
		for k, v := range priority {
			topLevelPriority[k] = v
		}
	}
}

// isTopLevelType reports whether the given type is in the current
// top-level set.
func isTopLevelType(t core.ResourceType) bool {
	for _, tl := range topLevelTypes {
		if tl == t {
			return true
		}
	}
	return false
}
```

Find the existing `All` method:

```go
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
	sort.Slice(out, func(i, j int) bool {
		if pri(out[i].Type) != pri(out[j].Type) {
			return pri(out[i].Type) < pri(out[j].Type)
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}
```

Replace with EXACTLY:

```go
func (m *Memory) All() []core.Resource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]core.Resource, 0, len(m.byTypeKey))
	for _, r := range m.byTypeKey {
		if isTopLevelType(r.Type) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := topLevelPriority[out[i].Type], topLevelPriority[out[j].Type]
		if pi != pj {
			return pi < pj
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}
```

Find `Len`:

```go
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, r := range m.byTypeKey {
		switch r.Type {
		case core.RTypeBucket, core.RTypeEcsService, core.RTypeEcsTaskDefFamily:
			n++
		}
	}
	return n
}
```

Replace with EXACTLY:

```go
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, r := range m.byTypeKey {
		if isTopLevelType(r.Type) {
			n++
		}
	}
	return n
}
```

Delete the standalone `pri` function entirely:

```go
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

(The `pri` lookups inside `All` now go through the `topLevelPriority` map directly.)

- [ ] **Step 2: Wire `SetTopLevelTypes` from `main.go`**

In `cmd/better-aws/main.go`, find the `runTUI` function and add this block right before opening the cache DB (after `awsctx.Resolve` and `activity.Attach`):

```go
	// Tell the index layer which types are top-level so it can
	// build the unified search snapshot. The data lives on the
	// services registry; we copy it here once so internal/index
	// doesn't need to import internal/services and create a cycle.
	{
		types := make([]core.ResourceType, 0)
		priority := make(map[core.ResourceType]int)
		for _, p := range services.TopLevel() {
			types = append(types, p.Type())
			priority[p.Type()] = p.SortPriority()
		}
		index.SetTopLevelTypes(types, priority)
	}
```

Add `"github.com/wagnermattei/better-aws-cli/internal/core"` and `"github.com/wagnermattei/better-aws-cli/internal/services"` to the import block of `main.go` (the `services` import is no longer blank — it's actively used here).

- [ ] **Step 3: Do the same in `runPreload`**

The preload subcommand also needs the registry data because it touches `index.Open`/`index.Persist` and the surrounding code might query Memory in the future. Add this in `cmd/better-aws/preload.go`'s `runPreload` right after `awsCtx, err := awsctx.Resolve(ctx)`:

```go
	{
		types := make([]core.ResourceType, 0)
		priority := make(map[core.ResourceType]int)
		for _, p := range services.TopLevel() {
			types = append(types, p.Type())
			priority[p.Type()] = p.SortPriority()
		}
		index.SetTopLevelTypes(types, priority)
	}
```

- [ ] **Step 4: Build**

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: clean. Verify the build runs:

```bash
./bin/better-aws cache clear
```

Should print the existing cache-clear message and exit 0 — proving main.go still works.

- [ ] **Step 5: Commit**

```bash
git add internal/index/memory.go cmd/better-aws/main.go cmd/better-aws/preload.go
git commit -m "refactor(index): top-level type set wired from services registry at startup"
```

---

## Task 20: Delete `Resource.Tag()` and `Resource.ARN()` from core

**Files:**
- Modify: `internal/core/resource.go`

These two methods now have provider equivalents (`Provider.TagLabel()` and `Provider.ARN()`). The standalone methods on `core.ResourceType` and `core.Resource` are dead.

But first, a check: anything still calling `core.Resource.ARN()` or `core.ResourceType.Tag()`?

- [ ] **Step 1: Find remaining callers**

```bash
grep -rn '\.ARN()\|\.Tag()' internal/ cmd/
```

If any matches show up that aren't inside the file you're about to delete from, those callers must first switch to going through the registry. Common offenders:
- `internal/tui/details.go` may still call `r.ARN()` as a fallback inside `detailsARN` — leave it; it's the fallback path.
- `internal/tui/action_copy.go`'s `arnForDetails` falls back to `r.ARN()` too — leave it.

Both of those callers are correct fallbacks that should keep working when the registry doesn't have a provider. Leave them.

- [ ] **Step 2: Decide whether to delete**

Look at the matches. If the only remaining callers are the fallbacks named above (or are inside `core/resource.go` itself), proceed with the deletion. Otherwise, fix the callers first.

If `Tag()` has zero callers outside its own file (likely — every renderer now calls `Provider.TagLabel()` via the registry), delete it:

```go
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
```

Delete the entire function. If `r.Type.Tag()` shows up anywhere as a fallback after this deletion, replace those sites with the literal "???" or, better, the provider's TagLabel.

- [ ] **Step 3: Decide on `ARN()`**

`Resource.ARN()` is still useful as a fallback (the registry might not have a provider for a given type during testing, or for forward compatibility). Leaving it in core is harmless — it's not a switch on type, it's just a value receiver method. **Skip the deletion of ARN()** to keep the fallback path simple.

If you do want to delete it, audit every caller first.

For this task: delete `Tag()` only.

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: clean. Search for stale `Tag()` calls:

```bash
grep -rn '\.Tag()' internal/ cmd/
```

Should return zero matches.

- [ ] **Step 5: Commit**

```bash
git add internal/core/resource.go
git commit -m "refactor(core): delete ResourceType.Tag (replaced by Provider.TagLabel)"
```

---

## Task 21: Final build + smoke verification

**Files:** none.

- [ ] **Step 1: Full clean build**

```bash
go build -o bin/better-aws ./cmd/better-aws
go vet ./...
```

Both must exit 0.

- [ ] **Step 2: Sanity-check the binary still runs**

```bash
./bin/better-aws cache clear
./bin/better-aws preload --help 2>&1 || true
```

The first should print the cache-clear message. The second should exit 1 with the usage banner. Neither should panic.

- [ ] **Step 3: Confirm no lingering switches**

```bash
grep -rn 'switch.*r\.Type\|switch r\.Type\|switch t {' internal/ cmd/
```

Expected matches AFTER the refactor:

- `internal/tui/actions.go` — `ActionsFor` switch (intentional; out of scope per the architecture decision)
- Anything inside provider files (`provider_*.go`) — these are intentional
- Possibly some `switch` inside SDK adapters (`s3/objects.go`, etc.) — those switch on AWS SDK types, not `core.ResourceType`

If you see any other `switch r.Type` or `switch t` on `core.ResourceType` outside `tui/actions.go`, decide whether it should also collapse into a provider call.

- [ ] **Step 4: No new commits unless something needed fixing**

If the smoke test surfaced a bug, commit a fix as `fix(refactor): <description>`. Otherwise this task is verification only.

---

## Task 22: Phase smoke-test checklist

**Files:** none — manual interactive verification.

This is the at-keyboard pass. Run the binary and walk through every code path the refactor touched.

- [ ] **Step 1: Cold start**

```bash
./bin/better-aws cache clear
./bin/better-aws
```

- Empty-cache hint should display.
- Type `s3:` → list of buckets should appear (live fetch fires).
- Type `ecs:` → list of services.
- Type `td:` → list of task def families.

- [ ] **Step 2: Tag chip colors and meta column**

While in `s3:`, `ecs:`, `td:`, verify the tag colors look right (S3 blue, ECS orange, TASK yellow) and the meta column shows region for buckets, cluster for ECS services, nothing for task defs.

- [ ] **Step 3: S3 drill-in via Tab**

From `s3:`, select a bucket, hit Tab. Input becomes `<bucket>/`. Folders and objects show with DIR/OBJ tags. Select a folder, hit Tab. Drills deeper. Select an object, hit Tab. Input gets the leaf name with no trailing slash. Hit Enter — Details view opens for the object.

- [ ] **Step 4: Details + actions per type**

For each resource type, open Details and verify:

- Bucket: 3 actions (Open, Copy URI, Copy ARN). All three work and produce toasts.
- Folder: 3 actions. All work.
- Object: 5 actions (Open, Copy URI, Copy ARN, Download, Preview). Open in Browser opens the console; Copy URI/ARN work; Download writes to `~/Downloads`; Preview opens an image/text file in the OS viewer.
- ECS service: 3 actions (Open, Force new Deployment, Tail Logs). Tail Logs opens the live tail viewport.
- Task def family: 3 actions (Open, Copy ARN, Tail Logs). ARN row briefly shows `…resolving` then resolves.

- [ ] **Step 5: Profile switcher resets state**

Press Ctrl+P, switch to a different profile, press Enter. Cache state resets, scope-fetched set resets, lazy details map resets. Type `s3:` again to verify the new profile fetches its own buckets.

- [ ] **Step 6: Preload subcommand**

```bash
./bin/better-aws preload --limit 5 s3
./bin/better-aws preload --prefix prod- ecs
./bin/better-aws preload all
```

Each should succeed and print the per-type item count.

- [ ] **Step 7: Tag the refactor**

If everything in steps 1–6 worked:

```bash
git tag refactor-service-provider-complete
git log --oneline phase-4-complete..refactor-service-provider-complete
```

Print the count of commits and a one-liner summary for the user.

- [ ] **Step 8: Fix-and-commit anything that surfaced**

Any bug surfaced in steps 1–6 gets a `fix(refactor): <thing>` commit. Re-run the failed step. Repeat until green.

---

## Refactor complete — what we got

After all 22 tasks ship:

- **One file per service.** Adding a hypothetical `RTypeLambdaFunction` becomes: create `internal/awsctx/lambda/provider_functions.go` with `funcProvider` implementing `Provider`, blank-import `internal/awsctx/lambda` from `cmd/better-aws/main.go`. Done. No edits to `tui/styles.go`, `tui/results.go`, `tui/browser.go`, `tui/commands.go`, `cmd/better-aws/preload.go`, `internal/index/memory.go`, `internal/core/resource.go`, etc.
- **Lazy details are generic.** Any provider that returns non-nil from `ResolveDetails` participates in the same fire-on-Enter, store-keyed-by-(type,key) flow as ECS task defs do today.
- **The actions switch in `tui/actions.go` remains.** Adding new actions is still "write `action_xxx.go` + add a line to `ActionsFor`". Adding a new resource type that uses *only* existing actions is a one-line addition to `ActionsFor`. Adding a resource type with new actions is the existing two-step process. Both are documented in the in-file comment.
- **No new tests** (per project convention). Smoke verification is the single quality gate, executed manually in Task 22.

The roughly 13 type-switch hot spots in the original review collapse to 1 (`ActionsFor`).
