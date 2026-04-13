// Command better-aws is an interactive TUI for navigating AWS resources.
// Phase 1 covers foundation + top-level search only; later phases add
// drill-in navigation, detail views, actions, and a profile switcher.
package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/tui"
)

const Version = "0.0.0-phase1"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "better-aws: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// 1. Resolve the AWS environment up front so we fail fast on bad creds.
	awsCtx, err := awsctx.Resolve(ctx)
	if err != nil {
		return err
	}

	// 2. Attach the activity counter so every subsequent SDK call is tracked.
	activity := awsctx.NewActivity()
	activity.Attach(&awsCtx.Cfg)

	// 3. Open the cache DB scoped to (profile, region).
	db, err := index.Open(awsCtx.Profile, awsCtx.Region)
	if err != nil {
		return err
	}
	defer db.Close()

	// 4. Build the in-memory index from whatever's cached on disk.
	memory := index.NewMemory()
	cached, err := db.LoadAll(ctx)
	if err != nil {
		return err
	}
	memory.Load(cached)

	// 5. Launch the bubbletea program.
	model := tui.NewModel(memory, db, awsCtx, activity)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
