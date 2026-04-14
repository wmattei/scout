// Command better-aws is an interactive TUI for navigating AWS resources.
//
// Argv forms:
//
//	better-aws                 — launch the TUI
//	better-aws cache clear     — wipe the on-disk cache and exit
//
// Environment flags:
//
//	BETTER_AWS_DEBUG=1  — enable the file-backed debug log at
//	                      $XDG_CACHE_HOME/better-aws/debug.log
package main

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
)

const Version = "0.0.0-phase4"

func main() {
	// Subcommand dispatch. Anything unrecognized falls through to the
	// TUI so legacy invocations don't break.
	if len(os.Args) >= 3 && os.Args[1] == "cache" && os.Args[2] == "clear" {
		if err := runCacheClear(); err != nil {
			fmt.Fprintf(os.Stderr, "better-aws: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "preload" {
		closeLog := debuglog.Init()
		defer closeLog()
		if err := runPreload(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "better-aws: %v\n", err)
			os.Exit(1)
		}
		return
	}

	closeLog := debuglog.Init()
	defer closeLog()

	if err := runTUI(); err != nil {
		fmt.Fprintf(os.Stderr, "better-aws: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "better-aws: additionally failed to write crash log: %v\n", crashErr)
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
		fmt.Println("better-aws: cache already clear")
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	fmt.Printf("better-aws: cleared cache at %s\n", dir)
	return nil
}

// cacheDir mirrors the resolver in internal/index but is duplicated
// here so the cache-clear subcommand works without opening a DB first.
func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "better-aws"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "better-aws"), nil
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
	fmt.Fprintf(f, "better-aws %s crash log\n", Version)
	fmt.Fprintf(f, "panic: %v\n\n", panicVal)
	fmt.Fprintf(f, "stack:\n%s\n", stack)
	return nil
}
