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
	"github.com/wmattei/scout/internal/awsctx/providers"
	"github.com/wmattei/scout/internal/debuglog"
	"github.com/wmattei/scout/internal/index"
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
			providers.RegisterAll()
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
	// unified search snapshot.
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
		"version", version.Current,
	)

	model := tui.NewModel(memory, db, awsCtx, activity, prefsDB, prefsState)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, runErr := program.Run(); runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
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
