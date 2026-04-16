# better-aws-cli

Interactive terminal TUI for navigating and managing AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services/task definitions, Lambda functions, and SSM parameters — with live prefix search into S3 bucket contents, real-time log tailing, and interactive actions (deploy, invoke, update).

## Quick start

```bash
go build -o bin/better-aws ./cmd/better-aws
./bin/better-aws                              # launch TUI
./bin/better-aws preload all                  # populate cache
./bin/better-aws preload --limit 50 s3        # selective preload
./bin/better-aws cache clear                  # wipe local cache
BETTER_AWS_DEBUG=1 ./bin/better-aws           # enable debug log
```

## Architecture overview

```
cmd/better-aws/           Binary entry point, subcommand dispatch
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
  search/                 Fuzzy matcher, prefix matcher, scope parser
  tui/                    Bubbletea TUI — model, views, actions, styles
  debuglog/               Optional slog-backed debug log (BETTER_AWS_DEBUG=1)
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
  ← cmd/better-aws (binary, subcommands)
```

**Import rules:**
- `core` imports nothing internal (only stdlib + `fmt`)
- `services` imports `core`, `search`, `awsctx` (for ListOptions), lipgloss
- Provider packages (`awsctx/*`) import `services`, `core`, `awsctx`
- `tui` imports `services`, `core`, `search`, `index`, `awsctx/*` adapters
- `search` imports `core` (and `core.LookupAlias` for scope parsing — NOT `services`, to avoid a cycle)
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

### 4. Register via blank import (1 line)

`cmd/better-aws/main.go`:
```go
_ "github.com/wagnermattei/better-aws-cli/internal/awsctx/myservice"
```

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

## File reference

### `cmd/better-aws/`

| File | Purpose |
|---|---|
| `main.go` | Subcommand dispatch, panic recovery, debuglog init, TUI launch |
| `preload.go` | `better-aws preload` subcommand with `--limit` and `--prefix` flags |

### `internal/tui/`

| File | Purpose |
|---|---|
| `model.go` | Bubbletea Model struct + `Init()` + lazy-detail types + helper accessors |
| `update.go` | `Update()` — message routing, key handling per mode, all message handlers |
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

SQLite databases at `~/.cache/better-aws/` (or `$XDG_CACHE_HOME/better-aws/`), one file per `(profile, region)` pair: `<profile>__<region>.db`.

Two tables: `resources` (top-level types) and `bucket_contents` (S3 folder/object drill-in cache). Schema version is pinned; mismatches drop+recreate.

Cache is populated by:
- Service-scope first-entry fetches (`s3:`, `ecs:`, etc.)
- `better-aws preload <service>` subcommand
- Opportunistic upsert of every S3 ListObjectsV2 result during drill-in

No launch-time refresh. No automatic expiration. `better-aws cache clear` wipes everything.

## Authentication

Standard `aws-sdk-go-v2` credential chain. Profile resolution: `AWS_PROFILE` > `AWS_DEFAULT_PROFILE` > `default`. Region: `AWS_REGION` > `AWS_DEFAULT_REGION` > profile's configured region.

`Ctrl+P` opens an in-TUI profile/region switcher that hot-swaps the AWS context without restarting.

## Debug log

`BETTER_AWS_DEBUG=1` writes structured JSON to `~/.cache/better-aws/debug.log` (truncated per run). SDK log records (via a smithy-go adapter) and app-level events are both captured.

## Panic recovery

`cmd/better-aws/main.go` wraps `runTUI()` in a `defer recover()` that writes a stack dump to `~/.cache/better-aws/crash.log` and returns a clean error to stderr.

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
