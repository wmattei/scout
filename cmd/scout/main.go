// Command scout is an interactive TUI for navigating AWS resources.
//
// Argv forms:
//
//	scout                            — launch the TUI
//	scout preload [flags] <service>  — populate cache from AWS
//	scout cache clear                — wipe the on-disk cache
//
// Environment flags:
//
//	SCOUT_DEBUG=1  — enable the file-backed debug log at
//	                 $XDG_CACHE_HOME/scout/debug.log
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/debuglog"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/services"
	"github.com/wmattei/scout/internal/tui"

	// Provider registrations — blank imports trigger each package's
	// init() which calls services.Register for its providers.
	_ "github.com/wmattei/scout/internal/awsctx/automation"
	_ "github.com/wmattei/scout/internal/awsctx/ecs"
	_ "github.com/wmattei/scout/internal/awsctx/lambda"
	_ "github.com/wmattei/scout/internal/awsctx/s3"
	_ "github.com/wmattei/scout/internal/awsctx/secretsmanager"
	_ "github.com/wmattei/scout/internal/awsctx/ssm"
)

var Version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Cobra already prints the error; just exit non-zero.
		os.Exit(1)
	}
}

// rootCmd builds the root command tree. The root's RunE launches the
// TUI when invoked with no subcommand; subcommands override that.
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "scout",
		Short:        "Interactive AWS resource navigator",
		Long:         rootLongHelp,
		Version:      Version,
		SilenceUsage: true, // keep errors terse; usage only on bad flags
		RunE: func(cmd *cobra.Command, args []string) error {
			closeLog := debuglog.Init()
			defer closeLog()
			return runTUI()
		},
	}
	root.AddCommand(preloadCmd(), cacheCmd())
	return root
}

const rootLongHelp = `scout is an interactive terminal UI for navigating AWS infrastructure.

Running scout with no arguments launches the TUI. Subcommands let you
populate the local cache or wipe it.

Service scopes (type in TUI):
  s3:, buckets:                         S3 buckets
  ecs:, svc:, services:                 ECS services
  td:, task:, taskdef:                  ECS task definitions
  lambda:, fn:, functions:              Lambda functions
  ssm:, param:, params:, parameter:     SSM parameters
  secrets:, secret:, sm:, sec:          Secrets Manager secrets
  auto:, automation:, runbook:          SSM Automation documents

Key bindings:
  ↑/↓          Navigate results
  Tab          Autocomplete / drill into bucket
  Enter        Open details + actions
  f            Toggle favorite on focused resource
  Click        Copy Name / ARN / linked cell in Details
  Esc          Back
  Ctrl+P       Switch AWS profile/region
  Ctrl+C       Quit
  /            Filter (in tail logs)
  Opt+Bksp     Delete path segment

Environment:
  AWS_PROFILE, AWS_REGION    Standard SDK credential chain
  SCOUT_DEBUG=1              Enable debug log
  EDITOR                     Editor for Lambda Run / SSM Update / Secret Update`

// runTUI wraps the bubbletea program in a panic-safe hook. Any panic
// originating inside Init / Update / View / Cmd handlers gets a stack
// dump written to crash.log and is converted into a normal error
// returned to main.
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

	// Tell the index layer which types are top-level so it can build the
	// unified search snapshot. The data lives on the services registry;
	// copy it here once so internal/index doesn't need to import
	// internal/services and create a cycle.
	seedTopLevelTypes()

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

	prefsDB, prefsState, err := prefs.Open(awsCtx.Profile, awsCtx.Region)
	if err != nil {
		// Non-fatal: the TUI handles nil prefs gracefully.
		debuglog.Logger().Warn("prefs unavailable", "err", err)
		prefsDB, prefsState = nil, nil
	}
	defer prefsDB.Close()

	debuglog.Logger().Info("starting tui",
		"profile", awsCtx.Profile,
		"region", awsCtx.Region,
		"version", Version,
	)

	model := tui.NewModel(memory, db, awsCtx, activity, prefsDB, prefsState)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, runErr := program.Run(); runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
}

// seedTopLevelTypes populates the index layer's top-level-type registry
// from the services package. Shared by runTUI and preload so both paths
// build identical unified search snapshots.
func seedTopLevelTypes() {
	types := make([]core.ResourceType, 0)
	priority := make(map[core.ResourceType]int)
	for _, p := range services.TopLevel() {
		types = append(types, p.Type())
		priority[p.Type()] = p.SortPriority()
	}
	index.SetTopLevelTypes(types, priority)
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

func crashLogPath() string {
	dir, err := cacheDir()
	if err != nil {
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

// cacheCmd builds the `cache` subcommand group and its `clear` child.
func cacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local resource cache",
	}
	c.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Wipe the on-disk cache (preserves favorites/recents)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear()
		},
	})
	return c
}

// runCacheClear wipes the on-disk AWS resource cache. User preference
// files (*__prefs.db holding favorites and recents) are preserved
// by design — clearing the cache should not destroy user state.
func runCacheClear() error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		fmt.Println("scout: cache already clear")
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("listing %s: %w", dir, err)
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "__prefs.db") {
			continue
		}
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("removing %s: %w", name, err)
		}
		removed++
	}
	fmt.Printf("scout: cleared %d cache file(s) at %s (prefs preserved)\n", removed, dir)
	return nil
}
