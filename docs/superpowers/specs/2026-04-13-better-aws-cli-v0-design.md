# scout — v0 design

**Status:** approved for implementation
**Date:** 2026-04-13
**Scope:** v0 proof-of-concept. Tests, Windows support, multi-service breadth, and release automation are explicitly deferred to v1.

## 1. Summary

`scout` is an interactive terminal CLI that speeds up AWS resource navigation. Launching it opens a full-screen TUI with a single search bar. Typing fuzzy-matches across cached S3 buckets, ECS services, and ECS task definition families; once you drill into a bucket, the same input doubles as a path breadcrumb and switches to prefix search against S3 keys. Hitting Enter opens a per-resource details view with a short set of actions (Open in Browser, Copy URI/ARN, Force Deploy, Tail Logs, Download, Preview).

It is a sibling project to the existing `scout` Chrome extension — same name (the binary is `scout`), no shared code.

## 2. Architecture & stack

- **Language:** Go 1.22+
- **Distribution:** single static binary installed as `scout` on `$PATH`. No runtime dependencies for end users.
- **TUI:** `charmbracelet/bubbletea` (Elm-style model/update/view) + `lipgloss` (styles, color, layout) + `bubbles` (textinput, viewport). Custom list renderer for neovim-style per-character highlighting.
- **AWS SDK:** `aws-sdk-go-v2` via the default credential chain (`config.LoadDefaultConfig` + `WithSharedConfigProfile`). Services used: `s3`, `ecs`, `cloudwatchlogs` (for `StartLiveTail`), `sts` (caller identity only).
- **Persistence:** `modernc.org/sqlite` (pure-Go, no CGO). One DB file per `(profile, region)` pair at `~/.cache/scout/<profile>__<region>.db`.
- **Fuzzy match:** `sahilm/fuzzy` or equivalent, chosen for its ability to return matched byte positions (drives highlighting).
- **Clipboard:** `atotto/clipboard`.
- **Process shape:** single foreground Bubble Tea program. Indexing runs as goroutines spawned from `Init`/`Update`, results flow back via `tea.Msg`. No daemon, no IPC.

### Project layout

```
scout/
  cmd/scout/           main.go — flag parsing, launch program
  internal/tui/             bubbletea model, views, keymaps, styles, details panel
  internal/search/          fuzzy + prefix engines, match-span extraction
  internal/index/           sqlite schema, upsert, load-into-memory
  internal/aws/             sdk wiring, credential chain, profile switcher
  internal/aws/s3/          ListBuckets, ListObjectsV2 (prefix, live)
  internal/aws/ecs/         ListClusters, ListServices, DescribeServices, task def families
  internal/aws/logs/        StartLiveTail wrapper
  internal/actions/         action interface + per-resource implementations
  internal/preview/         streaming download to temp file, open/xdg-open, cleanup
  internal/browser/         console URL builder, opener
  docs/superpowers/specs/   this document and its successors
```

### Startup flow

1. `main` parses flags, resolves the initial `(profile, region)` from the AWS SDK chain.
2. Opens (creates if missing) the cache DB for that pair.
3. Launches the Bubble Tea program. `Init` dispatches two commands:
   - `loadCache` — instant SQLite read into memory (sub-10ms for tens of thousands of rows).
   - `refreshTopLevel` — stale-while-revalidate fetch for buckets, ECS services, ECS task def families.
4. UI is responsive immediately from the cache; background fetches stream updates into the list as they complete.

## 3. Resource model & actions

### Resource types

| Type               | Tag    | Tag color | Source                                   | Indexed?          |
|--------------------|--------|-----------|------------------------------------------|-------------------|
| S3 bucket          | `S3`   | blue      | `ListBuckets`                            | Yes (SWR)         |
| S3 folder (prefix) | `DIR`  | cyan      | `ListObjectsV2` with `Delimiter=/`       | Opportunistic     |
| S3 object          | `OBJ`  | dim gray  | `ListObjectsV2`                          | Opportunistic     |
| ECS service        | `ECS`  | orange    | `ListClusters` → `ListServices` → `DescribeServices` | Yes (SWR) |
| ECS task def fam.  | `TASK` | yellow    | `ListTaskDefinitionFamilies(ACTIVE)`     | Yes (SWR)         |

### Unified resource struct

```go
type ResourceType int

const (
    RTypeBucket ResourceType = iota
    RTypeFolder
    RTypeObject
    RTypeEcsService
    RTypeEcsTaskDefFamily
)

type Resource struct {
    Type        ResourceType
    DisplayName string            // what the list renders, what fuzzy matches against
    Key         string            // unique within (profile, region, type)
    Meta        map[string]string // region, cluster, size, mtime, etc.
}
```

### Actions matrix

| Resource         | Actions (in order)                                                             |
|------------------|---------------------------------------------------------------------------------|
| S3 bucket        | 1. Open in Browser · 2. Copy URI · 3. Copy ARN                                  |
| S3 folder        | 1. Open in Browser · 2. Copy URI · 3. Copy ARN                                  |
| S3 object        | 1. Open in Browser · 2. Copy URI · 3. Copy ARN · 4. Download · 5. Preview       |
| ECS service      | 1. Open in Browser · 2. Force new Deployment · 3. Tail Logs                     |
| ECS task def fam.| 1. Open in Browser · 2. Copy ARN · 3. Tail Logs                                 |

### Action implementation notes

- **Open in Browser:** constructs an AWS console deep link including `?region=<region>`, then `open` (macOS) or `xdg-open` (Linux). Not a signed URL; the user's browser session controls authentication. URL shapes:
  - bucket → `https://s3.console.aws.amazon.com/s3/buckets/<bucket>?region=<r>`
  - folder → `https://s3.console.aws.amazon.com/s3/buckets/<bucket>?region=<r>&prefix=<prefix>&showversions=false`
  - object → `https://s3.console.aws.amazon.com/s3/object/<bucket>?region=<r>&prefix=<key>`
  - ecs service → `https://console.aws.amazon.com/ecs/v2/clusters/<cluster>/services/<svc>/health?region=<r>`
  - ecs task def → `https://console.aws.amazon.com/ecs/v2/task-definitions/<family>/<rev>?region=<r>`
- **Copy URI / Copy ARN:** writes the string to OS clipboard. URIs use the `s3://bucket/key` shape; ARNs use canonical AWS formats. S3 folders and objects get pseudo-ARNs: `arn:aws:s3:::<bucket>/<key>`.
- **Download** (objects): streams `GetObject` to `~/Downloads/<basename>`, shows a progress indicator in the status area, returns to the details view when complete.
- **Preview** (objects): streams `GetObject` to `$TMPDIR/scout/<uuid>.<ext>`, hands off to `open`/`xdg-open`, registers the temp file for cleanup on program exit. Hard size cap: 100 MB (rejected inline otherwise). Allowed v0 extensions: `.jpg`, `.jpeg`, `.png`, `.txt`, `.csv` (csv handed to the OS as plain text — no special table rendering inside the TUI).
- **Force new Deployment:** `ecs.UpdateService(ForceNewDeployment: true)`. No confirmation prompt. Returns to the details view with a "deployment triggered" toast. Failures surface as an error toast.
- **Tail Logs:** resolves log group(s) from `containerDefinitions[*].logConfiguration.options["awslogs-group"]` on the current task definition. If multiple container log groups, uses the first and shows a footer note ("tailing group X of Y"); switching between groups is a v1 concern. Calls `cloudwatchlogs.StartLiveTail`, renders into a full-screen viewport. `Esc` or `Ctrl+C` returns to the details view.
  - **Task def variant:** before starting the tail, a one-shot `ListTasks(Family=<family>, DesiredStatus=RUNNING)` decides whether to show a "no running tasks on latest revision — tail will be silent until one starts" notice. The tail itself starts regardless.

### Return behavior

After any non-streaming action completes, focus returns to the details view (not the search). `Esc` from the details view returns to search with the query and highlighted result preserved.

## 4. Search, navigation & keybindings

**The input bar is the breadcrumb.** Single text field at the top of the frame. Presence of `/` in the input determines search mode.

### Search modes

1. **Top-level (no `/` in input):** fuzzy match across all cached top-level resources (S3 buckets + ECS services + ECS task def families). The matcher returns matched byte positions, which drive per-character highlight spans in the result row. Resource type is shown via a colored tag so type disambiguation is always visible.
2. **Scoped (input contains `/`):** the input is parsed as `<bucket>/<prefix>`. Mode is **prefix**, not fuzzy, because that's what S3's own API supports efficiently.
   - First paint reads from the cache: `SELECT * FROM bucket_contents WHERE bucket=? AND key LIKE ? || '%'`.
   - Simultaneously fires a live `ListObjectsV2(Bucket=<b>, Prefix=<p>, Delimiter='/')`, streaming folder/object results into the list as they arrive.
   - Every streamed result is opportunistically upserted to `bucket_contents` (§5).

### Keybindings

| Key         | Effect                                                                                                     |
|-------------|------------------------------------------------------------------------------------------------------------|
| printable   | Inserts into input, recomputes results.                                                                    |
| `↑` / `↓`   | Move selection.                                                                                            |
| `Tab`       | Replaces input with selected row's name. If the row is enterable (bucket or folder), trailing `/` is appended so scope advances. Does **not** open the details view. |
| `Enter`     | Opens details view for the selected row.                                                                   |
| `Backspace` | Normal text delete. Deleting past a `/` naturally pops the scope back up — no special-case code.          |
| `Esc`       | In search: clears input. In details / tail / switcher / error: returns to the previous screen.             |
| `Ctrl+R`    | At top level: force re-index of top-level resources. Inside a scoped S3 view: bulk pre-fill of that entire bucket's contents. |
| `Ctrl+P`    | Opens the profile / region switcher overlay (§6).                                                          |
| `Ctrl+C`    | Quit. While a streaming or background action is active (tail logs, download, preview, bulk crawl), aborts that action instead. |

### Result row rendering

```
  [S3  ] my-prod-bucket-logs                    us-east-1
▸ [ECS ] payments-api                           prod-cluster
  [S3  ] payment-receipts                       us-east-1
  [TASK] payments-worker
```

- Selection indicator (`▸ ` or `  `, 2 cols).
- Colored type tag, fixed width (6 cols including brackets).
- Name, flex-width, with per-character highlight styling from the matcher (matched chars bold + high-contrast foreground; unmatched chars normal).
- Meta, right-aligned, dim foreground (region for buckets, cluster for services, size + mtime for objects).
- Selected row: full-width background bar.

### Ranking

- Top-level fuzzy: score from the matcher, tiebreak by type priority (S3 bucket > ECS service > ECS task def > folder > object), then lexicographic.
- Scoped prefix: lexicographic within folder-then-object groups (folders first).

### Empty states

- No cache, no query → `press / to begin searching — cache is empty, fetching…` with the activity spinner active.
- Query with zero matches at top level → `no matches in cache — try Ctrl+R to refresh`.
- Query with zero matches in a scoped view → live call decides; nothing special to render until it returns.

## 5. Caching & indexing

### Storage layout

- Directory: `~/.cache/scout/`
- One SQLite file per `(profile, region)` pair: `<profile>__<region>.db`
- Schema:

```sql
CREATE TABLE resources (
  type        TEXT NOT NULL,   -- 'bucket' | 'ecs_service' | 'ecs_taskdef'
  key         TEXT NOT NULL,
  name        TEXT NOT NULL,
  meta_json   TEXT NOT NULL,
  indexed_at  INTEGER NOT NULL,
  PRIMARY KEY (type, key)
);
CREATE INDEX resources_type ON resources(type);

CREATE TABLE bucket_contents (
  bucket     TEXT NOT NULL,
  key        TEXT NOT NULL,    -- full S3 key; folder virtual keys end in '/'
  is_folder  INTEGER NOT NULL,
  size       INTEGER,
  mtime      INTEGER,
  PRIMARY KEY (bucket, key)
);
CREATE INDEX bucket_contents_bucket_key ON bucket_contents(bucket, key);

CREATE TABLE meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
```

- Schema version pinned in `meta`. On load, if versions don't match, the DB is dropped and recreated. The cache is rebuildable, not a source of truth — migrations are not a v0 concern.

### Lifecycle

**On startup:**
1. Resolve `(profile, region)`.
2. Open / create the cache DB for that pair.
3. Instant in-memory load from `resources`.
4. UI comes up with cached data.
5. `refreshTopLevel` dispatches in the background.

**`refreshTopLevel` (stale-while-revalidate):**
- Three parallel subtasks, one per top-level resource type.
  - `listBuckets()` — one call.
  - `listEcsServices()` — `ListClusters` → per-cluster `ListServices` (paginated) → `DescribeServices` in batches of 10.
  - `listEcsTaskDefFamilies()` — `ListTaskDefinitionFamilies(status=ACTIVE)`, paginated. Latest revision resolved lazily via `DescribeTaskDefinition` at action time.
- Each subtask diff-patches the in-memory index (upsert new, delete missing), then writes the diff to SQLite in a single transaction.
- UI is notified via a `resourcesUpdated` message after each subtask completes.
- Triggered automatically on startup and on profile/region switch, or manually by `Ctrl+R`.

**Opportunistic caching:**
- Every resource that ever surfaces in the UI is upserted to SQLite before or as it reaches the render layer.
- A single `persistResources(rs []Resource)` helper sits between the AWS layer and the UI model; every result-producing path calls it.
- Applies to: top-level SWR results, scoped live `ListObjectsV2` results, pagination as the user scrolls, everything.
- Write path: `INSERT … ON CONFLICT(bucket,key) DO UPDATE SET size=excluded.size, mtime=excluded.mtime`, wrapped in a transaction per batch, executed off the UI goroutine.

**Scoped search = cached + live in parallel:**
- First paint from `bucket_contents` (instant, possibly empty on first visit).
- Live `ListObjectsV2` fires at the same time; results stream into the list and into the cache.

**`Ctrl+R` in a scoped view** crawls the entire current bucket with no delimiter and bulk-upserts every key. Progress shown in the status area; cancellable with `Ctrl+C` (partial crawls roll back).

**No TTL, no eviction.** Cache files grow unbounded. `scout cache clear` wipes `~/.cache/scout/` on demand. Stale-deleted keys linger until `Ctrl+R`.

### Concurrency

- Single `sql.DB` handle per loaded cache, serialized writes.
- SQLite opened in WAL mode.
- UI reads are always against the in-memory index; SQLite is only touched on load, on write-back, and on clear.

## 6. Profile / region switcher

**Initial context on launch** mirrors the AWS SDK chain:
1. `AWS_PROFILE` → that profile
2. else `AWS_DEFAULT_PROFILE` → that profile
3. else `default`
4. Region: `AWS_REGION` → `AWS_DEFAULT_REGION` → profile's configured `region` → modal fallback prompt if still unresolved.

`Ctrl+P` opens a centered modal overlay with two panes (Profile, Region):

```
┌─ Switch context ───────────────────────┐
│ Profile                 Region         │
│ ▸ default               ▸ us-east-1    │
│   prod-admin              us-west-2    │
│   staging                 eu-central-1 │
│   dev                     ap-south-1   │
│                                        │
│   (type to filter)                     │
└────────────────────────────────────────┘
```

- Profile list built by parsing `~/.aws/config` + `~/.aws/credentials` on overlay open.
- Region list is a static list of common regions plus whatever the selected profile has configured.
- `Tab` switches focus between panes. Substring filter while typing. `Enter` commits; `Esc` cancels.

### Commit semantics

1. Cancel the in-flight `refreshTopLevel` context for the old pair.
2. Close the old SQLite handle; open/create the new pair's DB.
3. Rebuild the in-memory index from the new DB (instant).
4. Kick off a fresh `refreshTopLevel`.
5. Clear search input and scope. Breadcrumb from the previous context is not carried over.
6. Status footer updates with the new profile/region/account. Account ID resolved lazily via `sts:GetCallerIdentity`, cached in `meta`.

### Error handling on switch

If the new profile fails to load credentials (e.g. SSO session expired), the overlay stays open with an inline error (`SSO session expired — run 'aws sso login --profile prod-admin'`). The old context remains active. No silent failures.

## 7. TUI layout & rendering

Three horizontal zones, composed with `lipgloss`:

```
┌─────────────────────────────────────────────────────────────────┐
│ > my-prod-bucket-logs/2026/                                  🔍 │
├─────────────────────────────────────────────────────────────────┤
│   [DIR ] 01/                                                    │
│ ▸ [DIR ] 02/                                                    │
│   [DIR ] 03/                                                    │
│   [OBJ ] backup.tar.gz          12.4 MB   2026-03-04 08:11      │
│   [OBJ ] index.json              8.2 KB   2026-04-12 15:40      │
├─────────────────────────────────────────────────────────────────┤
│ profile=prod-admin  region=us-east-1  acct=1234…  ⠋ ListObjectsV2│
└─────────────────────────────────────────────────────────────────┘
```

### Modes

The model holds a `Mode` enum and the `View` method switches on it:

- `modeSearch` — the layout above.
- `modeDetails` — replaces the result list with a Details panel over an Actions list. Details are read-only (v0: `Name`, `ARN`). Actions are the only interactable element.
- `modeTailLogs` — full-screen log viewport, footer shows `tailing <group> — Esc to return`. Auto-scroll pinned to bottom unless the user scrolls up.
- `modeSwitcher` — profile / region overlay from §6.
- `modeError` — a toast overlay at the bottom. Auto-dismiss after 4s or any key.

### Details view

```
┌─────────────────────────────────────────────────────────────────┐
│ > my-prod-bucket-logs                                        🔍 │
├─────────────────────────────────────────────────────────────────┤
│  Details                                                        │
│                                                                 │
│    Name   my-prod-bucket-logs                                   │
│    ARN    arn:aws:s3:::my-prod-bucket-logs                      │
│                                                                 │
│  Actions                                                        │
│                                                                 │
│  ▸ 1. Open in Browser                                           │
│    2. Copy URI                                                  │
│    3. Copy ARN                                                  │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ profile=prod-admin  region=us-east-1  acct=1234…                │
└─────────────────────────────────────────────────────────────────┘
```

- **Details panel:** a small component taking a `Resource` and rendering a list of `(label, value)` rows. v0 fields for every resource: `Name` and `ARN`. ECS task def families show `…resolving` until `DescribeTaskDefinition` returns, then the full revision ARN. Future fields (size, last-modified, cluster, desired count, image URI, etc.) are added by extending the panel's `switch r.Type`.
- **Actions list:** the only focusable widget. Number hotkeys for direct selection, `↑`/`↓`/`Enter` for keyboard navigation.

### Highlighting

- The matcher returns `[]int` matched byte positions on the display name.
- The renderer splits the name into alternating matched / unmatched spans and applies styles via `lipgloss`.
- Prefix mode uses the same mechanism with match positions `[0, len(prefix))`.

### Color palette

All via `lipgloss.AdaptiveColor` so it renders in both light and dark terminals.

- `S3` → bright blue
- `DIR` → cyan
- `OBJ` → dim gray
- `ECS` → orange (`#FF8800`)
- `TASK` → yellow
- Match highlight → bold, adaptive high-contrast foreground
- Selected row background → subtle bar fill
- Activity spinner → dim cyan
- Errors → red

### Activity indicator

A single status-line widget in the bottom-right, always visible:

- Animated spinner whenever ≥1 AWS API call is in flight.
- Shows the name of the most recent in-flight op (`⠋ ListObjectsV2`).
- Multiple concurrent calls show a count (`⠋ 3 calls…`).
- Driven by an `aws.middleware` stack entry that increments on request-start and decrements on response — every SDK call is instrumented automatically, no per-call-site bookkeeping.
- Failed calls still decrement the counter — no permanently stuck spinner.

### Terminal size handling

- On `tea.WindowSizeMsg`, recompute column widths: type tag fixed, meta right-aligns and truncates with `…`, name takes whatever's left.
- Minimum usable width: 60 columns. Below that, render a `terminal too narrow` placeholder.
- Mouse support: off.

## 8. Error handling & observability

### Philosophy

- **No silent failures.** Every error that reaches a UI boundary is rendered: either in the error-toast overlay or in an inline slot of the current mode (e.g. inside the profile switcher).
- AWS errors are unwrapped to their underlying `smithy.APIError` so users see real service codes (`AccessDenied`, `NoSuchBucket`, `ThrottlingException`) instead of generic Go strings.
- **Throttling:** no retry at our layer. `aws-sdk-go-v2`'s adaptive retryer handles it. If it still fails after the SDK's retry budget, the error surfaces as a toast.
- The activity spinner counter decrements on both success and failure, so it never sticks.

### Panic safety

The Bubble Tea program runs under a top-level `recover`:

- Dumps the panic to `~/.cache/scout/crash.log`.
- Restores the terminal state before exiting.
- No raw stack traces in the user's shell.

### Logging

- Structured file log at `~/.cache/scout/debug.log` using `log/slog` with a JSON handler.
- Off by default. Enabled via `SCOUT_DEBUG=1`.
- Logs every AWS API call: op name, duration, error, correlation id. **Never request bodies** — credential leak risk too high.
- Shares the same middleware that drives the activity spinner, so counters and logs stay in sync.

### Developer aids

- `SCOUT_DEBUG_VIEW=1` — extra status-bar row showing result count, current mode, and last message type.
- Crash log lives next to cache DBs for easy discovery.

## 9. Explicit non-goals for v0

1. **No tests.** v0 is a POC; test suites are deferred to v1.
2. **No services beyond the five resource types.** No RDS, Lambda, EC2, CloudWatch metrics, Secrets Manager, IAM. The service layer is structured to make adding more services cheap later.
3. **No filter shortcuts.** No `s3:` / `ecs:` prefixes, no `Alt+1/2` narrowing. Type tags in the result row are the only disambiguation.
4. **No per-revision task def indexing.** Families only; latest revision resolved lazily at action time.
5. **No STS AssumeRole gymnastics.** Whatever the SDK chain provides is what the tool uses. No cross-account juggling, no role-chaining UX.
6. **No signed console URLs.** Open-in-Browser relies on the user's existing console session.
7. **No confirmation on force-deploy.**
8. **No multi-container log group picker for Tail Logs.** First `awslogs-group` only; footer note if there are more.
9. **No inline text/csv rendering for Preview.** Temp file + `open`, always.
10. **No auto-refresh on a timer.** SWR fires only on launch and on `Ctrl+R`.
11. **No cache size limit, no eviction.** `cache clear` subcommand is the only cleanup.
12. **No mouse support.**
13. **No Windows support.** macOS (`open`) and Linux (`xdg-open`) only. Code is not hostile to Windows but the "open" paths are POSIX-only for v0.
14. **No CI/CD or release automation.** Local `go build`; `go install` or copy-to-`$PATH` for install. No Homebrew tap, no signed releases.
15. **No telemetry.** Ever.
