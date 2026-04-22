package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// cacheCmd builds the `cache` subcommand group and its `clear` child.
func cacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local resource cache",
	}
	c.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Wipe the on-disk cache (preserves favorites/recents)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear()
		},
	})
	return c
}

// runCacheClear wipes the on-disk AWS resource cache. User preference
// files (*__prefs.db holding favorites and recents) are preserved
// by design — clearing the cache should not destroy user state.
func runCacheClear() error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		fmt.Println("scout: cache already clear")
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("listing %s: %w", dir, err)
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "__prefs.db") {
			continue
		}
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("removing %s: %w", name, err)
		}
		removed++
	}
	fmt.Printf("scout: cleared %d cache file(s) at %s (prefs preserved)\n", removed, dir)
	return nil
}
