package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/cache"
	"github.com/wmattei/scout/internal/debuglog"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/modules"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/tui"
	"github.com/wmattei/scout/internal/version"
)

// rootCmd builds the root command tree. The root's RunE launches the
// TUI when invoked with no subcommand; subcommands override that.
func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "scout",
		Short:        "Interactive AWS resource navigator",
		Long:         rootLongHelp,
		Version:      version.Current,
		SilenceUsage: true, // keep errors terse; usage only on bad flags
		RunE: func(cmd *cobra.Command, args []string) error {
			closeLog := debuglog.Init()
			defer closeLog()
			return runTUI()
		},
	}
	root.AddCommand(cacheCmd())
	return root
}

const rootLongHelp = `scout is an interactive terminal UI for navigating AWS infrastructure.

Running scout with no arguments launches the TUI. Use ` + "`scout cache clear`" + `
to wipe the local cache.

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

	// Build the module registry. Every user-visible flow runs through
	// modules after the Cutover 14 legacy-deletion pass.
	registry := module.NewRegistry()
	modules.RegisterAll(registry)

	activity := awsctx.NewActivity()

	awsCtx, resolveErr := awsctx.Resolve(ctx)
	var (
		prefsDB    *prefs.DB
		prefsState *prefs.State
	)

	if resolveErr == nil {
		activity.Attach(&awsCtx.Cfg)

		prefsDB, prefsState, err = prefs.Open(awsCtx.Profile, awsCtx.Region)
		if err != nil {
			// Non-fatal: the TUI handles nil prefs gracefully.
			debuglog.Logger().Warn("prefs unavailable", "err", err)
			prefsDB, prefsState = nil, nil
		}
		defer prefsDB.Close()
	} else {
		// AWS context didn't resolve — we still launch the TUI so the
		// user lands in an onboarding flow instead of seeing a raw
		// stderr error. Downstream code paths guard on empty profile;
		// the switcher's commit flow reopens everything once the user
		// picks a profile + region.
		awsCtx = &awsctx.Context{}
		debuglog.Logger().Warn("aws resolve failed", "err", resolveErr)
	}

	debuglog.Logger().Info("starting tui",
		"profile", awsCtx.Profile,
		"region", awsCtx.Region,
		"version", version.Current,
	)

	var moduleCache *cache.DB
	if resolveErr == nil {
		moduleCache, err = cache.Open(awsCtx.Profile, awsCtx.Region)
		if err != nil {
			return fmt.Errorf("open module cache: %w", err)
		}
		defer moduleCache.Close()
		if err := moduleCache.PurgeOrphans(ctx, registry.IDs()); err != nil {
			debuglog.Logger().Warn("orphan purge failed", "err", err)
		}
	}

	model := tui.NewModel(awsCtx, activity, prefsDB, prefsState, registry, moduleCache)
	if resolveErr != nil {
		model = model.WithOnboarding(resolveErr.Error(), awsctx.ListProfiles())
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, runErr := program.Run(); runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
}

// cacheDir resolves the per-user scout cache directory, honouring XDG
// first and falling back to ~/.cache/scout. Used by cacheCmd's clear
// subcommand and by the crash-log writer.
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
	fmt.Fprintf(f, "scout %s crash log\n", version.Current)
	fmt.Fprintf(f, "panic: %v\n\n", panicVal)
	fmt.Fprintf(f, "stack:\n%s\n", stack)
	return nil
}
