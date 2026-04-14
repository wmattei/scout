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
