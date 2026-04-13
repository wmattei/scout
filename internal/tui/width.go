package tui

import "github.com/charmbracelet/lipgloss"

// lipglossWidth wraps lipgloss.Width so other files can import the function
// without pulling in the full package. Keeping it in its own file makes the
// import surface for status.go / results.go uniform.
func lipglossWidth(s string) int { return lipgloss.Width(s) }
