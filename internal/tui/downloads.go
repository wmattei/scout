package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// downloadsDir returns the directory where downloaded objects should be
// saved. Resolution order:
//
//  1. $XDG_DOWNLOAD_DIR environment variable
//  2. The XDG_DOWNLOAD_DIR entry in $HOME/.config/user-dirs.dirs (Linux)
//  3. $HOME/Downloads
//
// The directory is created (with parents) if it does not exist.
func downloadsDir() (string, error) {
	if env := os.Getenv("XDG_DOWNLOAD_DIR"); env != "" {
		if err := os.MkdirAll(env, 0o755); err != nil {
			return "", fmt.Errorf("creating XDG_DOWNLOAD_DIR %s: %w", env, err)
		}
		return env, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}

	// Parse ~/.config/user-dirs.dirs for XDG_DOWNLOAD_DIR.
	userDirs := filepath.Join(home, ".config", "user-dirs.dirs")
	if dir := parseUserDirsDownload(userDirs, home); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
		return dir, nil
	}

	// Fallback.
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

// parseUserDirsDownload reads a user-dirs.dirs file and returns the
// XDG_DOWNLOAD_DIR value with $HOME expanded, or "" if the file is
// missing or the entry is absent.
func parseUserDirsDownload(path, home string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "XDG_DOWNLOAD_DIR=") {
			continue
		}
		// Format: XDG_DOWNLOAD_DIR="$HOME/Downloads"
		val := strings.TrimPrefix(line, "XDG_DOWNLOAD_DIR=")
		val = strings.Trim(val, `"`)
		val = strings.ReplaceAll(val, "$HOME", home)
		return val
	}
	return ""
}

// downloadPathFor returns the absolute path under downloadsDir() to use
// for an object with the given basename. Collisions are not checked —
// existing files get overwritten.
func downloadPathFor(basename string) (string, error) {
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, basename), nil
}

// formatBytes turns a decimal byte-count string into a human-readable
// suffix ("12.4 MB"). Empty or unparseable input returns "".
// Used by action_download.go and action_preview.go for toast messages.
func formatBytes(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n < 0 {
		return ""
	}
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
