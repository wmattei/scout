# better-aws-cli — Phase 3: Action Implementations

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every action declared in Phase 2's Details view actually does something. Open opens the console in the default browser, Copy writes to the OS clipboard, Force Deploy triggers a rolling deployment, Download streams an object to the user's downloads directory, Preview streams an object to a temp file and hands it off to the OS file viewer, and Tail Logs opens a full-screen streaming viewport backed by CloudWatch Logs Live Tail. The `Details` view also lazily resolves extra data (latest task definition revision, log groups) as soon as it opens.

**Architecture:** Each action type is wired through a closure-based `Execute` field on the `Action` struct so the dispatcher in `update.go` is a one-liner. Actions that hit AWS return `tea.Cmd`s; synchronous actions (clipboard, browser) do their work inline and return a toast-producing message. A new `modeTailLogs` screen takes over the frame for the duration of a tail, with its own minimal keymap. While any action is in flight, the model sets an `inFlight` flag that blocks all input except `Ctrl+C`, and a "running: <label>" toast is pinned until the action finishes. Lazy task-def resolution runs as a `tea.Cmd` dispatched on entering `modeDetails`.

**Tech Stack:** Same as Phase 1–2 plus `github.com/atotto/clipboard` (OS clipboard), `github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs` (StartLiveTail), `charmbracelet/bubbles/viewport` (log scrolling).

**Scope boundary (what Phase 3 does NOT include):**
- `Ctrl+P` profile/region switcher overlay → **Phase 4**
- Panic recovery, debug log, cache-clear subcommand → **Phase 4**
- `Ctrl+R` bulk-crawl for S3 contents → **out of scope, deferrable indefinitely**
- Real error toasts for AWS failures — action errors use the same toast infrastructure but there's no unified error surface yet → **Phase 4**
- A config file for overriding defaults (download dir, preview formats, etc.) → **post-v0**

**Reference spec:** `docs/superpowers/specs/2026-04-13-better-aws-cli-v0-design.md`

**Working directory:** `/Users/wagnermattei/www/pied-piper/better-aws-cli`. Every command assumes this CWD.

**Testing policy:** No automated tests at v0. Each task ends with `go build ./...` and `git commit`. Manual verification lives in the final smoke-test task.

**Branch:** Work on `phase-3/actions`. All commits expected on this branch.

---

## Cross-OS limitations captured

The user picked Linux-style XDG defaults for download paths with explicit intent to revisit once a config file exists. Documenting here so Phase 4 or later can fix:

| Concern | v0 behavior | Future fix |
|---|---|---|
| Download directory | Reads `XDG_DOWNLOAD_DIR` from `~/.config/user-dirs.dirs`; falls back to `$HOME/Downloads`. On Windows `$HOME` is `%USERPROFILE%` — will land files in `C:\Users\<name>\Downloads`, which is correct but not via the Known Folders API | Config-driven override + Windows Known Folders lookup |
| `open` / `xdg-open` / `start` | macOS uses `open`, Linux uses `xdg-open`. No Windows branch — `openInBrowser` / `openPreview` return an error on Windows | Add `cmd /c start` for Windows, gate behind runtime.GOOS switch |
| Clipboard | `atotto/clipboard` works on macOS (`pbcopy`) and most Linux (`xclip`/`xsel`/`wl-copy`); fails with a clear error on headless systems without any clipboard backend | Document prerequisites or vendor an alternative |
| Temp file cleanup | Preview writes to `$TMPDIR/better-aws/<uuid>.<ext>`. **Not cleaned up by the program**. OS temp dir policies vary (macOS wipes on reboot; Linux `/tmp` varies) | Optional explicit cleanup on exit once Phase 4 adds a panic-safe shutdown hook |
| Preview formats | Whitelist is `.jpg/.jpeg/.png/.txt/.csv`. Anything else shows a toast error | Pluggable format registry, maybe auto-detection by content-type |

---

## File map

### New files

| Path | Responsibility |
|---|---|
| `internal/tui/browser.go` | AWS console URL builder + `openInBrowser` OS shell-out |
| `internal/tui/clipboard.go` | Thin wrapper around `atotto/clipboard` returning errors as strings for toasting |
| `internal/tui/downloads.go` | XDG-aware download path resolver + `downloadObject` helper |
| `internal/tui/preview.go` | Temp-path generator + format allowlist + `openPreview` shell-out |
| `internal/tui/tail.go` | `renderTailLogs` view for `modeTailLogs` |
| `internal/awsctx/s3/get.go` | `StreamObject` and `HeadObject` wrappers for GetObject / HeadObject |
| `internal/awsctx/ecs/update.go` | `ForceDeployment` wrapping `UpdateService(ForceNewDeployment=true)` |
| `internal/awsctx/ecs/describe.go` | `DescribeFamily` — DescribeTaskDefinition resolving latest revision + log groups |
| `internal/awsctx/logs/tail.go` | `StartLiveTail` wrapper returning an event-channel handle |

### Modified files

| Path | What changes |
|---|---|
| `internal/tui/actions.go` | Add `Execute` closure field, keep `ActionsFor` as the per-type source of truth |
| `internal/tui/mode.go` | Add `modeTailLogs` constant |
| `internal/tui/model.go` | Add `inFlight`, `inFlightLabel`, `taskDefDetails`, `tailStream`, `tailLines`, `tailGroup` fields |
| `internal/tui/update.go` | Action dispatcher, in-flight gating, `msgActionDone`, `msgTaskDefResolved`, tail-logs event pump, modeTailLogs key handling |
| `internal/tui/commands.go` | `resolveTaskDefCmd`, `tailLogsStartCmd`, `tailLogsNextCmd` |
| `internal/tui/details.go` | Show "…resolving" until `taskDefDetails` lands; add a details row for the log group if present |
| `internal/tui/view.go` | Dispatch `modeTailLogs` to `renderTailLogs` |

---

## Task 1: Browser URL builder + OS opener

**Files:**
- Create: `internal/tui/browser.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/browser.go` with EXACTLY this content:

```go
package tui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// consoleURL builds an AWS web-console deep link for the given resource.
// The region is always added as a query parameter so the console opens in
// the right place even if the user's browser session is set to a different
// default.
//
// For ECS task-def families the caller MUST pass the latest revision ARN
// in `taskDefArn` (resolved lazily on entering Details). If empty, the URL
// points at the family-level route which 404s on some consoles — the
// Details view blocks Open until the lazy resolution lands.
func consoleURL(r core.Resource, region string, taskDefArn string) string {
	switch r.Type {
	case core.RTypeBucket:
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s",
			url.PathEscape(r.Key), region)

	case core.RTypeFolder:
		bucket := r.Meta["bucket"]
		prefix := strings.TrimSuffix(r.Key, "/")
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s&prefix=%s&showversions=false",
			url.PathEscape(bucket), region, url.QueryEscape(prefix+"/"))

	case core.RTypeObject:
		bucket := r.Meta["bucket"]
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/object/%s?region=%s&prefix=%s",
			url.PathEscape(bucket), region, url.QueryEscape(r.Key))

	case core.RTypeEcsService:
		cluster := r.Meta["cluster"]
		// r.Key is the full service ARN. Extract the service name.
		svcName := lastARNSegment(r.Key)
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
			region, url.PathEscape(cluster), url.PathEscape(svcName), region)

	case core.RTypeEcsTaskDefFamily:
		// Prefer the resolved revision ARN when available.
		family := r.Key
		rev := ""
		if taskDefArn != "" {
			// arn:aws:ecs:...:task-definition/family:42
			if i := strings.LastIndexByte(taskDefArn, ':'); i > 0 {
				rev = taskDefArn[i+1:]
			}
		}
		if rev != "" {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s/%s?region=%s",
				region, url.PathEscape(family), url.PathEscape(rev), region)
		}
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s?region=%s",
			region, url.PathEscape(family), region)
	}
	return ""
}

// lastARNSegment returns the segment after the last `/` in an ARN. For
// "arn:aws:ecs:us-east-1:123:service/cluster/svc" it returns "svc".
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// openInBrowser hands off a URL to the OS's default browser launcher.
// Returns an error describing the problem so the caller can surface it
// via a toast. Windows is intentionally unsupported in v0 — see the
// "Cross-OS limitations" section of the Phase 3 plan.
func openInBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return fmt.Errorf("open-in-browser not supported on %s (v0 limitation)", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching browser: %w", err)
	}
	// We intentionally do not Wait() — xdg-open returns quickly but the
	// browser process may be long-lived, and we don't want to block.
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/browser.go
git commit -m "feat(tui): add AWS console URL builder and OS browser opener"
```

---

## Task 2: Clipboard wrapper

**Files:**
- Create: `internal/tui/clipboard.go`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/atotto/clipboard
```

- [ ] **Step 2: Create the file**

Create `internal/tui/clipboard.go` with EXACTLY:

```go
package tui

import (
	"fmt"

	"github.com/atotto/clipboard"
)

// copyToClipboard writes `s` to the OS clipboard. Wraps atotto/clipboard
// with a slightly friendlier error message so the toast surface can show
// something actionable on headless systems (where the underlying call
// fails with "xclip/xsel not found").
func copyToClipboard(s string) error {
	if err := clipboard.WriteAll(s); err != nil {
		return fmt.Errorf("copy to clipboard failed: %w (on Linux install xclip, xsel, or wl-clipboard)", err)
	}
	return nil
}
```

- [ ] **Step 3: Build**

```bash
go mod tidy
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/tui/clipboard.go
git commit -m "feat(tui): add clipboard wrapper around atotto/clipboard"
```

---

## Task 3: Downloads path helper

**Files:**
- Create: `internal/tui/downloads.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/downloads.go` with EXACTLY:

```go
package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// downloadsDir returns the directory where downloaded objects should be
// saved. Resolution order:
//
//  1. $XDG_DOWNLOAD_DIR environment variable
//  2. The XDG_DOWNLOAD_DIR entry in $HOME/.config/user-dirs.dirs (Linux)
//  3. $HOME/Downloads
//
// The directory is created (with parents) if it does not exist.
func downloadsDir() (string, error) {
	if env := os.Getenv("XDG_DOWNLOAD_DIR"); env != "" {
		if err := os.MkdirAll(env, 0o755); err != nil {
			return "", fmt.Errorf("creating XDG_DOWNLOAD_DIR %s: %w", env, err)
		}
		return env, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}

	// Parse ~/.config/user-dirs.dirs for XDG_DOWNLOAD_DIR.
	userDirs := filepath.Join(home, ".config", "user-dirs.dirs")
	if dir := parseUserDirsDownload(userDirs, home); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
		return dir, nil
	}

	// Fallback.
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

// parseUserDirsDownload reads a user-dirs.dirs file and returns the
// XDG_DOWNLOAD_DIR value with $HOME expanded, or "" if the file is
// missing or the entry is absent.
func parseUserDirsDownload(path, home string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "XDG_DOWNLOAD_DIR=") {
			continue
		}
		// Format: XDG_DOWNLOAD_DIR="$HOME/Downloads"
		val := strings.TrimPrefix(line, "XDG_DOWNLOAD_DIR=")
		val = strings.Trim(val, `"`)
		val = strings.ReplaceAll(val, "$HOME", home)
		return val
	}
	return ""
}

// downloadPathFor returns the absolute path under downloadsDir() to use
// for an object with the given basename. Collisions are not checked —
// existing files get overwritten.
func downloadPathFor(basename string) (string, error) {
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, basename), nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/downloads.go
git commit -m "feat(tui): add XDG-aware downloads directory resolver"
```

---

## Task 4: Preview path helper + format allowlist

**Files:**
- Create: `internal/tui/preview.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/preview.go` with EXACTLY:

```go
package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// previewAllowedExtensions lists the file extensions the Preview action
// will attempt to open. Anything else is rejected with a toast error.
// Update this set when you add support for more formats.
var previewAllowedExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".txt":  {},
	".csv":  {},
}

// previewAllowed reports whether the given object key has an extension
// that the preview action is willing to open. Case-insensitive.
func previewAllowed(key string) bool {
	ext := strings.ToLower(filepath.Ext(key))
	_, ok := previewAllowedExtensions[ext]
	return ok
}

// previewTempPath returns a unique temp-file path under
// `$TMPDIR/better-aws/` with the same extension as the object key. The
// parent directory is created if needed.
//
// The file is NOT cleaned up by the program — we rely on the OS temp
// dir lifecycle (macOS wipes on reboot, /tmp varies on Linux). See the
// Phase 3 plan for the explicit trade-off.
func previewTempPath(key string) (string, error) {
	dir := filepath.Join(os.TempDir(), "better-aws")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating preview temp dir %s: %w", dir, err)
	}
	ext := strings.ToLower(filepath.Ext(key))
	if ext == "" {
		ext = ".bin"
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating preview id: %w", err)
	}
	name := hex.EncodeToString(raw[:]) + ext
	return filepath.Join(dir, name), nil
}

// openPreview hands a file path off to the OS default handler for its
// extension. macOS uses `open`, Linux uses `xdg-open`; Windows is
// unsupported in v0. See the Phase 3 plan's "Cross-OS limitations"
// section.
func openPreview(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("preview not supported on %s (v0 limitation)", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching viewer: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/preview.go
git commit -m "feat(tui): add preview temp-path generator with format allowlist"
```

---

## Task 5: S3 GetObject stream wrapper

**Files:**
- Create: `internal/awsctx/s3/get.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/s3/get.go` with EXACTLY:

```go
package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// StreamObject streams the object at (bucket, key) into dst. Returns the
// number of bytes copied, plus any error from the SDK call or the copy.
//
// Callers that just want the size without downloading should use
// HeadObject instead.
func StreamObject(ctx context.Context, ac *awsctx.Context, bucket, key string, dst io.Writer) (int64, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("s3:GetObject (bucket=%s key=%s): %w", bucket, key, err)
	}
	defer out.Body.Close()

	n, err := io.Copy(dst, out.Body)
	if err != nil {
		return n, fmt.Errorf("copying object body: %w", err)
	}
	return n, nil
}

// HeadObject returns the object's size in bytes. Used by Preview to check
// the size cap before deciding whether to download.
func HeadObject(ctx context.Context, ac *awsctx.Context, bucket, key string) (int64, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("s3:HeadObject (bucket=%s key=%s): %w", bucket, key, err)
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/s3/get.go
git commit -m "feat(aws/s3): add StreamObject and HeadObject wrappers"
```

---

## Task 6: ECS UpdateService force-deployment wrapper

**Files:**
- Create: `internal/awsctx/ecs/update.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/ecs/update.go` with EXACTLY:

```go
package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// ForceDeployment triggers a rolling deployment on an ECS service by
// calling UpdateService with ForceNewDeployment=true. Neither the task
// definition nor the desired count is changed.
func ForceDeployment(ctx context.Context, ac *awsctx.Context, clusterArn, serviceArn string) error {
	client := awsecs.NewFromConfig(ac.Cfg)
	_, err := client.UpdateService(ctx, &awsecs.UpdateServiceInput{
		Cluster:            aws.String(clusterArn),
		Service:            aws.String(serviceArn),
		ForceNewDeployment: true,
	})
	if err != nil {
		return fmt.Errorf("ecs:UpdateService (service=%s): %w", serviceArn, err)
	}
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/ecs/update.go
git commit -m "feat(aws/ecs): add ForceDeployment wrapper for UpdateService"
```

---

## Task 7: ECS DescribeTaskDefinition wrapper

**Files:**
- Create: `internal/awsctx/ecs/describe.go`

- [ ] **Step 1: Create the file**

Create `internal/awsctx/ecs/describe.go` with EXACTLY:

```go
package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// TaskDefDetails is the minimal set of task-definition fields the TUI
// needs after lazy resolution: the full revision ARN, the family name,
// and each container's CloudWatch log group (when configured).
type TaskDefDetails struct {
	Family    string
	Revision  int32
	ARN       string
	LogGroups []string // one entry per container that has an awslogs log group
}

// DescribeFamily fetches the latest ACTIVE revision of a task definition
// family and returns a TaskDefDetails. The `family` argument is the bare
// family name (same as core.Resource.Key for RTypeEcsTaskDefFamily).
func DescribeFamily(ctx context.Context, ac *awsctx.Context, family string) (*TaskDefDetails, error) {
	client := awsecs.NewFromConfig(ac.Cfg)
	out, err := client.DescribeTaskDefinition(ctx, &awsecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(family),
	})
	if err != nil {
		return nil, fmt.Errorf("ecs:DescribeTaskDefinition (family=%s): %w", family, err)
	}
	td := out.TaskDefinition
	if td == nil {
		return nil, fmt.Errorf("ecs:DescribeTaskDefinition returned nil TaskDefinition for %s", family)
	}

	details := &TaskDefDetails{Family: family}
	if td.TaskDefinitionArn != nil {
		details.ARN = *td.TaskDefinitionArn
	}
	details.Revision = td.Revision

	for _, c := range td.ContainerDefinitions {
		if c.LogConfiguration == nil {
			continue
		}
		if string(c.LogConfiguration.LogDriver) != "awslogs" {
			continue
		}
		if group, ok := c.LogConfiguration.Options["awslogs-group"]; ok && group != "" {
			details.LogGroups = append(details.LogGroups, group)
		}
	}
	return details, nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/awsctx/ecs/describe.go
git commit -m "feat(aws/ecs): add DescribeFamily for lazy task-def resolution"
```

---

## Task 8: CloudWatch StartLiveTail wrapper

**Files:**
- Create: `internal/awsctx/logs/tail.go`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs
```

- [ ] **Step 2: Create the file**

Create `internal/awsctx/logs/tail.go` with EXACTLY:

```go
// Package logs wraps CloudWatch Logs Live Tail so the TUI can stream
// events through a plain Go channel without touching smithy event-stream
// types directly.
package logs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwl "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// TailEvent is a single log line surfaced to the TUI. Timestamp is
// milliseconds since epoch (what CloudWatch gives us).
type TailEvent struct {
	Timestamp int64
	Message   string
}

// TailStream wraps an in-flight StartLiveTail call. Events() returns a
// receive-only channel that is closed when the stream terminates. Close()
// cancels the underlying context and drains the channel.
type TailStream struct {
	Events <-chan TailEvent
	Err    <-chan error
	cancel context.CancelFunc
}

// Close stops the stream. Safe to call multiple times.
func (s *TailStream) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// StartLiveTail resolves the log group ARN for the given account+region
// and starts a live-tail stream. The returned TailStream pipes events as
// they arrive; close it when the caller is done.
func StartLiveTail(parentCtx context.Context, ac *awsctx.Context, logGroupName, account string) (*TailStream, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	client := cwl.NewFromConfig(ac.Cfg)
	arn := fmt.Sprintf("arn:aws:logs:%s:%s:log-group:%s", ac.Region, account, logGroupName)

	out, err := client.StartLiveTail(ctx, &cwl.StartLiveTailInput{
		LogGroupIdentifiers: []string{arn},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cloudwatchlogs:StartLiveTail (group=%s): %w", logGroupName, err)
	}

	evCh := make(chan TailEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(evCh)
		defer close(errCh)
		stream := out.GetStream()
		defer stream.Close()

		for ev := range stream.Events() {
			switch e := ev.(type) {
			case *cwltypes.StartLiveTailResponseStreamMemberSessionUpdate:
				for _, r := range e.Value.SessionResults {
					msg := ""
					if r.Message != nil {
						msg = *r.Message
					}
					ts := int64(0)
					if r.Timestamp != nil {
						ts = *r.Timestamp
					}
					select {
					case evCh <- TailEvent{Timestamp: ts, Message: msg}:
					case <-ctx.Done():
						return
					}
				}
			case *cwltypes.StartLiveTailResponseStreamMemberSessionStart:
				// Session metadata; ignore for v0.
			}
		}
		if err := stream.Err(); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	return &TailStream{
		Events: evCh,
		Err:    errCh,
		cancel: cancel,
	}, nil
}

// ensure aws package is referenced so goimports doesn't drop it when
// someone edits this file in the future — the ARN construction uses
// plain string formatting but we may switch to aws.String helpers.
var _ = aws.String
```

- [ ] **Step 3: Build**

```bash
go mod tidy
go build ./...
```

Expected: clean. If the SDK's `StartLiveTailResponseStreamMember*` type names have changed, report the exact compile error — do not guess.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/awsctx/logs/tail.go
git commit -m "feat(aws/logs): add StartLiveTail wrapper with channel interface"
```

---

## Task 9: Extend the Action struct with an Execute closure

**Files:**
- Modify: `internal/tui/actions.go`

- [ ] **Step 1: Overwrite the file**

Overwrite `internal/tui/actions.go` with EXACTLY:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// Action is a single selectable entry in the Details view's Actions list.
// Label is what the user sees; Execute is called (via the dispatcher in
// update.go) when the action is activated. Execute receives the current
// Model and returns a (new Model, tea.Cmd) pair using the same contract
// as Update so it can set fields on m, fire follow-up commands, or both.
//
// An Execute may be nil for actions that are not yet implemented — the
// dispatcher will fall back to the "not yet implemented" toast in that
// case. This is the migration path: Phase 3 tasks fill in each Execute
// one at a time, and nothing breaks along the way.
type Action struct {
	Label   string
	Execute ActionExecute
}

// ActionExecute is the function signature for an action's behavior. It
// mirrors bubbletea's Update signature so actions can freely mutate the
// model and dispatch side-effect commands.
type ActionExecute func(m Model) (Model, tea.Cmd)

// msgActionDone is emitted by any in-flight async action when its work
// completes. The dispatcher in update.go handles the message by clearing
// `inFlight` and showing the resulting toast.
type msgActionDone struct {
	toast string
	err   error
}

// ActionsFor returns the ordered action list for a resource type. The
// Execute fields are left nil in this declaration and populated by each
// action-implementation task so this file stays a single source of truth
// for ordering, labeling, and hotkey assignment.
func ActionsFor(t core.ResourceType) []Action {
	switch t {
	case core.RTypeBucket:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
		}
	case core.RTypeFolder:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
		}
	case core.RTypeObject:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy URI", Execute: execCopyURI},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Download", Execute: execDownload},
			{Label: "Preview", Execute: execPreview},
		}
	case core.RTypeEcsService:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Force new Deployment", Execute: execForceDeploy},
			{Label: "Tail Logs", Execute: execTailLogs},
		}
	case core.RTypeEcsTaskDefFamily:
		return []Action{
			{Label: "Open in Browser", Execute: execOpenInBrowser},
			{Label: "Copy ARN", Execute: execCopyARN},
			{Label: "Tail Logs", Execute: execTailLogs},
		}
	default:
		return nil
	}
}
```

- [ ] **Step 2: Build (expect failure)**

```bash
go build ./...
```

Expected: FAILS with `undefined: execOpenInBrowser`, `execCopyURI`, `execCopyARN`, `execDownload`, `execPreview`, `execForceDeploy`, `execTailLogs`. This is expected — each subsequent task adds one of these functions. DO NOT commit yet — stage the file and move to Task 10.

- [ ] **Step 3: Stage only**

```bash
git add internal/tui/actions.go
git status
```

Leave it staged. Task 10 will add `execOpenInBrowser` and ship a combined commit.

---

## Task 10: execOpenInBrowser action

**Files:**
- Create: `internal/tui/action_open.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_open.go` with EXACTLY:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// execOpenInBrowser builds the console URL for the details resource and
// hands it to openInBrowser. Synchronous (no network); produces a toast
// either way.
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

- [ ] **Step 2: Build**

The build will still fail because Tasks 11–16 haven't landed. DO NOT commit. Stage and move on.

```bash
go add internal/tui/action_open.go 2>/dev/null || git add internal/tui/action_open.go
git status
```

- [ ] **Step 3: No commit — proceed to Task 11**

---

## Task 11: execCopyURI and execCopyARN

**Files:**
- Create: `internal/tui/action_copy.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_copy.go` with EXACTLY:

```go
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execCopyURI copies a resource URI to the clipboard. Only S3 resources
// have URIs (s3://bucket/key). Other types show an informational toast.
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

// execCopyARN copies the resource ARN to the clipboard. ARNs come from
// Resource.ARN(), which handles every type in the current set. Task-def
// families rely on lazy resolution — if taskDefDetails has the revision
// ARN, use that; otherwise fall back to the family-only pseudo-ARN.
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

- [ ] **Step 2: Stage only**

```bash
git add internal/tui/action_copy.go
git status
```

No commit yet — still waiting on Tasks 12–16.

---

## Task 12: execForceDeploy action

**Files:**
- Create: `internal/tui/action_force_deploy.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_force_deploy.go` with EXACTLY:

```go
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execForceDeploy fires an ECS UpdateService(ForceNewDeployment=true) on
// the current details resource. Sets the in-flight lock so no other
// action can run concurrently; the lock is released in the msgActionDone
// handler. msgActionDone itself is declared in actions.go alongside the
// Action type so every action file can reach it without import cycles.
func execForceDeploy(m Model) (Model, tea.Cmd) {
	if m.detailsResource.Type != core.RTypeEcsService {
		m.toast = newToast("force deploy is only available for ECS services", 3*time.Second)
		return m, nil
	}
	cluster := m.detailsResource.Meta["clusterArn"]
	service := m.detailsResource.Key
	if cluster == "" || service == "" {
		m.toast = newToast("missing cluster or service ARN", 3*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "forcing new deployment…"
	m.toast = newToast("forcing new deployment…", 10*time.Second)
	ac := m.awsCtx
	return m, forceDeployCmd(ac, cluster, service)
}

// forceDeployCmd wraps the ECS UpdateService call in a tea.Cmd.
func forceDeployCmd(ac *awsctx.Context, clusterArn, serviceArn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := awsecs.ForceDeployment(ctx, ac, clusterArn, serviceArn)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("force deploy failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "deployment triggered", err: nil}
	}
}
```

- [ ] **Step 2: Stage only**

```bash
git add internal/tui/action_force_deploy.go
git status
```

---

## Task 13: execDownload action

**Files:**
- Create: `internal/tui/action_download.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_download.go` with EXACTLY:

```go
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execDownload streams the selected S3 object into the user's downloads
// directory and produces a toast on completion.
func execDownload(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeObject {
		m.toast = newToast("download is only available for S3 objects", 3*time.Second)
		return m, nil
	}
	bucket := r.Meta["bucket"]
	if bucket == "" {
		m.toast = newToast("object missing bucket metadata", 3*time.Second)
		return m, nil
	}

	basename := filepath.Base(r.Key)
	dest, err := downloadPathFor(basename)
	if err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "downloading…"
	m.toast = newToast(fmt.Sprintf("downloading to %s…", dest), 10*time.Second)
	return m, downloadCmd(m.awsCtx, bucket, r.Key, dest)
}

// downloadCmd streams an object to disk and emits msgActionDone.
func downloadCmd(ac *awsctx.Context, bucket, key, dest string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		f, err := os.Create(dest)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("create file failed: %v", err),
				err:   err,
			}
		}
		defer f.Close()

		n, err := awss3.StreamObject(ctx, ac, bucket, key, f)
		if err != nil {
			_ = os.Remove(dest)
			return msgActionDone{
				toast: fmt.Sprintf("download failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{
			toast: fmt.Sprintf("downloaded %s (%s)", dest, formatBytes(fmt.Sprintf("%d", n))),
			err:   nil,
		}
	}
}
```

- [ ] **Step 2: Stage only**

```bash
git add internal/tui/action_download.go
git status
```

---

## Task 14: execPreview action

**Files:**
- Create: `internal/tui/action_preview.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_preview.go` with EXACTLY:

```go
package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// previewSizeLimit caps how large an object may be before Preview refuses
// to fetch it. 100 MB matches the spec's hard limit.
const previewSizeLimit = 100 * 1024 * 1024

// execPreview fetches an object to a temp file and opens it with the OS
// default viewer. Rejects unsupported extensions and oversized objects
// via toast.
func execPreview(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeObject {
		m.toast = newToast("preview is only available for S3 objects", 3*time.Second)
		return m, nil
	}
	if !previewAllowed(r.Key) {
		m.toast = newToast("unsupported preview format (jpg, png, txt, csv only)", 4*time.Second)
		return m, nil
	}
	bucket := r.Meta["bucket"]
	if bucket == "" {
		m.toast = newToast("object missing bucket metadata", 3*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "preparing preview…"
	m.toast = newToast("preparing preview…", 10*time.Second)
	return m, previewCmd(m.awsCtx, bucket, r.Key)
}

// previewCmd head-checks the size, streams the object into a temp file,
// and hands it to the OS viewer. Returns msgActionDone for all paths.
func previewCmd(ac *awsctx.Context, bucket, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		size, err := awss3.HeadObject(ctx, ac, bucket, key)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview head failed: %v", err),
				err:   err,
			}
		}
		if size > previewSizeLimit {
			return msgActionDone{
				toast: fmt.Sprintf("object too large for preview (%s > 100 MB)", formatBytes(fmt.Sprintf("%d", size))),
				err:   fmt.Errorf("size %d over limit", size),
			}
		}

		path, err := previewTempPath(key)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview temp path: %v", err),
				err:   err,
			}
		}
		f, err := os.Create(path)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("create temp file: %v", err),
				err:   err,
			}
		}
		_, err = awss3.StreamObject(ctx, ac, bucket, key, f)
		_ = f.Close()
		if err != nil {
			_ = os.Remove(path)
			return msgActionDone{
				toast: fmt.Sprintf("preview download failed: %v", err),
				err:   err,
			}
		}
		if err := openPreview(path); err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview open failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "preview opened", err: nil}
	}
}
```

- [ ] **Step 2: Stage only**

```bash
git add internal/tui/action_preview.go
git status
```

---

## Task 15: execTailLogs action (stub until Tasks 16-20 wire the mode)

**Files:**
- Create: `internal/tui/action_tail.go`

- [ ] **Step 1: Create the file**

Create `internal/tui/action_tail.go` with EXACTLY:

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execTailLogs resolves the log group from cached task-def details and
// switches the TUI into modeTailLogs. Lazy resolution is triggered on
// entering modeDetails for both ECS services and task-def families
// (Task 17), so by the time the user activates this action the cache
// should be populated. If it isn't yet (resolution still in flight) we
// show a "details still loading" toast and the user can retry.
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

	m.mode = modeTailLogs
	m.tailGroup = group
	m.tailLines = nil
	m.inFlight = true
	m.inFlightLabel = "starting tail…"
	return m, tailLogsStartCmd(m.awsCtx, group, m.account)
}

// taskDefFamilyForDetails returns the task-def family name associated
// with the current details resource. For ECS task-def families the
// resource's Key is already the family name. For ECS services we read
// the family from Meta["taskDefFamily"], which is populated by the
// ListServices adapter extension in Task 17.
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

- [ ] **Step 2: Stage only**

```bash
git add internal/tui/action_tail.go
git status
```

No commit yet — this file references `m.tailGroup`, `m.tailLines`, `m.taskDefDetails`, `tailLogsStartCmd`, and `modeTailLogs` which don't exist yet. Task 17's model update + Tasks 18–20 will close the loop and land a combined commit.

---

## Task 16: msgActionDone handler + in-flight gating (stage only)

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Add the in-flight block at the top of `Update`**

Open `internal/tui/update.go`. Find the `tea.KeyMsg` case. Insert the in-flight block at the very start:

```go
	case tea.KeyMsg:
		if m.width < 60 && msg.String() != "ctrl+c" {
			return m, nil
		}
		if m.inFlight && msg.String() != "ctrl+c" {
			// Block every other action while an async action is running;
			// Ctrl+C always aborts the program regardless.
			return m, nil
		}
		switch m.mode {
```

- [ ] **Step 2: Add the `msgActionDone` case**

Inside the main `switch msg := msg.(type)` in `Update`, after the `msgScopedResults` case, add:

```go
	case msgActionDone:
		m.inFlight = false
		m.inFlightLabel = ""
		m.toast = newToast(msg.toast, 4*time.Second)
		return m, nil
```

- [ ] **Step 3: Route action activation through Action.Execute**

Locate the current stub in `updateDetails` where Enter / number keys produce the "not yet implemented" toast:

```go
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
```

Replace the stub bodies with the real dispatcher:

```go
	case "enter":
		return m.runAction(actions, m.actionSel)
	}
	// Number hotkeys 1..9 for direct selection + execution.
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(actions) {
				m.actionSel = idx
				return m.runAction(actions, idx)
			}
		}
	}
	return m, nil
```

- [ ] **Step 4: Add `runAction` at the bottom of `update.go`**

Append to `update.go`:

```go
// runAction dispatches the selected action via its Execute closure. If
// Execute is nil (not yet implemented), it falls back to the original
// stub toast so Phase 3 can migrate actions one at a time without
// breaking the UI.
func (m Model) runAction(actions []Action, idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(actions) {
		return m, nil
	}
	a := actions[idx]
	if a.Execute == nil {
		m.toast = newToast("not yet implemented — Phase 3", 3*time.Second)
		return m, nil
	}
	return a.Execute(m)
}
```

- [ ] **Step 5: Stage only**

```bash
git add internal/tui/update.go
git status
```

Still no commit — Task 17 adds the model fields that Tasks 12-15 reference.

---

## Task 17: Model state for in-flight, lazy details, tail, plus lazy-detail command (and final combined commit)

**Files:**
- Modify: `internal/awsctx/ecs/services.go` (capture task-def family on each service)
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/mode.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go` (handler for msgTaskDefResolved, services lazy trigger)
- Modify: `internal/tui/details.go` (render resolving / log-group row)

- [ ] **Step 0: Extend `internal/awsctx/ecs/services.go` to capture the task-def family**

In `ListServices`, the `DescribeServices` loop currently populates `core.Resource.Meta` with `cluster`, `clusterArn`, `launchType`, `desired`. Add one more entry: extract the task-def family name from `svc.TaskDefinition` (which is the full revision ARN like `arn:aws:ecs:us-east-1:123:task-definition/my-family:42`) and store it as `Meta["taskDefFamily"]`. This is what `execTailLogs` reads via `taskDefFamilyForDetails`, and it's also what the Enter handler uses to trigger lazy resolution for services.

Find the block inside `ListServices` where resources are built for each described service:

```go
for _, svc := range out.Services {
    if svc.ServiceArn == nil || svc.ServiceName == nil {
        continue
    }
    resources = append(resources, core.Resource{
        Type:        core.RTypeEcsService,
        Key:         *svc.ServiceArn,
        DisplayName: *svc.ServiceName,
        Meta: map[string]string{
            "cluster":    clusterShortName(cluster),
            "clusterArn": cluster,
            "launchType": string(svc.LaunchType),
            "desired":    fmt.Sprintf("%d", svc.DesiredCount),
        },
    })
}
```

Replace with:

```go
for _, svc := range out.Services {
    if svc.ServiceArn == nil || svc.ServiceName == nil {
        continue
    }
    meta := map[string]string{
        "cluster":    clusterShortName(cluster),
        "clusterArn": cluster,
        "launchType": string(svc.LaunchType),
        "desired":    fmt.Sprintf("%d", svc.DesiredCount),
    }
    if svc.TaskDefinition != nil {
        meta["taskDefFamily"] = taskDefFamilyFromArn(*svc.TaskDefinition)
    }
    resources = append(resources, core.Resource{
        Type:        core.RTypeEcsService,
        Key:         *svc.ServiceArn,
        DisplayName: *svc.ServiceName,
        Meta:        meta,
    })
}
```

Add the helper at the bottom of `services.go`:

```go
// taskDefFamilyFromArn extracts the family name from a task-definition
// ARN of the form
// arn:aws:ecs:<region>:<account>:task-definition/<family>:<revision>.
// Returns "" if the ARN doesn't match the expected shape.
func taskDefFamilyFromArn(arn string) string {
    // Find the segment after "task-definition/".
    const marker = "task-definition/"
    idx := strings.Index(arn, marker)
    if idx < 0 {
        return ""
    }
    rest := arn[idx+len(marker):]
    // Strip ":<revision>" suffix if present.
    if colon := strings.IndexByte(rest, ':'); colon >= 0 {
        return rest[:colon]
    }
    return rest
}
```

(The `strings` package is already imported by `services.go` — verify before committing.)

This change means that the next SWR refresh will repopulate Meta with the family. For an already-cached DB, existing rows won't have `taskDefFamily` until they get refreshed. That's acceptable — the user hits `Ctrl+R` (future phase) or restarts the binary to pick up the new shape on their existing cache, or we bump the schema version (not done here; trivial if it becomes a problem).

- [ ] **Step 1: Extend `Model` in `internal/tui/model.go`**

Update the `Model` struct to add the new fields. Find the existing struct and replace it with EXACTLY:

```go
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

	// In-flight async action — blocks further input until msgActionDone.
	inFlight      bool
	inFlightLabel string

	// Search-mode state.
	selected      int
	results       []search.Result
	scopedResults []search.Result
	scopedQuery   string

	// Details-mode state.
	detailsResource core.Resource
	actionSel       int
	// taskDefDetails caches the result of DescribeFamily (or equivalent)
	// for any task-def family whose Details view has been opened. Keyed
	// by family name. A present-but-nil entry means "resolution in
	// flight"; a missing entry means "not yet requested".
	taskDefDetails map[string]*awsecs.TaskDefDetails

	// Tail-logs-mode state.
	tailGroup  string                 // log group name currently being tailed
	tailLines  []string               // already-formatted lines in the scrollback
	tailStream *awslogs.TailStream    // cancellable stream handle
	tailViewport viewport.Model       // scrolling log viewport

	// Unused in Phase 2; reserved for Phase 4's refresh progress tracking.
	lastTopLevel []core.Resource
}
```

Update the `import` block at the top of `model.go` to include the new packages:

```go
import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)
```

Update `NewModel` to initialize the new map field:

```go
	return Model{
		memory:         memory,
		db:             db,
		awsCtx:         awsCtx,
		activity:       activity,
		input:          ti,
		width:          80,
		height:         24,
		mode:           modeSearch,
		taskDefDetails: make(map[string]*awsecs.TaskDefDetails),
	}
```

- [ ] **Step 2: Add `modeTailLogs` in `internal/tui/mode.go`**

Find the existing `const` block and add `modeTailLogs` at the end:

```go
const (
	modeSearch Mode = iota
	modeDetails
	modeTailLogs
)
```

Update the `String()` method to handle it:

```go
func (m Mode) String() string {
	switch m {
	case modeSearch:
		return "search"
	case modeDetails:
		return "details"
	case modeTailLogs:
		return "tail-logs"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 3: Add `resolveTaskDefCmd` to `internal/tui/commands.go`**

Append to `internal/tui/commands.go`:

```go

// msgTaskDefResolved carries the result of a DescribeFamily call for the
// given family. The handler populates m.taskDefDetails[family] so the
// Details view and action commands can read it.
type msgTaskDefResolved struct {
	family  string
	details *awsecs.TaskDefDetails
	err     error
}

// resolveTaskDefCmd kicks off a DescribeFamily call for the given family.
// The handler for msgTaskDefResolved stores the result in
// m.taskDefDetails so the Details view's ARN row and the Tail Logs
// action can read it.
func resolveTaskDefCmd(ac *awsctx.Context, family string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := awsecs.DescribeFamily(ctx, ac, family)
		return msgTaskDefResolved{family: family, details: d, err: err}
	}
}
```

- [ ] **Step 4: Handle `msgTaskDefResolved` in `internal/tui/update.go`**

Add a case inside the main `switch msg := msg.(type)`:

```go
	case msgTaskDefResolved:
		if msg.err != nil {
			// Phase 4 will surface this as an error toast.
			return m, nil
		}
		if m.taskDefDetails == nil {
			m.taskDefDetails = make(map[string]*awsecs.TaskDefDetails)
		}
		m.taskDefDetails[msg.family] = msg.details
		return m, nil
```

Also update the `updateSearch` Enter handler so entering a task-def Details view triggers lazy resolution:

Find:
```go
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
```

Replace with:
```go
	case "enter":
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
				// Mark as "in flight" with a nil value so the details
				// view can show "…resolving" instead of treating the
				// missing key as "not yet requested".
				m.taskDefDetails[family] = nil
				return m, resolveTaskDefCmd(m.awsCtx, family)
			}
		}
		return m, nil
```

- [ ] **Step 5: Update `internal/tui/details.go` to show the resolution state**

Find the ARN field in `renderDetails`:

```go
	writeField(&b, "ARN", r.ARN())
```

Replace with:

```go
	arn := r.ARN()
	if r.Type == core.RTypeEcsTaskDefFamily {
		// Replace the family-only pseudo-ARN with the resolved revision
		// ARN if lazy resolution has landed. A nil entry means "in
		// flight" — render an explicit resolving marker so the user
		// understands why actions may be disabled momentarily.
	}
	writeField(&b, "ARN", arn)
```

Wait — that block has no effect. Use this version instead:

Replace the single `writeField(&b, "ARN", r.ARN())` line with:

```go
	writeField(&b, "ARN", detailsARN(r, m))
```

And add `detailsARN` at the end of `details.go`:

```go
// detailsARN resolves the ARN shown in the Details view. For task-def
// families it returns the lazily-resolved revision ARN if available;
// otherwise it falls back to "…resolving" or the family pseudo-ARN.
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

And pass `m` into `renderDetails`. Change its signature:

```go
func renderDetails(r core.Resource, actionSel int, width int) string {
```

to:

```go
func renderDetails(m Model, width int) string {
```

then update the body to use `m.detailsResource`, `m.actionSel`, and `detailsARN(r, m)` in place of the old parameters. The final version of `renderDetails` reads:

```go
func renderDetails(m Model, width int) string {
	r := m.detailsResource
	actionSel := m.actionSel

	var b strings.Builder

	b.WriteString(styleDetailsHeader.Render("Details"))
	b.WriteString("\n\n")

	writeField(&b, "Name", r.DisplayName)
	writeField(&b, "ARN", detailsARN(r, m))

	// Log group row when we've resolved task-def details and at least
	// one log group is configured. Uses the same family-lookup logic as
	// the Tail Logs action so services and task-def families share the
	// same row.
	if family := taskDefFamilyForDetails(m); family != "" {
		if d, ok := m.taskDefDetails[family]; ok && d != nil && len(d.LogGroups) > 0 {
			writeField(&b, "Log", d.LogGroups[0])
		}
	}
	b.WriteString("\n")

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
```

- [ ] **Step 6: Update the call site in `internal/tui/view.go`**

Find:

```go
	case modeDetails:
		body = renderDetails(m.detailsResource, m.actionSel, m.width)
		body = padBlock(body, bodyHeight)
```

Replace with:

```go
	case modeDetails:
		body = renderDetails(m, m.width)
		body = padBlock(body, bodyHeight)
```

- [ ] **Step 7: Build (still expect failure)**

```bash
go build ./...
```

Expected: FAILS on `tailLogsStartCmd`, `renderTailLogs`, and the tail viewport types. Those come from Tasks 18–20.

- [ ] **Step 8: Stage everything**

```bash
git add internal/tui/model.go internal/tui/mode.go internal/tui/commands.go internal/tui/update.go internal/tui/details.go internal/tui/view.go
git status
```

Still no commit. Tasks 18–20 finish the tail-logs mode and will land a combined commit for the whole set.

---

## Task 18: Tail logs command + event pump

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Add `tailLogsStartCmd` and `tailLogsNextCmd`**

Append to `internal/tui/commands.go`:

```go

// msgTailStarted marks a successful StartLiveTail call. The handler
// stashes the stream on the model and schedules the first tailLogsNextCmd.
type msgTailStarted struct {
	stream *awslogs.TailStream
	err    error
}

// msgTailEvent carries one streamed log event to the Update loop. An
// event with Message=="" and Err!=nil means the stream terminated.
type msgTailEvent struct {
	ev  awslogs.TailEvent
	err error
	eof bool
}

// tailLogsStartCmd opens the StartLiveTail stream for the given log
// group. The returned tea.Cmd emits msgTailStarted; the Update handler
// stores the stream and schedules the first msgTailEvent pump.
func tailLogsStartCmd(ac *awsctx.Context, group, account string) tea.Cmd {
	return func() tea.Msg {
		stream, err := awslogs.StartLiveTail(context.Background(), ac, group, account)
		return msgTailStarted{stream: stream, err: err}
	}
}

// tailLogsNextCmd blocks until the next event arrives on the stream,
// then emits it as msgTailEvent. The handler schedules another
// tailLogsNextCmd to keep the pump going. When the stream closes the
// final message carries eof=true.
func tailLogsNextCmd(stream *awslogs.TailStream) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-stream.Events:
			if !ok {
				return msgTailEvent{eof: true}
			}
			return msgTailEvent{ev: ev}
		case err := <-stream.Err:
			return msgTailEvent{err: err, eof: true}
		}
	}
}
```

Make sure the `commands.go` import block includes `awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"` (add it if it's not there yet).

- [ ] **Step 2: Handle the tail messages in `internal/tui/update.go`**

Add inside the main `switch msg := msg.(type)` block:

```go
	case msgTailStarted:
		if msg.err != nil {
			m.inFlight = false
			m.inFlightLabel = ""
			m.mode = modeSearch
			m.toast = newToast("tail start failed: "+msg.err.Error(), 4*time.Second)
			return m, nil
		}
		m.tailStream = msg.stream
		m.inFlight = false
		m.inFlightLabel = ""
		m.toast = newToast("tailing "+m.tailGroup, 2*time.Second)
		return m, tailLogsNextCmd(msg.stream)

	case msgTailEvent:
		if msg.eof {
			m.tailStream = nil
			if msg.err != nil {
				m.toast = newToast("tail ended: "+msg.err.Error(), 4*time.Second)
			}
			return m, nil
		}
		line := formatTailLine(msg.ev)
		m.tailLines = append(m.tailLines, line)
		if len(m.tailLines) > 2000 {
			// Soft cap on scrollback to keep memory bounded.
			m.tailLines = m.tailLines[len(m.tailLines)-2000:]
		}
		m.tailViewport.SetContent(strings.Join(m.tailLines, "\n"))
		m.tailViewport.GotoBottom()
		if m.tailStream != nil {
			return m, tailLogsNextCmd(m.tailStream)
		}
		return m, nil
```

Also add the `strings` import to `update.go` if it's not already imported.

- [ ] **Step 3: Add `formatTailLine` at the bottom of `update.go`**

```go
// formatTailLine renders a single tail event into a display line with a
// local-time timestamp prefix.
func formatTailLine(ev awslogs.TailEvent) string {
	ts := time.Unix(0, ev.Timestamp*int64(time.Millisecond)).Local().Format("15:04:05.000")
	return ts + " " + ev.Message
}
```

Add the `awslogs` import:

```go
awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"
```

- [ ] **Step 4: Stage only**

```bash
git add internal/tui/commands.go internal/tui/update.go
git status
```

---

## Task 19: Tail logs view rendering

**Files:**
- Create: `internal/tui/tail.go`
- Modify: `internal/tui/view.go`
- Modify: `internal/tui/model.go` (initialize viewport)

- [ ] **Step 1: Create `internal/tui/tail.go`**

```go
package tui

import "fmt"

// renderTailLogs produces the full Tail Logs screen body. The viewport
// holds the streamed content; this function just wraps it with a header
// and a footer-help row.
func renderTailLogs(m Model, height int) string {
	header := styleDetailsHeader.Render("Tail Logs — " + m.tailGroup)
	help := styleRowDim.Render("Esc back    Ctrl+C stop")

	vpHeight := height - 3
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.tailViewport.Height = vpHeight
	m.tailViewport.Width = m.width

	body := m.tailViewport.View()

	return fmt.Sprintf("%s\n\n%s\n\n%s",
		header,
		body,
		help,
	)
}
```

- [ ] **Step 2: Dispatch in `view.go`**

Update the mode switch in `view.go`:

```go
	switch m.mode {
	case modeDetails:
		body = renderDetails(m, m.width)
		body = padBlock(body, bodyHeight)
	case modeTailLogs:
		body = renderTailLogs(m, bodyHeight)
	default:
		body = m.renderSearchBody(bodyHeight)
	}
```

- [ ] **Step 3: Initialize the viewport in `NewModel`**

In `internal/tui/model.go`, update `NewModel` to initialize the viewport:

```go
	return Model{
		memory:         memory,
		db:             db,
		awsCtx:         awsCtx,
		activity:       activity,
		input:          ti,
		width:          80,
		height:         24,
		mode:           modeSearch,
		taskDefDetails: make(map[string]*awsecs.TaskDefDetails),
		tailViewport:   viewport.New(80, 10),
	}
```

- [ ] **Step 4: Stage only**

```bash
git add internal/tui/tail.go internal/tui/view.go internal/tui/model.go
git status
```

---

## Task 20: Tail logs key handling + combined commit

**Files:**
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Route KeyMsg in modeTailLogs**

In `Update`'s `tea.KeyMsg` case, the current switch is:

```go
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		default:
			return m.updateSearch(msg)
		}
```

Replace with:

```go
		switch m.mode {
		case modeDetails:
			return m.updateDetails(msg)
		case modeTailLogs:
			return m.updateTail(msg)
		default:
			return m.updateSearch(msg)
		}
```

- [ ] **Step 2: Add `updateTail` at the bottom of `update.go`**

```go
// updateTail handles key events while in modeTailLogs. Esc stops the
// stream and returns to the Details view; Ctrl+C quits the program. All
// other keys are forwarded to the viewport so the user can scroll.
func (m Model) updateTail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		return m, tea.Quit
	case "esc":
		if m.tailStream != nil {
			m.tailStream.Close()
			m.tailStream = nil
		}
		m.mode = modeDetails
		m.toast = newToast("stopped tailing", 2*time.Second)
		return m, nil
	}
	// Forward scroll keys to the viewport.
	var cmd tea.Cmd
	m.tailViewport, cmd = m.tailViewport.Update(msg)
	return m, cmd
}
```

- [ ] **Step 3: Build (expect success)**

```bash
go build ./...
```

Expected: clean. All previously-staged files compile together.

- [ ] **Step 4: Single combined commit for Tasks 9–20**

```bash
git add internal/tui/update.go
git status
git commit -m "feat(tui): wire action dispatch, lazy task-def resolution, tail-logs mode, and all action implementations"
```

Every file that was staged across Tasks 9–20 should be in this one commit. Verify:

```bash
git show HEAD --stat
```

Expected files:
- `internal/tui/actions.go` (Task 9)
- `internal/tui/action_open.go`, `action_copy.go`, `action_force_deploy.go`, `action_download.go`, `action_preview.go`, `action_tail.go` (Tasks 10–15)
- `internal/tui/model.go`, `mode.go`, `commands.go`, `details.go`, `view.go`, `update.go`, `tail.go` (Tasks 16–20)

---

## Task 21: Build binary

**Files:** none.

- [ ] **Step 1: Build**

```bash
go build -o bin/better-aws ./cmd/better-aws
```

Expected: clean.

- [ ] **Step 2: Clean working tree**

```bash
git status
```

No commit — binary is gitignored.

---

## Task 22: Phase 3 smoke-test checklist

**Files:** none — manual verification.

Run from the project root with AWS credentials configured:

```bash
./bin/better-aws
```

### S3 bucket actions

- [ ] Select a real bucket, press Enter. Verify Details view shows Name + ARN + 3 actions.
- [ ] Press `1` (Open in Browser). Verify a browser tab opens to the S3 bucket page. Toast: `opened in browser`.
- [ ] Press `2` (Copy URI). Paste into another app — must read `s3://<bucket>`. Toast includes the URI.
- [ ] Press `3` (Copy ARN). Paste must read `arn:aws:s3:::<bucket>`.

### S3 folder / object actions

- [ ] Drill into a bucket via Tab. Select a folder, press Enter, verify 3 actions work (Open opens console prefix page; Copy URI/ARN include the path).
- [ ] Drill further, select an object, press Enter. Verify 5 actions listed.
- [ ] Press `4` (Download) on an object. Verify it streams to your downloads directory and the toast reports `downloaded <path> (<size>)`.
- [ ] Press `5` (Preview) on a `.png` or `.jpg` object. Verify it opens in Preview.app / default image viewer.
- [ ] Press `5` on a `.zip` or other unsupported format. Verify toast: `unsupported preview format`.
- [ ] Preview an oversized object (>100 MB). Verify toast: `object too large for preview`.

### ECS service actions

- [ ] Select an ECS service, press Enter. Verify Name + ARN + 3 actions shown.
- [ ] Press `1` (Open in Browser) — opens the ECS console service health page.
- [ ] Press `2` (Force new Deployment) — toast transitions through `forcing new deployment…` → `deployment triggered`. Verify in the AWS console that a new deployment started.

### ECS task def actions

- [ ] Select an ECS task def family, press Enter. Initially ARN shows `…resolving`, then updates to the revision ARN within ~1 second. If the family has a `awslogs` log group, the details view also shows a `Log  <group>` row.
- [ ] Press `1` (Open in Browser) — opens the ECS console task-def revision page.
- [ ] Press `2` (Copy ARN) — clipboard has `arn:aws:ecs:…:task-definition/<family>:<rev>`.

### Tail Logs

- [ ] On a task def family with a log group, press `3` (Tail Logs). Verify mode switches to the tail view with header `Tail Logs — <group>` and footer `Esc back    Ctrl+C stop`.
- [ ] Events stream in (if the service is producing logs). Scroll with ↑/↓/PgUp/PgDn.
- [ ] Press `Esc`. Verify stream stops and you return to Details view.
- [ ] Re-enter, then press `Ctrl+C`. Program exits cleanly; terminal is restored.

### In-flight gating

- [ ] Press `2` (Force new Deployment). While the toast says `forcing new deployment…`, try pressing `1` or other keys. Verify they are ignored until `deployment triggered` appears.
- [ ] Ctrl+C during the in-flight deploy exits the program.

### Fix, tag, report

- [ ] Fix any issues surfaced above. Commit each fix as `fix(tui): <what>` or similar.
- [ ] `git tag phase-3-complete`
- [ ] `git log --oneline phase-2-complete..phase-3-complete`
- [ ] Report completion to the user.

---

## Phase 3 complete — next up

At this point every action in the matrix does the real thing. Phase 4 closes the remaining items from the v0 spec: the `Ctrl+P` profile/region switcher overlay, panic-safe shutdown, a debug log file behind `BETTER_AWS_DEBUG=1`, the `cache clear` subcommand, and a real AWS-error toast surface that replaces the "Phase 4 will surface this" comments scattered throughout the code.
