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
//
// The running version is injected at build time into
// internal/version.Current by GoReleaser.
package main

import "os"

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Cobra already prints the error; just exit non-zero.
		os.Exit(1)
	}
}
