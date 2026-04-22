# scout

Interactive terminal TUI for navigating and managing AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services/task definitions, Lambda functions, and SSM parameters — with live prefix search into S3 bucket contents, real-time log tailing, and interactive actions (deploy, invoke, update).

## Quick start

```bash
go build -o bin/scout ./cmd/scout
./bin/scout                              # launch TUI
./bin/scout preload all                  # populate cache
./bin/scout preload --limit 50 s3        # selective preload
./bin/scout cache clear                  # wipe local cache
SCOUT_DEBUG=1 ./bin/scout           # enable debug log
```

## Architecture overview

```
cmd/scout/           Binary entry point, subcommand dispatch
internal/
  core/                   Root of dependency graph — Resource, ResourceType
  services/               Provider interface + process-global registry
  awsctx/                 AWS SDK wiring — Context, Activity, ListOptions
    ecs/                  ECS adapters + providers (services, task defs)
    s3/                   S3 adapters + providers (buckets, folders, objects)
    lambda/               Lambda adapters + provider (functions)
    ssm/                  SSM adapters + provider (parameters)
    logs/                 CloudWatch Logs — StartLiveTail, GetRecentEvents
  index/                  SQLite cache (per profile+region) + in-memory index
  prefs/                  Per-context user prefs (favorites + recents) — separate DB
  search/                 Fuzzy matcher, prefix matcher, scope parser
  tui/                    Bubbletea TUI — model, views, actions, styles
  debuglog/               Optional slog-backed debug log (SCOUT_DEBUG=1)
```

## Dependency graph

```
core (root — no internal imports)
  ← services (Provider interface, registry)
  ← search (scope parser, fuzzy/prefix matchers)
  ← index (SQLite DB, in-memory Memory)
  ← awsctx (AWS SDK config, activity middleware)
    ← awsctx/s3, awsctx/ecs, awsctx/lambda, awsctx/ssm, awsctx/logs
  ← tui (bubbletea Model, views, actions)
  ← cmd/scout (binary, subcommands)
```

**Import rules:**
- `core` imports nothing internal (only stdlib + `fmt`)
- `services` imports `core`, `search`, `awsctx` (for ListOptions), lipgloss
- Provider packages (`awsctx/*`) import `services`, `core`, `awsctx`
- `tui` imports `services`, `core`, `search`, `index`, `awsctx/*` adapters
- `search` imports `core` (and `core.LookupAlias` for scope parsing — NOT `services`, to avoid a cycle)
- `prefs` imports only `core` + stdlib + sqlite driver — deliberately kept free of other internal deps
- To avoid `search → services → search` cycle, alias registration flows through `core.RegisterAlias`/`core.LookupAlias`

## Adding a new AWS service

The service-provider registry makes this a contained change:

### 1. Define the ResourceType (1 line)

`internal/core/resource.go` — add to the `const` block and the `String()` switch:
```go
RTypeMyNewService
```

### 2. Add parseType case (1 line)

`internal/index/db.go` — add to `parseType`:
```go
case "my_new_service": return core.RTypeMyNewService
```

### 3. Create the adapter + provider package

```
internal/awsctx/myservice/
  list.go                  — ListMyThings(ctx, ac, opts) ([]core.Resource, error)
  describe.go              — GetMyThing(ctx, ac, name) (*Details, error)  [optional]
  provider_mythings.go     — implements services.Provider, init() registers
```

The provider file implements `services.Provider`. Embed `services.BaseProvider` for sensible defaults, then override:

| Method | Required? | What it does |
|---|---|---|
| `Type()` | yes | Return your `core.RTypeMyNewService` |
| `Aliases()` | yes | `[]string{"mysvc", "ms"}` for `mysvc:` scope |
| `TagLabel()` | yes | Short tag like `"SVC"` (max 4 chars) |
| `TagStyle()` | yes | lipgloss color for the tag chip |
| `SortPriority()` | yes | Lower = earlier in mixed result lists |
| `IsTopLevel()` | yes | `true` if it shows in unified search |
| `ARN(r, lazy)` | yes | Build the ARN string |
| `ConsoleURL(r, region, lazy)` | yes | AWS console deep link |
| `RenderMeta(r)` | yes | Right-aligned column in result rows |
| `ListAll(ctx, ac, opts)` | yes | Your list adapter call |
| `TabComplete(scope, r)` | override if drillable | Default returns `r.DisplayName` |
| `URI(r)` | override if copyable | Default returns `("", false)` |
| `ResolveDetails(ctx, ac, r)` | override for details | Return `map[string]string` for lazy store |
| `DetailRows(r, lazy)` | override for details | Return `[]services.DetailRow` |
| `LogGroup(r, lazy)` | override if tailable | Return CloudWatch log group name |
| `PollingInterval()` | override for live data | Default 0 (no polling) |
| `AlwaysRefresh()` | override for live data | Default false |

### 4. Expose a `Register()` function and wire it

In your provider package, expose an exported `Register()` that calls
`services.Register(&yourProvider{})` for every provider in the package:

```go
// internal/awsctx/myservice/register.go
func Register() { services.Register(&myServiceProvider{}) }
```

Then add a call in `internal/awsctx/providers/providers.go`:

```go
myservice.Register()
```

Registration is explicit (not `init()`-based) so commands that don't
need AWS access — like `scout cache clear` — avoid paying the cost
and avoid the dependency.

### 5. Add actions (a few lines)

`internal/tui/actions.go` — add a case to `ActionsFor`:
```go
case core.RTypeMyNewService:
    return []Action{
        {Label: "Open in Browser", Execute: execOpenInBrowser},
        {Label: "Copy ARN", Execute: execCopyARN},
    }
```

### That's it

No edits to `tui/styles.go`, `tui/results.go`, `tui/browser.go`, `tui/commands.go`, `index/memory.go`, or any other file. The registry handles tag rendering, meta columns, console URLs, Tab completion, preload subcommand support, and scoped search (`mysvc:query`) automatically.

## Key abstractions

### `services.Provider` interface

The central abstraction. One implementation per resource type. Lives alongside the SDK adapters it wraps. Registered via `init()` + `services.Register()`.

### `core.Resource`

Unified record for anything browsable. `Type` is the discriminator. `Key` uniquely identifies within `(profile, region, type)`. `DisplayName` is what the TUI renders. `Meta` is a `map[string]string` bag for type-specific fields (region, cluster, runtime, etc.).

### Lazy details (`lazyDetails` + `lazyDetailsState`)

When the user opens a Details view, the Enter handler fires `Provider.ResolveDetails()` as a `tea.Cmd`. The result lands in `m.lazyDetails[lazyDetailKey{Type, Key}]` as a `map[string]string`. Providers that return `AlwaysRefresh() = true` re-fire on every entry. Providers with `PollingInterval() > 0` auto-refresh on a timer while the Details view is open.

### Service scopes

Typing `<alias>:` in the search bar restricts results to one resource type. The scope parser (`search.ParseScope`) detects the colon, looks up the alias via `core.LookupAlias`, and the TUI dispatches to `memory.ByType()` for fuzzy matching. First entry per alias per session fires a live `Provider.ListAll()` fetch.

### Tail logs

`modeTailLogs` takes over the frame with a `bubbles/viewport`. Historical events are pre-fetched via `FilterLogEvents` (last 50 events, 30min window), followed by a `────── live ─▶` divider, then `StartLiveTail` streams events in real time. Users can scroll up (auto-follow pauses), press `Ctrl+↓` to resume, and press `/` to filter by substring.

### Editor integration

Interactive actions (Lambda Run, SSM Update Value) suspend the TUI via `tea.ExecProcess`, open `$EDITOR` on a temp file, and resume on editor close. The handler checks file mtime to detect "quit without saving" and validates JSON for Lambda payloads.

### Favorites and Recents (per-context user preferences)

`internal/prefs/` owns a second SQLite file per `(profile, region)` pair — `<profile>__<region>__prefs.db` — storing the user's favorites (pinned with `f`) and the last-10 resources whose Details view they entered. The TUI holds a `*prefs.DB` and a `*prefs.State` (in-memory snapshot) on the `Model` and consults the state from:

- the home page (`tui/home.go`) rendered on empty input,
- the search ranker (`tui/ranking.go`) which sorts favorites ahead of non-favorites,
- the row renderer (`tui/results.go`) which prepends `★ ` to favorited rows,
- the hint line (`tui/hint.go`) that advertises the `f` shortcut above the status bar.

`scout cache clear` does NOT touch prefs files. Context-switch (Ctrl+P) closes the old prefs DB and opens the new one.

### Zoned Details UI

`tui/details.go` renders the Details view as a dashboard of lipgloss bordered zones: **Identity** top-left (Name, color-coded type chip via `Provider.TagStyle()`, ARN), **Status** top-center (prominent state rows), **Metadata** top-right (the per-provider key/value bag), **Events** bottom-right (variable-length event stream), **Actions** bottom-left (numbered action list). Below 75 columns the zones stack vertically in the same order.

Providers opt rows into zones via `services.DetailRow.Zone` (`ZoneStatus`, `ZoneEvents`, default `ZoneMetadata`). Any DetailRow can be marked `Clickable: true` to render underlined dim-blue and become copy-on-click. `Name` and `ARN` in Identity are always clickable. Click dispatch happens in the top-level `Update` handler via `tea.MouseMsg`; `renderDetails` publishes a `[]clickRegion` into a pointer-valued slice on the Model (`m.detailsHitMap`) on every frame.

Keyboard behaviour is unchanged — mouse clicks only copy; every existing shortcut (number hotkeys, Enter, `f`, Esc, Ctrl+P) still routes through the keyboard path.

## File reference

### `cmd/scout/`

| File | Purpose |
|---|---|
| `main.go` | Subcommand dispatch, panic recovery, debuglog init, TUI launch |
| `preload.go` | `scout preload` subcommand with `--limit` and `--prefix` flags |

### `internal/tui/`

| File | Purpose |
|---|---|
| `model.go` | Bubbletea Model struct + `Init()` + lazy-detail types + helper accessors |
| `update.go` | `Update()` dispatcher — routes to per-mode key handlers and per-message handlers; also holds mouse routing and `spinTickCmd` |
| `update_search.go` | `updateSearch` + `recomputeResults` + scope helpers (readScopedCache, clampSelected, deleteLastPathSegment, schedulePollIfNeeded, computeResults, visibleSearchResults) |
| `update_details.go` | `updateDetails` + `runAction` + `toggleFavoriteForResource` |
| `update_tail.go` | `updateTail` + viewport helpers (rebuildTailViewport, formatTailLine) |
| `update_switcher.go` | `updateSwitcher` — profile/region overlay key handling |
| `update_messages.go` | Async tea.Msg handlers — handleResourcesUpdated, handleActionDone, handleEditorClosed, handleLazyDetailsResolved, handleAutomationStarted, handleTailStarted/Event, handleSwitcherCommitted, handleSpinTick, summarizeErrors |
| `view.go` | `View()` — frame composition, mode dispatch, toast overlay |
| `details.go` | Details panel renderer — Name/ARN + provider DetailRows + Actions list |
| `results.go` | Search result list renderer — tag chips, highlights, meta columns |
| `status.go` | Status bar — profile, region, account, activity spinner |
| `tail.go` | Tail Logs view — header, viewport, filter prompt/badge, help footer |
| `styles.go` | Shared lipgloss styles (status bar, toast variants, divider, etc.) |
| `toast.go` | Toast types (Info/Error/Success), constructors, `renderToast` |
| `config.go` | `MaxDisplayedResults = 20` |
| `mode.go` | Mode enum (search, details, tailLogs, switcher) |
| `switcher.go` | Profile/region overlay — state, filter, render |
| `editor.go` | `$EDITOR` integration — `openEditorCmd`, `editorAction` enum |
| `actions.go` | `Action` struct, `ActionsFor()` switch, `msgActionDone` |
| `action_open.go` | Open in Browser — delegates to `Provider.ConsoleURL` |
| `action_copy.go` | Copy URI / Copy ARN — delegates to `Provider.URI` / `Provider.ARN` |
| `action_force_deploy.go` | ECS Force Deploy with y/n confirmation gate |
| `action_download.go` | S3 object download to `~/Downloads` |
| `action_preview.go` | S3 object preview via temp file + OS viewer |
| `action_tail.go` | Tail Logs — resolves log group, enters `modeTailLogs` |
| `action_lambda_run.go` | Lambda Invoke via `$EDITOR` JSON payload |
| `action_ssm.go` | SSM Copy Value + Update Value (via `$EDITOR`) |
| `commands.go` | Async `tea.Cmd` factories — scoped search, service refresh, lazy resolve, tail pump |
| `browser.go` | `openInBrowser()` — OS shell-out (`open` / `xdg-open`) |
| `clipboard.go` | `copyToClipboard()` — atotto/clipboard wrapper |
| `downloads.go` | XDG-aware downloads directory resolver |
| `preview.go` | Temp file creation + format allowlist for Preview |
| `width.go` | `lipglossWidth()` shim |

### `internal/awsctx/`

Each service has its own subpackage with adapters + a `provider_*.go` file:

| Package | Provider | Aliases | Key adapters |
|---|---|---|---|
| `s3/` | bucketProvider, folderProvider, objectProvider | `s3:`, `buckets:` | ListBuckets, ListAtPrefix, StreamObject, HeadObject, DescribeBucket |
| `ecs/` | ecsServiceProvider, ecsTaskDefProvider | `ecs:`, `svc:`, `td:`, `task:` | ListServices, ListTaskDefFamilies, DescribeService, DescribeFamily, ForceDeployment, CountRunningTasks |
| `lambda/` | lambdaFunctionProvider | `lambda:`, `fn:` | ListFunctions, GetFunction, InvokeFunction |
| `ssm/` | ssmParameterProvider | `ssm:`, `param:` | ListParameters, GetParameter, PutParameter |
| `secretsmanager/` | secretProvider | `secrets:`, `secret:`, `sm:`, `sec:` | ListSecrets, GetSecretValue, PutSecretValue |
| `automation/` | documentProvider | `auto:`, `automation:`, `runbook:` | ListDocuments, DescribeDocument, ListExecutions, StartExecution |
| `logs/` | (no provider — shared utility) | — | StartLiveTail, GetRecentEvents |

### `internal/index/`

| File | Purpose |
|---|---|
| `db.go` | SQLite cache — `Open`, `LoadAll`, `UpsertResources`, `DeleteMissing`, schema |
| `memory.go` | In-memory index — `All`, `ByType`, `Len`, `Upsert`, `DeleteMissing`, `SetTopLevelTypes` |
| `persist.go` | `Persist()` — diff-patch upsert to both memory + SQLite |
| `bucket_contents.go` | `UpsertBucketContents`, `QueryBucketContents` for S3 drill-in cache |

### `internal/search/`

| File | Purpose |
|---|---|
| `scope.go` | `ParseScope()` — splits input into service scope / S3 drill-in / top-level |
| `fuzzy.go` | `Fuzzy()` — wraps `sahilm/fuzzy` with `Result{Resource, MatchedRunes, Score}` |
| `prefix.go` | `Prefix()` — case-sensitive prefix matcher for S3 scoped mode |

## Cache

SQLite databases at `~/.cache/scout/` (or `$XDG_CACHE_HOME/scout/`), one file per `(profile, region)` pair: `<profile>__<region>.db`.

Two tables: `resources` (top-level types) and `bucket_contents` (S3 folder/object drill-in cache). Schema version is pinned; mismatches drop+recreate.

Cache is populated by:
- Service-scope first-entry fetches (`s3:`, `ecs:`, etc.)
- `scout preload <service>` subcommand
- Opportunistic upsert of every S3 ListObjectsV2 result during drill-in

No launch-time refresh. No automatic expiration. `scout cache clear` wipes everything.

## Authentication

Standard `aws-sdk-go-v2` credential chain. Profile resolution: `AWS_PROFILE` > `AWS_DEFAULT_PROFILE` > `default`. Region: `AWS_REGION` > `AWS_DEFAULT_REGION` > profile's configured region.

`Ctrl+P` opens an in-TUI profile/region switcher that hot-swaps the AWS context without restarting.

## Debug log

`SCOUT_DEBUG=1` writes structured JSON to `~/.cache/scout/debug.log` (truncated per run). SDK log records (via a smithy-go adapter) and app-level events are both captured.

## Panic recovery

`cmd/scout/main.go` wraps `runTUI()` in a `defer recover()` that writes a stack dump to `~/.cache/scout/crash.log` and returns a clean error to stderr.

## Testing policy

v0 has no automated tests. Quality gate is manual smoke testing. Every commit builds clean via `go build ./...` and `go vet ./...`.

## Code style

- No tests yet — v0 POC policy
- No auto-formatting enforced (standard `gofmt` applies)
- Providers own their own lipgloss styles (no cross-package style imports)
- Actions live in `internal/tui/action_*.go` — one file per action group
- The `ActionsFor()` switch in `tui/actions.go` is the ONE intentional type-switch; all others go through the `services.Provider` registry
- `core.Resource.Meta` is a `map[string]string` bag — type-specific field names are documented per provider
- Lazy details use `map[string]string` with JSON-encoded slices/maps for multi-value fields
- Toast levels: Info (purple), Error (red), Success (green)
- Destructive actions use a generic `pendingConfirmType` enum for y/n confirmation

## Known limitations

| Area | Limitation | Future fix |
|---|---|---|
| Windows | `open`/`xdg-open` not supported; clipboard needs `xclip`/`xsel`/`wl-clipboard` on Linux | Add `cmd /c start` branch |
| Download dir | XDG path with `$HOME/Downloads` fallback | Config file override |
| Preview formats | jpg, jpeg, png, txt, csv only | Pluggable format registry |
| Temp files | Not cleaned up by the program | OS temp lifecycle handles it |
| ECS services | DescribeServices runs on every ListServices call | Lazy describe on Details entry only |
| Cache | No TTL, no size limit, no auto-expiration | Configurable retention |
| Tests | None | v1 priority |
