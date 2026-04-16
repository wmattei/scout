// Package format provides pure string-formatting helpers shared across
// provider packages. This package must NOT import lipgloss or any other
// rendering library — it returns plain strings only. Callers that need
// styled output apply their own lipgloss styling after calling these functions.
package format

import (
	"encoding/json"
	"fmt"
	"time"
)

// HumanDuration renders a time.Duration as "34d", "6h", "12m", "45s".
func HumanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// UnixTime formats a Unix-seconds string as "2006-01-02 15:04" (local time).
// Returns "" on bad input.
func UnixTime(unixSecondsStr string) string {
	var unix int64
	if _, err := fmt.Sscanf(unixSecondsStr, "%d", &unix); err != nil || unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}

// TimeAge formats a Unix-seconds string as "2006-01-02 15:04 (2d ago)".
// Returns "" on bad input. The age suffix is a plain string — no lipgloss
// styling. Callers that want a styled suffix should call UnixTime and
// HumanDuration separately and wrap the age in their own style.
func TimeAge(unixSecondsStr string) string {
	var unix int64
	if _, err := fmt.Sscanf(unixSecondsStr, "%d", &unix); err != nil || unix <= 0 {
		return ""
	}
	t := time.Unix(unix, 0).Local()
	age := time.Since(t)
	return fmt.Sprintf("%s (%s ago)", t.Format("2006-01-02 15:04"), HumanDuration(age))
}

// Bytes formats a decimal byte-count string into a human-readable string
// like "12.4 MB". Returns "" on bad or negative input.
func Bytes(bytesStr string) string {
	var n int64
	if _, err := fmt.Sscanf(bytesStr, "%d", &n); err != nil || n < 0 {
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

// DecodeJSONSlice unmarshals a JSON-encoded []string. Returns nil on empty
// input or decode failure so callers can treat "missing" and "empty"
// identically.
func DecodeJSONSlice(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// DecodeStringMap unmarshals a JSON-encoded map[string]string. Returns nil
// on empty input or decode failure.
func DecodeStringMap(s string) map[string]string {
	if s == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
