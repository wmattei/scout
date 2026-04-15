package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

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
