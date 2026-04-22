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
	"os"

	// Provider registrations — blank imports trigger each package's
	// init() which calls services.Register for its providers.
	_ "github.com/wmattei/scout/internal/awsctx/automation"
	_ "github.com/wmattei/scout/internal/awsctx/ecs"
	_ "github.com/wmattei/scout/internal/awsctx/lambda"
	_ "github.com/wmattei/scout/internal/awsctx/s3"
	_ "github.com/wmattei/scout/internal/awsctx/secretsmanager"
	_ "github.com/wmattei/scout/internal/awsctx/ssm"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Cobra already prints the error; just exit non-zero.
		os.Exit(1)
	}
}
