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
// `$TMPDIR/scout/` with the same extension as the object key. The
// parent directory is created if needed.
//
// The file is NOT cleaned up by the program — we rely on the OS temp
// dir lifecycle (macOS wipes on reboot, /tmp varies on Linux).
func previewTempPath(key string) (string, error) {
	dir := filepath.Join(os.TempDir(), "scout")
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
// unsupported — see the Cross-OS limitations in CLAUDE.md.
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
