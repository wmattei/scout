# scout

Interactive terminal TUI for navigating and managing AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services/task definitions, Lambda functions, SSM parameters, Secrets Manager, and SSM Automation runbooks — with live prefix search into S3 bucket contents, real-time log tailing, and interactive actions (deploy, invoke, update).

## Quick start

```bash
go build -o bin/scout ./cmd/scout
./bin/scout                              # launch TUI
./bin/scout cache clear                  # wipe local cache (prefs preserved)
SCOUT_DEBUG=1 ./bin/scout                # enable debug log
```

## Architecture overview

```
cmd/scout/                Binary entry point, subcommand dispatch, panic recovery
internal/
  core/                   Row — the single shared record type
  effect/                 Effect union + pure Reducer + Host interface
  widget/                 Block interface + KeyValue/StatusPill/EventList/Raw
  cache/                  SQLite shared cache (Row-keyed, orphan purge)
  module/                 Module interface, Manifest, Registry, Context
  modules/                One subpackage per feature module:
    s3/                     buckets, folders, objects (single module, drill-in state)
    lambda/                 functions
    ssm/                    parameters
    secrets/                Secrets Manager (masked-by-default Reveal toggle)
    automation/             SSM Automation documents + virtual-row executions
    ecs/                    services + task-def families (two Modules, one package)
  awsctx/                 AWS SDK wiring — Context, Activity, ListOptions
    ecs/ s3/ lambda/ ssm/ secretsmanager/ automation/    adapters (list/describe/put/...)
    logs/                   CloudWatch Logs — StartLiveTail, GetRecentEvents
  prefs/                  Per-context user prefs (favorites + recents) — separate SQLite file
  search/                 Fuzzy matcher over []core.Row
  tui/                    bubbletea Model + modelHost (implements effect.Host)
  debuglog/               Optional slog-backed debug log (SCOUT_DEBUG=1)
  format/ version/        Small utility packages
```

## Dependency graph

```
core (root — no internal imports)
  ← effect          (Effect types + pure reducer + Host interface)
  ← widget          (Block + KeyValue/StatusPill/EventList/Raw; imports effect for Level)
  ← cache           (SQLite Row store + orphan purge)
  ← awsctx          (SDK config, activity middleware)
    ← awsctx/s3, awsctx/ecs, awsctx/lambda, awsctx/ssm, awsctx/secretsmanager, awsctx/automation, awsctx/logs
  ← module          (Manifest + Module interface + Registry; imports effect, widget, awsctx, cache)
  ← modules/*       (each imports module, effect, widget, core, awsctx/<its-service>)
  ← prefs           (core + sqlite driver)
  ← search          (core + sahilm/fuzzy)
  ← tui             (everything above — this is where bubbletea lives)
  ← cmd/scout       (binary: wires registry, opens cache+prefs, launches tui)
```

**Import rules:**
- `core` imports nothing internal (only stdlib).
- `effect` imports `core`. The reducer is pure — side effects (clipboard, browser, tea.ExecProcess) happen in the `Host` implementation, which lives in `tui`.
- `widget` imports `effect` (for `Level` on `StatusPill`) and lipgloss.
- `module` imports `core`, `effect`, `widget`, `awsctx`, `cache`.
- `modules/*` import `module`, `effect`, `widget`, `core`, `awsctx`, `awsctx/<their service>`. Feature modules never import `tui`.
- `tui` is the only package that imports bubbletea. It implements `effect.Host`.

## Adding a new module

A scout feature is a Go package under `internal/modules/<name>/` that implements the `module.Module` interface. Adding one requires no edits outside the module's own directory except a single-line `Register(...)` call.

### 1. Create the package

```
internal/modules/myservice/
  myservice.go   — Manifest + the struct that implements Module
  search.go      — HandleSearch + list-adapter Async
  details.go     — ResolveDetails + BuildDetails (zone builders)
  actions.go     — Actions(r) → []module.Action
```

### 2. Implement `module.Module`

The interface, in `internal/module/module.go`:

| Method                        | Purpose                                                              |
|-------------------------------|----------------------------------------------------------------------|
| `Manifest()`                  | Static description — ID, Name, Aliases, Tag, TagStyle, SortPriority. |
| `HandleSearch(ctx, q, state)` | Per-keystroke. Returns rows + newState + effects (Async for live fetch). |
| `ARN(r)`                      | ARN for Identity zone + generic Copy ARN action.                     |
| `ConsoleURL(r, region)`       | Open-in-browser deep link.                                           |
| `ResolveDetails(ctx, r)`      | Effect fired on Enter. Typically Async{Describe…} → SetLazy.         |
| `BuildDetails(ctx, r, lazy)`  | Fills the 5-zone DetailZones (Status/Metadata/Value/Events).         |
| `Actions(r)`                  | Returns `[]module.Action`. Each `Run(ctx, r) effect.Effect`.         |
| `HandleEvent(ctx, r, id)`     | Fired on Enter in a selectable Events-zone row.                      |
| `PollingInterval()`           | > 0 re-fires `ResolveDetails` on a timer.                            |
| `AlwaysRefresh()`             | true = drop lazy and re-resolve on every Details entry.              |

### 3. Register it

In `internal/modules/modules.go`:

```go
func RegisterAll(r *module.Registry) {
    r.Register(s3.New())
    r.Register(lambda.New())
    ...
    r.Register(myservice.New())
}
```

That's it. The registry handles tag rendering, fuzzy-search inclusion, scope-prefix routing (`myservice:`), cache orphan purge, and details/action dispatch automatically.

## Key abstractions

### `core.Row`

Every cached record. Shape: `{PackageID, Key, Name, Meta map[string]string}`. Fuzzy search matches on `Name`. `PackageID` is the module's manifest ID; `(PackageID, Key)` is the cache primary key. `Meta` is opaque to core — modules own their meta keys.

### `module.Module` + `module.Manifest`

The Module interface is the only place the TUI looks for feature behaviour. The Registry (`internal/module/registry.go`) keeps a process-global map from manifest ID → Module plus an alias → ID lookup table driven by `Manifest.Aliases`.

### Effect system

`effect.Effect` is a sealed union implemented by `Copy`, `Browser`, `Toast`, `Editor`, `Confirm`, `TailLogs`, `Async`, `Batch`, `SetState`, `UpsertCache`, `SetLazy`, `OpenVirtualDetails`, `Tick`, `None`. Modules return Effects from `HandleSearch`, `Action.Run`, `HandleEvent`, and `ResolveDetails`. The pure `effect.Reduce` function dispatches to `effect.Host`, which `tui/effects.go` implements against `*Model`. Side effects (clipboard writes, OS shell-outs, `tea.ExecProcess`) live inside the tea.Cmd closures the reducer queues — keeping `effect/` bubbletea-free.

### `cache` — shared SQLite store

`internal/cache/cache.go` holds a single `rows` table keyed by `(package_id, row_key)`. Modules call `Upsert`, `RowsByPackage`, and `Query(packageID, prefix)`. S3 drill-in uses `Query` with prefix `"o:<bucket>/<path>"`. `PurgeOrphans(liveIDs)` runs at startup once the registry is populated — rows belonging to retired modules get dropped.

Cache DB path: `~/.cache/scout/<profile>__<region>.db` (or `$XDG_CACHE_HOME/scout/...`). Schema-version mismatch drops + recreates — the cache is rebuildable.

### `widget` — zone content blocks

`Block` is the widget interface (`Render(w, h) string`, `ClickableRegions() []ClickRegion`). Concrete widgets: `KeyValue`, `StatusPill`, `EventList`, `Raw`, and the zero `Empty`. Modules compose these into `module.DetailZones{Status, Metadata, Value, Events}`. Core wraps each non-empty zone in a titled bordered panel (see `tui/details_module.go`).

### Lazy details

When the user enters Details on a row, the Enter handler calls `module.ResolveDetails(ctx, r)` → typically an Async effect wrapping a Describe call → returns `SetLazy{PackageID, Key, Lazy}`. The lazy map lands on `m.lazyDetails[moduleDetailKey(PackageID, Key)]` and `BuildDetails` reads it on every frame. `AlwaysRefresh()=true` modules drop their lazy entry on every entry so the "resolving…" placeholder shows until the re-fetch lands. `PollingInterval() > 0` → core re-fires `ResolveDetails` on a timer (Automation executions use this via the `Tick` effect).

### Service scopes

Typing `<alias>:` (or any `Manifest.Aliases` entry) dispatches per-keystroke to `module.HandleSearch(ctx, rest, state)`. First-entry Async in the returned effects populates the shared cache; subsequent keystrokes paint instantly from cache and re-fire the Async only if the module wants to.

### Virtual rows

Some modules (Automation) expose synthetic rows that don't come from `HandleSearch`. The `effect.OpenVirtualDetails{PackageID, Key, Name}` effect (usually returned from `HandleEvent` on a selectable Events-zone row) sets `m.virtualRow = &core.Row{...}` and jumps into Details. `BuildDetails` branches on a Key prefix (e.g. `"exec:"`) to render an execution-detail page instead of the document page.

### Tail logs

`effect.TailLogs{LogGroup}` enters `modeTailLogs`. The actual stream is `awslogs.StartLiveTail` — historical events are pre-fetched (last 50 in 30 min), then live events stream in. Auto-follow pauses on scroll-up; `Ctrl+↓` resumes; `/` filters by substring.

### Editor integration

`effect.Editor{Prefill, OnSave}` writes `Prefill` to a temp file, suspends the TUI via `tea.ExecProcess`, runs `$EDITOR`. On exit, file content is read and passed to `OnSave`, whose returned `Effect` is reduced via `msgEffectDone` — the caller can chain `Async{PutParameter}` or similar.

### Favorites and Recents

`internal/prefs/` holds a second SQLite file per context: `<profile>__<region>__prefs.db`. Tables are `favorites` and `recents`, both keyed on `(package_id, row_key)` with a `display` snapshot. Pressing `f` on a row in search mode or Details toggles favorite; entering Details auto-marks visited. The home page (empty input + at least one favorite/recent) renders favorites and recents as `search.Result{Row: core.Row{...}}` rebuilt from the prefs state. `scout cache clear` preserves `*__prefs.db` files.

### Switcher (Ctrl+P)

Opens an overlay to pick profile + region. Commit resets `moduleState`, `lazyDetails`, closes the old cache handle, reopens for the new context, and re-runs orphan purge. Legacy `index.DB` handles are gone; cache + prefs are the only SQLite files scout manages.

## File reference

### `cmd/scout/`

| File       | Purpose                                                                       |
|------------|-------------------------------------------------------------------------------|
| `main.go`  | Cobra entry point.                                                            |
| `root.go`  | `rootCmd` builder, panic recovery, `runTUI` (opens cache + prefs, launches). |
| `cache.go` | `scout cache clear` — wipes `*.db` except `*__prefs.db`.                     |

### `internal/tui/`

| File                   | Purpose                                                                            |
|------------------------|------------------------------------------------------------------------------------|
| `model.go`             | Model struct + `NewModel` + `Init`; holds moduleState, lazyDetails, virtualRow.    |
| `update.go`            | `Update` dispatcher — key routing + per-message handlers.                          |
| `update_search.go`     | `updateSearch`, `recomputeResults`, `enterModuleDetails`, `dispatchModuleScope`.   |
| `update_details.go`    | `updateDetails` + `handleModuleDetailsKey` (navigation + action dispatch).         |
| `update_tail.go`       | Tail-logs key handling + viewport helpers.                                         |
| `update_switcher.go`   | Profile/region overlay.                                                            |
| `update_messages.go`   | Async message handlers — tail, switcher, spin tick.                                |
| `effects.go`           | `modelHost` (implements `effect.Host`), `ApplyEffect`, editor + tail tea.Cmds.     |
| `modules.go`           | `moduleForAlias`, `moduleForID`, `scopeFromInput` — the registry facade.           |
| `cache_bridge.go`      | `Model.reopenModuleCache` — called on switcher commit.                             |
| `details.go`           | `renderDetails` dispatcher + zone-block helpers (`renderZoneBlock`, overlay).      |
| `details_module.go`    | `renderModuleDetails` + zone renderers (Identity, Status, Metadata, Value, Events, Actions). |
| `results.go`           | Search result list renderer.                                                       |
| `home.go`              | Favorites + Recents home page (empty-input view).                                  |
| `ranking.go`           | `partitionByFavorites` — stable-sort favs ahead of non-favs.                       |
| `hint.go`              | Favorite-toggle hint line above the status bar.                                    |
| `status.go`            | Status bar.                                                                        |
| `tail.go`              | Tail-logs view renderer + filter prompt.                                           |
| `toast.go`             | Toast types (Info/Success/Error) + `renderToast`.                                  |
| `styles.go`            | Shared lipgloss styles.                                                            |
| `switcher.go`          | Profile/region overlay renderer + state.                                           |
| `onboarding.go`        | Initial AWS-setup screen when `awsctx.Resolve` fails.                              |
| `view.go`              | `View` — frame composition.                                                        |
| `mode.go`              | Mode enum (search, details, tailLogs, switcher, onboarding).                       |
| `commands.go`          | `resolveAccountCmd`, `tailLogsStartCmd`, `tailLogsNextCmd`, `msgSwitcherCommitted`.|
| `browser.go`           | OS shell-out.                                                                      |
| `clipboard.go`         | atotto/clipboard wrapper.                                                          |
| `downloads.go`         | XDG downloads directory resolver.                                                  |
| `preview.go`           | Temp file creation + format allowlist for S3 Preview.                              |
| `config.go`            | `MaxDisplayedResults`.                                                             |
| `width.go`             | `lipglossWidth` shim.                                                              |

### `internal/module/`

| File          | Purpose                                                                      |
|---------------|------------------------------------------------------------------------------|
| `module.go`   | `Module` interface + `Manifest` + `DetailZones` + `Action`.                  |
| `registry.go` | Process-global Registry (alias lookup, ID enumeration, deterministic order). |
| `context.go`  | `Context{AWSCtx, Cache, State}` threaded into every module entry point.      |

### `internal/modules/`

| Package         | Aliases                                                  | Notes                                          |
|-----------------|----------------------------------------------------------|------------------------------------------------|
| `s3/`           | `s3`, `buckets`                                          | Single module; drill-in state encoded in query.|
| `lambda/`       | `lambda`, `fn`, `functions`                              | Run via $EDITOR JSON payload.                  |
| `ssm/`          | `ssm`, `param`, `params`, `parameter`                    | AlwaysRefresh=true.                            |
| `secrets/`      | `secrets`, `secret`, `sm`, `sec`                         | Masked by default; Reveal toggles state.       |
| `automation/`   | `auto`, `automation`, `runbook`                          | Executions are virtual rows (Key prefix `exec:`). |
| `ecs/`          | Services: `ecs`, `svc`, `services`; Task defs: `td`, `task`, `taskdef` | Two Modules in one package; services polls 10s. |

### `internal/effect/`

| File          | Purpose                                                                         |
|---------------|---------------------------------------------------------------------------------|
| `effect.go`   | Effect union types (Copy, Browser, Toast, Editor, Confirm, TailLogs, Async, Batch, SetState, UpsertCache, SetLazy, OpenVirtualDetails, Tick, None). |
| `reducer.go`  | `Host` interface + pure `Reduce` dispatcher + `Row` + `AsyncRunner` + `EditorOpener`. |
| `levels.go`   | `Level` enum (Info/Success/Warning/Error).                                      |

### `internal/widget/`

| File            | Purpose                                                    |
|-----------------|------------------------------------------------------------|
| `widget.go`     | `Block` interface + `ClickRegion` + `Empty`.               |
| `keyvalue.go`   | `KeyValue` + `KVRow` with optional `Clickable`/`ClipValue`.|
| `pill.go`       | `StatusPill` with Level-driven colour.                     |
| `eventlist.go`  | `EventList` + `EventRow` with `ActivationID`.              |
| `raw.go`        | `Raw` — pre-rendered escape-hatch content.                 |
| `style.go`      | Shared lipgloss styles.                                    |

### `internal/cache/`

| File         | Purpose                                                             |
|--------------|---------------------------------------------------------------------|
| `cache.go`   | `DB` + `Open` + `Upsert` + `AllRows` + `RowsByPackage` + `Query` + `PurgeOrphans`. |
| `schema.go`  | Schema DDL + version constant.                                      |
| `path.go`    | `DBPath(profile, region)` + XDG-aware base directory.               |

### `internal/awsctx/`

One subpackage per AWS service — plain adapters, no module code.

| Package            | Key adapters                                                                                   |
|--------------------|-----------------------------------------------------------------------------------------------|
| `s3/`              | `ListBuckets`, `ListAtPrefix`, `StreamObject`, `HeadObject`, `DescribeBucket`                 |
| `ecs/`             | `ListServices`, `ListTaskDefFamilies`, `DescribeService`, `DescribeFamily`, `ForceDeployment`, `CountRunningTasks` |
| `lambda/`          | `ListFunctions`, `GetFunction`, `InvokeFunction`                                               |
| `ssm/`             | `ListParameters`, `GetParameter`, `PutParameter`                                               |
| `secretsmanager/`  | `ListSecrets`, `GetSecretValue`, `PutSecretValue`                                              |
| `automation/`      | `ListDocuments`, `DescribeDocument`, `ListExecutions`, `StartExecution`, `GetExecution`, `StepLogSnapshot`, `IsTerminalStatus` |
| `logs/`            | `StartLiveTail`, `GetRecentEvents`                                                             |

### `internal/search/`

| File         | Purpose                                                           |
|--------------|-------------------------------------------------------------------|
| `fuzzy.go`   | `Fuzzy(query, []core.Row, limit) []Result` — wraps sahilm/fuzzy.  |

### `internal/prefs/`

| File            | Purpose                                                                    |
|-----------------|----------------------------------------------------------------------------|
| `db.go`         | `DB` + `Open` + schema migration (schemaVersion=2, `(package_id, row_key)`).|
| `favorites.go`  | `SetFavorite(core.Row)`, `UnsetFavorite(packageID, rowKey)`.               |
| `recents.go`    | `MarkVisited(core.Row)`.                                                   |
| `state.go`      | In-memory `State` snapshot + `IsFavorite(packageID, rowKey)`.              |
| `rows.go`       | `FavoriteRow` + `RecentRow` (`PackageID`, `RowKey`, `Display`).            |

## Authentication

Standard `aws-sdk-go-v2` credential chain. Profile resolution: `AWS_PROFILE` > `AWS_DEFAULT_PROFILE` > `default`. Region: `AWS_REGION` > `AWS_DEFAULT_REGION` > profile's configured region. `Ctrl+P` opens an in-TUI profile/region switcher that hot-swaps the AWS context without restarting.

## Debug log

`SCOUT_DEBUG=1` writes structured JSON to `~/.cache/scout/debug.log` (truncated per run). SDK log records (via a smithy-go adapter) and app-level events are both captured.

## Panic recovery

`cmd/scout/root.go` wraps `runTUI` in a `defer recover()` that writes a stack dump to `~/.cache/scout/crash.log` and returns a clean error to stderr.

## Testing policy

v0 has no automated tests. Quality gate is manual smoke testing. Every commit builds clean via `go build ./...` and `go vet ./...`.

## Code style

- No tests yet — v0 POC policy.
- Standard `gofmt`.
- Modules own their own lipgloss styles in `Manifest.TagStyle`.
- The only intentional type-switch outside the module registry is `BuildDetails` inside a module branching on row kind (e.g. S3 `Meta["kind"]`, Automation `Key` prefix). Everything else routes through `module.Module`.
- `core.Row.Meta` is `map[string]string` — modules document their meta keys in the module file. Multi-value fields are JSON-encoded into the bag.
- Toast levels: Info (purple), Success (green), Warning (yellow), Error (red).
- Destructive actions use `effect.Confirm` + a y/n gate.

## Known limitations

| Area              | Limitation                                                                        | Future fix                              |
|-------------------|-----------------------------------------------------------------------------------|-----------------------------------------|
| Windows           | `open`/`xdg-open` not supported; clipboard needs `xclip`/`xsel`/`wl-clipboard` on Linux | Add `cmd /c start` branch.        |
| Download dir      | XDG path with `$HOME/Downloads` fallback.                                         | Config file override.                   |
| Preview formats   | jpg, jpeg, png, txt, csv only.                                                    | Pluggable format registry.              |
| Temp files        | Not cleaned up by the program.                                                    | OS temp lifecycle handles it.           |
| ECS services      | DescribeServices runs on every ListServices call.                                 | Lazy describe on Details entry only.    |
| Cache             | No TTL, no size limit, no auto-expiration.                                        | Configurable retention.                 |
| Tests             | None.                                                                             | v1 priority.                            |
