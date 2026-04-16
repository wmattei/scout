# scout — Phase 4: Polish, Switcher, and Error Surface

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close out the v0 spec. Ship a `Ctrl+P` profile/region switcher overlay that hot-swaps the AWS context, add a panic-safe shutdown path that preserves a `crash.log`, add a `SCOUT_DEBUG=1` debug log that captures SDK + app events to `~/.cache/scout/debug.log`, add a `scout cache clear` subcommand, and wire every `// Phase 4 will surface this` TODO to an actual error toast with a red visual variant.

**Architecture:** The switcher is a new `modeSwitcher` that drops over the main frame while the user picks a profile + region; committing closes the old SQLite handle, opens a new DB scoped to `(profile, region)`, rebuilds the in-memory index, and fires a fresh top-level refresh. An in-flight refresh from the old context is not actively cancelled — if it's still running when the swap completes, its writes to the now-closed db fail silently and its final `msgResourcesUpdated` lands on the new in-memory index (where it's a harmless no-op because the new memory is what's already shown). This is acceptable for v0 and can be made clean with a session counter later. The debug log is an `internal/debuglog` package that initializes an `slog.Logger` + an adapter implementing smithy's `logging.Logger` interface; `awsctx.Resolve` picks between `logging.Nop{}` and the adapter based on the env var. Panic recovery is a single deferred `recover()` in `main.run()` that dumps the stack + routine state to the crash file and returns a normal error so the existing stderr handler prints and exits cleanly. The error toast surface is a `Level` field on the existing `Toast` type plus a red style variant; every site that previously emitted `toast: <plain text>` with an error context now uses `newErrorToast(<text>)`, and two message types (`msgResourcesUpdated`, `msgScopedResults`) grow `err` fields so the commands that drop errors today can route them to the Update handler.

**Tech Stack:** Same as Phase 3 plus `log/slog` (stdlib). No new external dependencies.

**Scope boundary (what Phase 4 does NOT include):**
- Search / cache refactor (deferred per user decision — keep Phase 3 behavior as-is)
- `Ctrl+R` bulk crawl for S3 contents (deferred per user decision)
- Windows support for `open` / `xdg-open` / clipboard (explicit v0 non-goal)
- Config file for user overrides (post-v0)
- `cache list` / `cache size` subcommands (only `cache clear` for v0)
- STS AssumeRole or SSO-specific UX (non-goal; standard SDK chain only)

**Reference spec:** `docs/superpowers/specs/2026-04-13-scout-v0-design.md`

**Working directory:** `/Users/wmattei/www/pied-piper/scout`. Every command assumes this CWD.

**Testing policy:** No automated tests at v0. Each task ends with `go build ./...` and `git commit`. Manual verification is in the final smoke-test task.

**Branch:** Work on `phase-4/polish-and-switcher`. All commits on this branch.

---

## File map

### New files

| Path | Responsibility |
|---|---|
| `internal/debuglog/debuglog.go` | slog setup, smithy-logger adapter, env-var gating, file lifecycle |
| `internal/tui/switcher.go` | profile+region picker state, `renderSwitcher` view, filter helpers |
| `internal/awsctx/profiles.go` | INI parser for `~/.aws/config` + `~/.aws/credentials`, region constants |
| `internal/awsctx/context.go` | `ResolveForProfile(profile, region)` helper that mirrors `Resolve` but takes explicit parameters (used by the switcher) |

### Modified files

| Path | What changes |
|---|---|
| `cmd/scout/main.go` | Subcommand routing (`cache clear`), panic recovery wrapper, debuglog init + teardown |
| `internal/awsctx/config.go` | Route `cfg.Logger` to debuglog adapter when enabled |
| `internal/tui/model.go` | `switcher Switcher` state, `refreshCancel func()` for context cancellation |
| `internal/tui/mode.go` | `modeSwitcher` constant |
| `internal/tui/update.go` | `Ctrl+P` trigger, modeSwitcher routing, `updateSwitcher`, switcher commit handler, error-toast plumbing for msgScopedResults / msgResourcesUpdated / msgTaskDefResolved / msgTailStarted / msgTailEvent |
| `internal/tui/view.go` | `modeSwitcher` dispatch |
| `internal/tui/commands.go` | `refreshTopLevelCmd` carries errors on msgResourcesUpdated; `scopedSearchCmd` carries err on msgScopedResults |
| `internal/tui/toast.go` | `Level` field on Toast, `newErrorToast` helper, `styleToastError` pick logic |
| `internal/tui/styles.go` | `styleToastError` red variant |

---

## Task 1: Debug log package

**Files:**
- Create: `internal/debuglog/debuglog.go`

- [ ] **Step 1: Create the file**

Create `internal/debuglog/debuglog.go` with EXACTLY this content:

```go
// Package debuglog owns the optional structured log file at
// $XDG_CACHE_HOME/scout/debug.log (or $HOME/.cache/scout/debug.log).
// It is gated on the environment variable SCOUT_DEBUG=1. When the
// variable is unset or set to any other value, all exported functions
// return no-op implementations so the rest of the program can call them
// unconditionally.
//
// The file is TRUNCATED at the start of each program run so a single
// log file represents a single session — easier to share when
// reproducing a bug. Rotating logs is out of scope for v0.
package debuglog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aws/smithy-go/logging"
)

// envVar is the gate that decides whether debug logging is active.
const envVar = "SCOUT_DEBUG"

// enabled caches the result of the env-var check at Init time so later
// callers don't have to re-consult the environment.
var (
	enabled  bool
	handle   *os.File
	logger   *slog.Logger
	sdkLogger logging.Logger = logging.Nop{}
)

// Init wires up the debug log. It returns a close function that the
// caller should defer from main; the close function is always safe to
// call, even when logging is disabled.
//
// If SCOUT_DEBUG is unset, Init is a no-op and the returned close
// function does nothing. Any error opening the log file is reported
// on stderr (because the TUI hasn't started yet) and the function
// degrades gracefully to a no-op logger.
func Init() func() {
	if os.Getenv(envVar) != "1" {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	path, err := logPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot resolve debug log path: %v\n", err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot create debug log dir: %v\n", err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot open debug log %s: %v\n", path, err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	enabled = true
	handle = f
	logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sdkLogger = smithyAdapter{logger: logger}

	logger.Info("debug log started", "path", path)

	return func() {
		if handle != nil {
			_ = handle.Sync()
			_ = handle.Close()
			handle = nil
		}
	}
}

// Enabled reports whether the debug log is active for this run.
func Enabled() bool { return enabled }

// Logger returns the app-level slog.Logger. When disabled, it returns
// a logger that drops every record.
func Logger() *slog.Logger {
	if logger == nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return logger
}

// SDKLogger returns a smithy-go logging.Logger suitable for plugging
// into aws.Config.Logger. When disabled, the returned logger is
// smithy's Nop{}.
func SDKLogger() logging.Logger { return sdkLogger }

// logPath resolves the absolute location of the debug log file.
func logPath() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout", "debug.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "scout", "debug.log"), nil
}

// smithyAdapter routes aws-sdk-go-v2 log records (delivered via the
// smithy logging.Logger interface) into our slog.Logger. The adapter
// is used only when debug logging is enabled; otherwise awsctx wires
// logging.Nop{} directly.
type smithyAdapter struct {
	logger *slog.Logger
}

func (a smithyAdapter) Logf(classification logging.Classification, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	switch classification {
	case logging.Warn:
		a.logger.LogAttrs(context.Background(), slog.LevelWarn, msg, slog.String("source", "sdk"))
	case logging.Debug:
		a.logger.LogAttrs(context.Background(), slog.LevelDebug, msg, slog.String("source", "sdk"))
	default:
		a.logger.LogAttrs(context.Background(), slog.LevelInfo, msg, slog.String("source", "sdk"))
	}
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/debuglog/debuglog.go
git commit -m "feat(debuglog): add gated slog-backed debug log with smithy adapter"
```

---

## Task 2: Route SDK logger through debuglog when enabled

**Files:**
- Modify: `internal/awsctx/config.go`

- [ ] **Step 1: Replace the logger wiring in `Resolve`**

In `internal/awsctx/config.go`, the Phase 3 fix set `cfg.Logger = logging.Nop{}`. Update the imports and the assignment to route through debuglog when the env var is set:

Find the import block and add the debuglog import:

```go
import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/wmattei/scout/internal/debuglog"
)
```

Remove the now-unused `"github.com/aws/smithy-go/logging"` import (it gets re-added by the debuglog package instead).

Find:

```go
	// Silence the default SDK logger so stray WARN/INFO lines don't
	// bleed onto the alt-screen and shift the TUI frame. A common
	// offender is the S3 GetObject "response has no supported checksum"
	// warning that fires on pre-checksum uploads. Phase 4 will swap
	// Nop{} for a file-backed logger gated on SCOUT_DEBUG=1.
	cfg.Logger = logging.Nop{}
```

Replace with:

```go
	// Route the SDK logger through debuglog. When SCOUT_DEBUG is
	// unset the adapter is smithy's Nop{}, so no output hits the
	// terminal and the alt-screen frame stays stable. With the env
	// var set, SDK records flow into
	// $XDG_CACHE_HOME/scout/debug.log alongside app-level
	// events.
	cfg.Logger = debuglog.SDKLogger()
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean. Unused-import errors on `smithy-go/logging` will appear if you left it in — remove it.

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/config.go
git commit -m "feat(awsctx): route cfg.Logger through debuglog (nop when unset)"
```

---

## Task 3: Panic recovery wrapper + debuglog init in main.go

**Files:**
- Modify: `cmd/scout/main.go`

- [ ] **Step 1: Overwrite `cmd/scout/main.go`**

Replace the entire file with:

```go
// Command scout is an interactive TUI for navigating AWS resources.
//
// Argv forms:
//
//	scout                 — launch the TUI
//	scout cache clear     — wipe the on-disk cache and exit
//
// Environment flags:
//
//	SCOUT_DEBUG=1  — enable the file-backed debug log at
//	                      $XDG_CACHE_HOME/scout/debug.log
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/debuglog"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/tui"
)

const Version = "0.0.0-phase4"

func main() {
	// Subcommand dispatch. Anything unrecognized falls through to the
	// TUI so legacy invocations don't break.
	if len(os.Args) >= 3 && os.Args[1] == "cache" && os.Args[2] == "clear" {
		if err := runCacheClear(); err != nil {
			fmt.Fprintf(os.Stderr, "scout: %v\n", err)
			os.Exit(1)
		}
		return
	}

	closeLog := debuglog.Init()
	defer closeLog()

	if err := runTUI(); err != nil {
		fmt.Fprintf(os.Stderr, "scout: %v\n", err)
		os.Exit(1)
	}
}

// runTUI wraps the bubbletea program in a panic-safe hook. Any panic
// originating inside Init / Update / View / Cmd handlers gets a stack
// dump written to crash.log and is converted into a normal error
// returned to main. Bubbletea's own Run() should restore the terminal
// before the panic propagates, but we belt-and-suspenders with a
// deferred os.Stdout write just in case.
func runTUI() (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			crashErr := writeCrashLog(r, stack)
			if crashErr != nil {
				fmt.Fprintf(os.Stderr, "scout: additionally failed to write crash log: %v\n", crashErr)
			}
			err = fmt.Errorf("panic recovered: %v (crash log written to %s)", r, crashLogPath())
		}
	}()

	ctx := context.Background()

	awsCtx, err := awsctx.Resolve(ctx)
	if err != nil {
		return err
	}

	activity := awsctx.NewActivity()
	activity.Attach(&awsCtx.Cfg)

	db, err := index.Open(awsCtx.Profile, awsCtx.Region)
	if err != nil {
		return err
	}
	defer db.Close()

	memory := index.NewMemory()
	cached, err := db.LoadAll(ctx)
	if err != nil {
		return err
	}
	memory.Load(cached)

	debuglog.Logger().Info("starting tui",
		"profile", awsCtx.Profile,
		"region", awsCtx.Region,
		"version", Version,
	)

	model := tui.NewModel(memory, db, awsCtx, activity)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, runErr := program.Run(); runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
}

// runCacheClear wipes the entire on-disk cache directory. Safe to call
// when the directory does not exist. Prints a single line on success.
func runCacheClear() error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		fmt.Println("scout: cache already clear")
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	fmt.Printf("scout: cleared cache at %s\n", dir)
	return nil
}

// cacheDir mirrors the resolver in internal/index but is duplicated
// here so the cache-clear subcommand works without opening a DB first.
func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "scout"), nil
}

// crashLogPath returns the absolute path to the crash log.
func crashLogPath() string {
	dir, err := cacheDir()
	if err != nil {
		// Fall back to the current working directory so the user still
		// has a way to find the dump.
		return "crash.log"
	}
	return filepath.Join(dir, "crash.log")
}

// writeCrashLog persists a panic + its stack to crash.log, overwriting
// any previous crash.
func writeCrashLog(panicVal interface{}, stack []byte) error {
	path := crashLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "scout %s crash log\n", Version)
	fmt.Fprintf(f, "panic: %v\n\n", panicVal)
	fmt.Fprintf(f, "stack:\n%s\n", stack)
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build -o bin/scout ./cmd/scout
```

Expected: clean.

- [ ] **Step 3: Verify cache clear works**

```bash
./bin/scout cache clear
```

Expected output: either `scout: cache already clear` or `scout: cleared cache at <path>`. Exit 0. No TUI.

If you already have a cache (likely from Phase 3 smoke tests), this will wipe it — that's fine, it'll be repopulated next launch.

- [ ] **Step 4: Commit**

```bash
git add cmd/scout/main.go
git commit -m "feat(cmd): add cache-clear subcommand, panic recovery, and debuglog init"
```

---

## Task 4: Toast Level field and red error variant

**Files:**
- Modify: `internal/tui/toast.go`
- Modify: `internal/tui/styles.go`

- [ ] **Step 1: Extend the Toast struct and add the helpers**

Overwrite `internal/tui/toast.go` with EXACTLY:

```go
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ToastLevel tags a toast as informational or as an error. Errors get
// the red style variant and a slightly longer default lifetime so the
// user can actually read the message.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastError
)

// Toast is a transient bottom-centered overlay displayed over whatever
// screen is currently rendered. A zero-valued Toast is "inactive":
// renderToast returns "" and the view layer skips the overlay.
type Toast struct {
	Message   string
	ExpiresAt time.Time
	Level     ToastLevel
}

// newToast returns an info-level Toast that expires after dur.
func newToast(message string, dur time.Duration) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(dur),
		Level:     ToastInfo,
	}
}

// newErrorToast returns an error-level Toast that stays up for 6s by
// default so the user has time to read it.
func newErrorToast(message string) Toast {
	return Toast{
		Message:   message,
		ExpiresAt: time.Now().Add(6 * time.Second),
		Level:     ToastError,
	}
}

// isActive reports whether the toast should currently render.
func (t Toast) isActive() bool {
	return t.Message != "" && time.Now().Before(t.ExpiresAt)
}

// renderToast returns a single-line overlay string centered horizontally,
// or "" if the toast is inactive. Errors render in a red style; info
// toasts use the default purple style. width is the full frame width.
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
	style := styleToast
	if t.Level == ToastError {
		style = styleToastError
	}
	boxed := style.Render(inner)
	left := (width - lipglossWidth(boxed)) / 2
	if left < 0 {
		left = 0
	}
	return strings.Repeat(" ", left) + boxed
}

// styleToast is the default (info) toast look. styleToastError is
// declared in styles.go alongside the rest of the palette.
var styleToast = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
	Background(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#5F005F"}).
	Padding(0, 1)
```

- [ ] **Step 2: Add `styleToastError` in `internal/tui/styles.go`**

Find the existing `styleStatusBar` / `styleSpinner` / `styleError` block inside the `var (...)` and add `styleToastError` next to `styleError`:

```go
	styleToastError = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
		Background(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#870000"}).
		Padding(0, 1)
```

Place the declaration so it stays inside the existing `var (...)` block. The order within the block doesn't matter as long as both `styleToast` (in toast.go) and `styleToastError` (in styles.go) exist when the package compiles.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean. There is no collision on `styleToast` because it's in `toast.go`, not `styles.go`.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/toast.go internal/tui/styles.go
git commit -m "feat(tui): add ToastLevel and red error-toast style variant"
```

---

## Task 5: Route AWS errors onto the toast surface

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Add `err` fields to the relevant message types in `update.go`**

Find the `type (...)` block with `msgResourcesUpdated` and `msgScopedResults`. Replace with:

```go
type (
	msgResourcesUpdated struct {
		errors []string // one string per failed subtask, empty on full success
	}
	msgAccount  struct{ account string }
	msgSpinTick struct{}

	// msgScopedResults carries the merged cache+live result set for a
	// scoped (bucket/prefix) search. `query` is the exact input value
	// that produced these results — the handler drops the message if
	// the input has moved on since, so stale results can't clobber
	// fresher ones. `err` is set when the live fetch failed; the
	// handler surfaces it as an error toast only if the query still
	// matches the current input.
	msgScopedResults struct {
		query   string
		results []search.Result
		err     string
	}
)
```

- [ ] **Step 2: Handle errors in the Update branches**

Find the `case msgResourcesUpdated:` branch and replace with:

```go
	case msgResourcesUpdated:
		// The SWR refresh wrote new data into m.memory. Recompute the
		// current top-level list against the updated snapshot.
		m.results = computeResults(m.input.Value(), m.memory)
		m.clampSelected()
		if len(msg.errors) > 0 {
			m.toast = newErrorToast(summarizeErrors(msg.errors))
		}
		return m, nil
```

Find the `case msgScopedResults:` branch and add error handling after the stale-query guard:

```go
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
		if msg.err != "" {
			m.toast = newErrorToast(msg.err)
		}
		return m, nil
```

Find the `case msgTaskDefResolved:` branch and replace with:

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

- [ ] **Step 3: Add `summarizeErrors` helper at the bottom of `update.go`**

Append to `update.go`:

```go
// summarizeErrors turns a slice of subtask error strings into a single
// toast message. One error yields its text; multiple are prefixed with
// a count so the user knows more than one thing broke.
func summarizeErrors(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) == 1 {
		return "refresh failed: " + errs[0]
	}
	return fmt.Sprintf("%d subtasks failed: %s", len(errs), errs[0])
}
```

Add the `fmt` import to `update.go` if it isn't already there.

- [ ] **Step 4: Update `refreshTopLevelCmd` in `commands.go` to return errors**

Find the body of the function returned by `refreshTopLevelCmd`. Replace the sequential subtask loop so errors are collected and forwarded:

```go
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

		type subtaskResult struct {
			typ core.ResourceType
			rs  []core.Resource
			err error
		}

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
		var errs []string
		for _, run := range subtasks {
			res := run()
			if res.err != nil {
				errs = append(errs, res.err.Error())
				continue
			}
			persist(ctx, db, mem, res.typ, res.rs)
		}
		cancel()
		return msgResourcesUpdated{errors: errs}
	}
}
```

- [ ] **Step 5: Update `scopedSearchCmd` to surface the error string**

Find the error branch in `scopedSearchCmd` (where the live fetch fails and we fall back to cache). Replace:

```go
		live, err := awss3.ListAtPrefix(ctx, ac, scope.Bucket, livePrefix, MaxDisplayedResults)
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
```

with:

```go
		live, err := awss3.ListAtPrefix(ctx, ac, scope.Bucket, livePrefix, MaxDisplayedResults)
		if err != nil {
			// On live failure, return whatever was in the cache so the
			// UI still shows something and forward the error text so
			// the Update handler can pop a toast.
			return msgScopedResults{
				query:   query,
				results: search.Prefix(scope.Leaf, cached, MaxDisplayedResults),
				err:     "scoped search failed: " + err.Error(),
			}
		}
```

- [ ] **Step 6: Update the tail-logs error paths in `update.go`**

Find the `case msgTailStarted:` branch and replace the error path so it uses `newErrorToast`:

```go
	case msgTailStarted:
		if msg.err != nil {
			m.inFlight = false
			m.inFlightLabel = ""
			m.mode = modeSearch
			m.toast = newErrorToast("tail start failed: " + msg.err.Error())
			return m, nil
		}
		m.tailStream = msg.stream
		m.inFlight = false
		m.inFlightLabel = ""
		m.toast = newToast("tailing "+m.tailGroup, 2*time.Second)
		return m, tailLogsNextCmd(msg.stream)
```

Find the `case msgTailEvent:` branch's eof error path and replace:

```go
		if msg.eof {
			m.tailStream = nil
			if msg.err != nil {
				m.toast = newErrorToast("tail ended: " + msg.err.Error())
			}
			return m, nil
		}
```

- [ ] **Step 7: Update `msgActionDone` to pick info vs error style**

Find the `case msgActionDone:` branch and replace with:

```go
	case msgActionDone:
		m.inFlight = false
		m.inFlightLabel = ""
		if msg.err != nil {
			m.toast = newErrorToast(msg.toast)
		} else {
			m.toast = newToast(msg.toast, 4*time.Second)
		}
		return m, nil
```

- [ ] **Step 8: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/update.go internal/tui/commands.go
git commit -m "feat(tui): surface AWS errors on the toast surface with error styling"
```

---

## Task 6: Profile + region data sources

**Files:**
- Create: `internal/awsctx/profiles.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/profiles.go` with EXACTLY:

```go
package awsctx

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProfiles parses ~/.aws/config and ~/.aws/credentials and returns
// a de-duplicated, sorted list of profile names. Supports the standard
// section shapes:
//
//	[default]
//	[profile my-profile]   (config only)
//	[my-profile]           (credentials only)
//
// Unknown / malformed sections are skipped. Missing files are treated
// as empty — no error.
func ListProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	addFromConfig(filepath.Join(home, ".aws", "config"), true, seen)
	addFromConfig(filepath.Join(home, ".aws", "credentials"), false, seen)

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// addFromConfig reads one INI-ish file and adds every section name to
// `seen`. When isConfig is true, `profile ` prefixes are stripped so
// the config file's verbose form lines up with the credentials file's
// plain form.
func addFromConfig(path string, isConfig bool, seen map[string]struct{}) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		name := strings.TrimSpace(line[1 : len(line)-1])
		if isConfig && strings.HasPrefix(name, "profile ") {
			name = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
		}
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
}

// CommonRegions is a curated list of AWS regions shown in the switcher
// overlay's region pane. Selected from the most commonly-used regions
// as of early 2026. Users whose profile resolves to a region outside
// this list still see that region pre-selected via the context's
// current Region value — the UI adds it to the list on the fly if it
// isn't already present.
var CommonRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"ca-central-1",
	"sa-east-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-central-1",
	"eu-north-1",
	"eu-south-1",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-east-1",
	"me-south-1",
	"af-south-1",
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/profiles.go
git commit -m "feat(awsctx): add profile INI parser and common-regions list"
```

---

## Task 7: ResolveForProfile helper

**Files:**
- Create: `internal/awsctx/context.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/context.go` with EXACTLY:

```go
package awsctx

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/wmattei/scout/internal/debuglog"
)

// ResolveForProfile is the same as Resolve but takes explicit profile
// and region parameters instead of reading the environment. Used by
// the TUI's profile/region switcher to hot-swap the AWS context
// without re-exec'ing the program.
//
// Both arguments MUST be non-empty; the switcher is responsible for
// picking from ListProfiles + CommonRegions (plus any pre-selected
// current region) before committing.
func ResolveForProfile(ctx context.Context, profile, region string) (*Context, error) {
	if profile == "" {
		return nil, fmt.Errorf("ResolveForProfile: profile is empty")
	}
	if region == "" {
		return nil, fmt.Errorf("ResolveForProfile: region is empty")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithSharedConfigProfile(profile),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config (profile=%s region=%s): %w", profile, region, err)
	}

	cfg.Logger = debuglog.SDKLogger()

	return &Context{
		Profile: profile,
		Region:  region,
		Cfg:     cfg,
	}, nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/context.go
git commit -m "feat(awsctx): add ResolveForProfile helper for switcher hot-swaps"
```

---

## Task 8: Switcher state + rendering

**Files:**
- Create: `internal/tui/switcher.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/switcher.go` with EXACTLY:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
)

// switcherPane identifies which pane currently has keyboard focus
// inside the profile/region overlay.
type switcherPane int

const (
	switcherPaneProfile switcherPane = iota
	switcherPaneRegion
)

// Switcher holds all state for the profile/region overlay. When
// Visible is false the overlay doesn't render and key events fall
// through to the previous mode.
type Switcher struct {
	Visible bool

	// Data sources.
	profiles []string
	regions  []string

	// Filters (substring, case-insensitive). The overlay re-applies
	// the filter on every keystroke so the visible index is always
	// computed against the filtered slice.
	profileFilter string
	regionFilter  string

	// Selection indices are into the currently filtered slices.
	profileSel int
	regionSel  int

	// Focused pane.
	focused switcherPane
}

// newSwitcher constructs a hidden Switcher seeded with data sources.
// currentProfile and currentRegion pre-select the user's current
// context so Enter without moving commits a no-op (and cancels the
// overlay rather than triggering a costly refresh).
func newSwitcher(currentProfile, currentRegion string) Switcher {
	profiles := awsctx.ListProfiles()
	if len(profiles) == 0 {
		// Fall back to whatever the current context is so the user at
		// least sees one row.
		profiles = []string{currentProfile}
	}
	regions := append([]string{}, awsctx.CommonRegions...)
	if !containsString(regions, currentRegion) {
		regions = append([]string{currentRegion}, regions...)
	}

	s := Switcher{
		profiles: profiles,
		regions:  regions,
		focused:  switcherPaneProfile,
	}
	s.profileSel = indexOf(profiles, currentProfile)
	s.regionSel = indexOf(regions, currentRegion)
	if s.profileSel < 0 {
		s.profileSel = 0
	}
	if s.regionSel < 0 {
		s.regionSel = 0
	}
	return s
}

// Show makes the switcher visible without resetting filters.
func (s *Switcher) Show() { s.Visible = true }

// Hide closes the overlay without committing.
func (s *Switcher) Hide() { s.Visible = false }

// filteredProfiles applies profileFilter and returns the visible slice
// plus a parallel slice of original indices into s.profiles so the
// caller can resolve the selection back to a real profile name.
func (s Switcher) filteredProfiles() ([]string, []int) {
	return applyFilter(s.profiles, s.profileFilter)
}

// filteredRegions mirrors filteredProfiles.
func (s Switcher) filteredRegions() ([]string, []int) {
	return applyFilter(s.regions, s.regionFilter)
}

// selectedProfile returns the profile name currently under the cursor,
// or "" if the filter matches nothing.
func (s Switcher) selectedProfile() string {
	vals, _ := s.filteredProfiles()
	if s.profileSel < 0 || s.profileSel >= len(vals) {
		return ""
	}
	return vals[s.profileSel]
}

// selectedRegion returns the region currently under the cursor, or ""
// if the filter matches nothing.
func (s Switcher) selectedRegion() string {
	vals, _ := s.filteredRegions()
	if s.regionSel < 0 || s.regionSel >= len(vals) {
		return ""
	}
	return vals[s.regionSel]
}

// applyFilter returns the subset of values whose lowercased form
// contains the lowercased filter, plus each match's original index.
func applyFilter(values []string, filter string) ([]string, []int) {
	if filter == "" {
		idxs := make([]int, len(values))
		for i := range values {
			idxs[i] = i
		}
		return values, idxs
	}
	low := strings.ToLower(filter)
	out := make([]string, 0, len(values))
	idxs := make([]int, 0, len(values))
	for i, v := range values {
		if strings.Contains(strings.ToLower(v), low) {
			out = append(out, v)
			idxs = append(idxs, i)
		}
	}
	return out, idxs
}

// renderSwitcher draws the overlay body. Called from view.go when
// m.switcher.Visible is true. The returned string is exactly `height`
// lines tall so it slots into the frame in place of the normal body.
func renderSwitcher(s Switcher, width, height int) string {
	if width < 50 {
		return centerEmptyState(width, height, "terminal too narrow for switcher")
	}

	header := styleDetailsHeader.Render("Switch AWS context")
	help := styleRowDim.Render("Tab switch pane    ↑/↓ select    Enter commit    Esc cancel")

	profileTitle := "Profile"
	regionTitle := "Region"
	if s.focused == switcherPaneProfile {
		profileTitle = "▸ " + profileTitle
	} else {
		regionTitle = "▸ " + regionTitle
	}

	paneWidth := (width - 6) / 2
	profileList := renderSwitcherPane(profileTitle, s.profileFilter, s.profiles, s.profileFilter, s.profileSel, s.focused == switcherPaneProfile, paneWidth)
	regionList := renderSwitcherPane(regionTitle, s.regionFilter, s.regions, s.regionFilter, s.regionSel, s.focused == switcherPaneRegion, paneWidth)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, profileList, "  ", regionList)

	body := strings.Join([]string{
		header,
		"",
		panes,
		"",
		help,
	}, "\n")

	return padBlock(body, height)
}

// renderSwitcherPane builds one pane of the overlay. Shows the pane
// title, the filter input (with a live caret when focused), and up to
// 12 visible rows of the filtered slice with the current selection
// highlighted.
func renderSwitcherPane(title, _ string, values []string, filter string, sel int, focused bool, width int) string {
	const maxRows = 12

	vals, _ := applyFilter(values, filter)

	var b strings.Builder
	b.WriteString(styleDetailsHeader.Render(title))
	b.WriteString("\n")

	filterLine := "filter: " + filter
	if focused {
		filterLine += "█"
	}
	b.WriteString(styleRowDim.Render(filterLine))
	b.WriteString("\n\n")

	if len(vals) == 0 {
		b.WriteString(styleRowDim.Render("  (no matches)"))
		return padPaneToHeight(b.String(), width, maxRows+4)
	}

	start := 0
	if sel >= maxRows {
		start = sel - maxRows + 1
	}
	end := start + maxRows
	if end > len(vals) {
		end = len(vals)
	}

	for i := start; i < end; i++ {
		indi := "  "
		line := vals[i]
		if i == sel {
			indi = styleSelIndi.Render("▸ ")
			line = styleRowSel.Width(width).Render(indi + line)
		} else {
			line = indi + line
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return padPaneToHeight(b.String(), width, maxRows+4)
}

// padPaneToHeight pads a pane string with blank lines until it has
// exactly `rows` lines so both panes align vertically in
// JoinHorizontal regardless of how many rows each filter matched.
func padPaneToHeight(s string, _, rows int) string {
	lines := strings.Count(s, "\n") + 1
	if lines >= rows {
		return s
	}
	return s + strings.Repeat("\n", rows-lines)
}

// containsString is a small helper for the region-list seeding logic.
func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

// indexOf returns the first index of target in xs, or -1.
func indexOf(xs []string, target string) int {
	for i, x := range xs {
		if x == target {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/switcher.go
git commit -m "feat(tui): add profile/region switcher state and view"
```

---

## Task 9: Model state for switcher + `modeSwitcher` + refresh cancel

**Files:**
- Modify: `internal/tui/mode.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `modeSwitcher` to `internal/tui/mode.go`**

Find the const block and add the new mode:

```go
const (
	modeSearch Mode = iota
	modeDetails
	modeTailLogs
	modeSwitcher
)
```

Update `String()`:

```go
func (m Mode) String() string {
	switch m {
	case modeSearch:
		return "search"
	case modeDetails:
		return "details"
	case modeTailLogs:
		return "tail-logs"
	case modeSwitcher:
		return "switcher"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 2: Add switcher state to `internal/tui/model.go`**

Add these fields to the `Model` struct:

```go
	// Switcher overlay state and the previous mode to return to on
	// Esc. `switcher.Visible` mirrors `mode == modeSwitcher`; keeping
	// both in sync is the responsibility of the Update handlers.
	switcher Switcher
	prevMode Mode
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean. The new fields are unused right now, which Go is fine with for struct fields.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/mode.go internal/tui/model.go
git commit -m "feat(tui): add modeSwitcher, switcher state, and refresh cancel hook"
```

---

## Task 10: Ctrl+P + updateSwitcher key handler + view dispatch

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/view.go`

- [ ] **Step 1: Add Ctrl+P handling in `updateSearch`**

Find the `case "ctrl+p", "ctrl+r", "esc":` line in `updateSearch` and split ctrl+p out so it opens the switcher:

```go
	case "ctrl+p":
		m.switcher = newSwitcher(m.awsCtx.Profile, m.awsCtx.Region)
		m.switcher.Show()
		m.prevMode = modeSearch
		m.mode = modeSwitcher
		return m, nil
	case "ctrl+r", "esc":
		return m, nil
```

Do the same under `updateDetails` — just check the same `ctrl+p` key and open the switcher, setting `m.prevMode = modeDetails` so Esc returns to the details view rather than jumping back to search. The Details key handler currently handles `ctrl+c`, `esc`, `up`, `down`, `enter`, and number keys. Add `"ctrl+p"` above `"ctrl+c"`:

```go
	case "ctrl+p":
		m.switcher = newSwitcher(m.awsCtx.Profile, m.awsCtx.Region)
		m.switcher.Show()
		m.prevMode = modeDetails
		m.mode = modeSwitcher
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
```

- [ ] **Step 2: Add `updateSwitcher` at the bottom of `update.go`**

```go
// updateSwitcher handles key events while the profile/region overlay is
// open. Esc hides the overlay and restores the previous mode; Enter
// commits the selection and triggers a context swap via
// commitSwitcherCmd; Tab flips focused panes; ↑/↓ move the selection;
// printable keys append to the focused pane's filter; Backspace trims
// one rune from the focused filter.
func (m Model) updateSwitcher(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.switcher.Hide()
		m.mode = m.prevMode
		return m, nil
	case "tab":
		if m.switcher.focused == switcherPaneProfile {
			m.switcher.focused = switcherPaneRegion
		} else {
			m.switcher.focused = switcherPaneProfile
		}
		return m, nil
	case "up":
		if m.switcher.focused == switcherPaneProfile && m.switcher.profileSel > 0 {
			m.switcher.profileSel--
		}
		if m.switcher.focused == switcherPaneRegion && m.switcher.regionSel > 0 {
			m.switcher.regionSel--
		}
		return m, nil
	case "down":
		if m.switcher.focused == switcherPaneProfile {
			vals, _ := m.switcher.filteredProfiles()
			if m.switcher.profileSel < len(vals)-1 {
				m.switcher.profileSel++
			}
		}
		if m.switcher.focused == switcherPaneRegion {
			vals, _ := m.switcher.filteredRegions()
			if m.switcher.regionSel < len(vals)-1 {
				m.switcher.regionSel++
			}
		}
		return m, nil
	case "enter":
		profile := m.switcher.selectedProfile()
		region := m.switcher.selectedRegion()
		if profile == "" || region == "" {
			m.toast = newErrorToast("switcher: nothing selected")
			return m, nil
		}
		// No-op commit when the user didn't actually change anything.
		if profile == m.awsCtx.Profile && region == m.awsCtx.Region {
			m.switcher.Hide()
			m.mode = m.prevMode
			return m, nil
		}
		m.inFlight = true
		m.inFlightLabel = "switching context…"
		return m, commitSwitcherCmd(profile, region)
	case "backspace":
		if m.switcher.focused == switcherPaneProfile && len(m.switcher.profileFilter) > 0 {
			r := []rune(m.switcher.profileFilter)
			m.switcher.profileFilter = string(r[:len(r)-1])
			m.switcher.profileSel = 0
		}
		if m.switcher.focused == switcherPaneRegion && len(m.switcher.regionFilter) > 0 {
			r := []rune(m.switcher.regionFilter)
			m.switcher.regionFilter = string(r[:len(r)-1])
			m.switcher.regionSel = 0
		}
		return m, nil
	}
	// Printable characters append to the focused filter.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= 32 {
			if m.switcher.focused == switcherPaneProfile {
				m.switcher.profileFilter += string(r)
				m.switcher.profileSel = 0
			} else {
				m.switcher.regionFilter += string(r)
				m.switcher.regionSel = 0
			}
		}
	}
	return m, nil
}
```

- [ ] **Step 3: Route `modeSwitcher` in the Update mode switch**

Find the mode dispatch block inside the `tea.KeyMsg` case in `Update` and add the switcher branch:

```go
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		case modeTailLogs:
			return m.updateTail(msg)
		case modeSwitcher:
			return m.updateSwitcher(msg)
		default:
			return m.updateSearch(msg)
		}
```

- [ ] **Step 4: Dispatch `renderSwitcher` in `view.go`**

Find the view's mode switch and add:

```go
	switch m.mode {
	case modeDetails:
		body = renderDetails(m, m.width)
		body = padBlock(body, bodyHeight)
	case modeTailLogs:
		body = renderTailLogs(m, bodyHeight)
	case modeSwitcher:
		body = renderSwitcher(m.switcher, m.width, bodyHeight)
	default:
		body = m.renderSearchBody(bodyHeight)
	}
```

- [ ] **Step 5: Build (expect failure until Task 11)**

```bash
go build ./...
```

Expected: `undefined: commitSwitcherCmd` — defined in Task 11. Stage everything and move on.

- [ ] **Step 6: Stage only**

```bash
git add internal/tui/update.go internal/tui/view.go
git status
```

---

## Task 11: `commitSwitcherCmd` and the context swap handler (combined commit)

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `commitSwitcherCmd` + `msgSwitcherCommitted` to `internal/tui/commands.go`**

Append to `internal/tui/commands.go`:

```go

// msgSwitcherCommitted carries the outcome of a profile/region swap.
// On success, the new Context replaces m.awsCtx, the new DB handle
// replaces m.db, and the in-memory index is swapped to the freshly
// loaded cache. On failure, the old state is preserved and an error
// toast is raised.
type msgSwitcherCommitted struct {
	ctx    *awsctx.Context
	db     *index.DB
	memory *index.Memory
	err    error
}

// commitSwitcherCmd runs the heavy lifting of a profile/region swap
// off the UI goroutine: load a new aws.Config via ResolveForProfile,
// open the matching SQLite file, LoadAll() into a fresh Memory, and
// return everything via msgSwitcherCommitted. The UI handler does
// the final state assignment so the swap is atomic from the Update
// loop's perspective.
func commitSwitcherCmd(profile, region string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		newCtx, err := awsctx.ResolveForProfile(ctx, profile, region)
		if err != nil {
			return msgSwitcherCommitted{err: err}
		}
		newDB, err := index.Open(newCtx.Profile, newCtx.Region)
		if err != nil {
			return msgSwitcherCommitted{err: err}
		}
		cached, err := newDB.LoadAll(ctx)
		if err != nil {
			_ = newDB.Close()
			return msgSwitcherCommitted{err: err}
		}
		mem := index.NewMemory()
		mem.Load(cached)
		return msgSwitcherCommitted{
			ctx:    newCtx,
			db:     newDB,
			memory: mem,
		}
	}
}
```

- [ ] **Step 2: Add the `msgSwitcherCommitted` handler to `internal/tui/update.go`**

Inside the main `switch msg := msg.(type)` block, add:

```go
	case msgSwitcherCommitted:
		m.inFlight = false
		m.inFlightLabel = ""
		if msg.err != nil {
			m.toast = newErrorToast("switch failed: " + msg.err.Error())
			return m, nil
		}
		// Close the old DB handle — we're done with it. Any still-
		// running refreshTopLevelCmd from the old context will fail
		// its next UpsertResources silently, which is acceptable for
		// v0 (see the Phase 4 plan's architecture note).
		if m.db != nil {
			_ = m.db.Close()
		}
		m.awsCtx = msg.ctx
		m.db = msg.db
		m.memory = msg.memory
		// The new context needs its own activity middleware so SDK
		// call instrumentation continues to work.
		m.activity.Attach(&m.awsCtx.Cfg)
		// Reset search state so the user lands on a clean frame.
		m.input.SetValue("")
		m.results = nil
		m.scopedResults = nil
		m.scopedQuery = ""
		m.selected = 0
		m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
		m.account = ""
		// Close the switcher overlay.
		m.switcher.Hide()
		m.mode = modeSearch
		m.toast = newToast(fmt.Sprintf("context: %s / %s", m.awsCtx.Profile, m.awsCtx.Region), 3*time.Second)
		// Kick off a fresh top-level refresh + re-resolve caller
		// identity for the new profile.
		return m, tea.Batch(
			refreshTopLevelCmd(m.awsCtx, m.db, m.memory),
			resolveAccountCmd(m.awsCtx),
		)
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: clean. All references resolved. If an import is missing (`awsecs`, `fmt`), add it.

- [ ] **Step 4: Combined commit for Tasks 10 + 11**

```bash
git add internal/tui/commands.go internal/tui/update.go internal/tui/model.go
git status
git commit -m "feat(tui): wire Ctrl+P switcher with context swap command"
```

The commit should cover the four files from Task 10 (update.go, view.go) + the two files touched in this task (commands.go, update.go — both already staged). Verify:

```bash
git show HEAD --stat
```

---

## Task 12: Build binary

**Files:** none.

- [ ] **Step 1: Produce `bin/scout`**

```bash
go build -o bin/scout ./cmd/scout
```

Expected: clean.

- [ ] **Step 2: Confirm working tree is clean**

```bash
git status
```

No commit — verification only.

---

## Task 13: Phase 4 smoke-test checklist

**Files:** none — manual verification.

### Cache-clear subcommand

- [ ] `./bin/scout cache clear` — prints either `cache already clear` or `cleared cache at <path>`, exits 0 without opening the TUI.
- [ ] Confirm the cache dir is gone: `ls ~/.cache/scout/ 2>&1` should show "No such file or directory".

### Debug log

- [ ] `SCOUT_DEBUG=1 ./bin/scout` — launch and quit with Ctrl+C.
- [ ] `ls ~/.cache/scout/debug.log` — the file exists.
- [ ] `cat ~/.cache/scout/debug.log | head` — each line is JSON with a `"msg":"starting tui"` or similar record.
- [ ] Relaunch `SCOUT_DEBUG=1 ./bin/scout` and quit. Verify the log was **truncated** — no records from the previous run should remain.
- [ ] Relaunch WITHOUT the env var. Verify the debug.log file is not touched / re-created / truncated.

### Panic recovery

- [ ] This one is hard to trigger without a fake panic site. If you want to verify it, temporarily insert a `panic("smoke test")` into `cmd/scout/main.go`'s `runTUI()` just before `program.Run()`. Launch the binary. Expected: the program exits 1, stderr reads `scout: panic recovered: smoke test (crash log written to ~/.cache/scout/crash.log)`, and the terminal is still usable. `cat ~/.cache/scout/crash.log` shows the stack trace. Remove the panic, rebuild, recommit if needed.

### Error toast surface

- [ ] Simulate a failure: launch with a bogus region via `AWS_REGION=xx-west-99 ./bin/scout`. Type a character. Verify a red toast appears at the bottom with the refresh error from the SDK (something like `refresh failed: operation error S3: ListBuckets, ...`).
- [ ] Select an ECS service → press `2` (Force new Deployment) on a service you don't have `ecs:UpdateService` permission for. Verify a red toast reads `force deploy failed: operation error ECS: UpdateService, ...`.

### Profile / region switcher

- [ ] Launch normally. Press `Ctrl+P`. Verify:
  - Overlay appears centered with two panes (`▸ Profile` / `Region`).
  - Profile pane lists every `[profile ...]` and `[default]` section from `~/.aws/config` / `~/.aws/credentials`.
  - Region pane shows the common-regions list plus whatever you're currently using.
  - Current profile and current region are pre-selected.
- [ ] Type into the profile filter — the list narrows on every keystroke.
- [ ] Press `Tab` — focus jumps to the region pane.
- [ ] `↑`/`↓` moves the selection.
- [ ] Press `Enter` with a *different* profile selected — switcher closes, a toast reads `context: <new-profile> / <region>`, the status bar updates, the search results reset, and typing triggers a fresh refresh against the new context.
- [ ] Repeat with `Esc` — overlay closes, no context change.
- [ ] `Ctrl+P` from inside the Details view: overlay opens, Esc returns to Details (not Search).

### Fix, tag, report

- [ ] Fix anything that broke, commit as `fix(tui): <what>`.
- [ ] `git tag phase-4-complete`
- [ ] `git log --oneline phase-3-complete..phase-4-complete`
- [ ] Report to the user.

---

## Phase 4 complete — v0 done

At this point the v0 spec is fully implemented: every resource type has its actions, every action works, the search flow works end-to-end, the profile/region switcher lives inside the TUI, the debug log is opt-in, panics are survivable, the cache can be nuked from the command line, and AWS errors actually reach the user.

**Deliberately-deferred items (still post-v0):**
- Search/cache refactor (user-deferred in Phase 4 brainstorming — keep current behavior)
- `Ctrl+R` bulk-crawl for S3 contents (user-deferred)
- Windows support for browser/preview openers and clipboard
- Config file for user overrides (download dir, preview formats, column widths, etc.)
- Automated tests of any kind
- Release automation (Homebrew, code signing, Windows builds)
- More services (RDS, Lambda, EC2, CloudWatch metrics, IAM, etc.)

After Phase 4 ships, a natural `v0.1.0` tag lands on `main` and the user picks which of the above to tackle first.
