package automation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// executionStatusBadge returns a color-coded status pill for the
// Status zone. Runbook terminal states use success/fail colors;
// in-progress and pending use amber; cancel-family states use dim.
func executionStatusBadge(status string) string {
	switch status {
	case "Success", "CompletedWithSuccess":
		return styleOk.Render("✓ " + status)
	case "Failed", "TimedOut", "CompletedWithFailure", "Rejected":
		return styleErr.Render("✗ " + status)
	case "InProgress", "Pending", "Waiting", "RunbookInProgress", "PendingApproval", "Scheduled":
		return styleWarn.Render("● " + status)
	case "Cancelling", "Cancelled":
		return styleDim.Render("⊘ " + status)
	default:
		return status
	}
}

// executionStatusIcon returns a one-character prefix for the Events
// zone rows. Mirrors the badge color mapping.
func executionStatusIcon(status string) string {
	switch status {
	case "Success", "CompletedWithSuccess":
		return styleOk.Render("✓")
	case "Failed", "TimedOut", "CompletedWithFailure", "Rejected":
		return styleErr.Render("✗")
	case "InProgress", "Pending", "Waiting", "RunbookInProgress", "PendingApproval", "Scheduled":
		return styleWarn.Render("●")
	case "Cancelling", "Cancelled":
		return styleDim.Render("⊘")
	default:
		return "·"
	}
}

// executionStatusText renders the plain status name in the matching
// color (no icon/prefix). Used inside Events rows where the icon
// renders separately.
func executionStatusText(status string) string {
	switch status {
	case "Success", "CompletedWithSuccess":
		return styleOk.Render(status)
	case "Failed", "TimedOut", "CompletedWithFailure", "Rejected":
		return styleErr.Render(status)
	case "InProgress", "Pending", "Waiting", "RunbookInProgress", "PendingApproval", "Scheduled":
		return styleWarn.Render(status)
	case "Cancelling", "Cancelled":
		return styleDim.Render(status)
	default:
		return status
	}
}

// truncateID shortens a long automation execution UUID to the first
// 8 characters plus an ellipsis so the Events zone stays readable.
func truncateID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}

// humanDuration renders a duration as "4m 12s" / "1h 3m" / "12s" —
// always two granular units max so the Events row stays compact.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d / time.Minute)
		s := int(d/time.Second) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d / time.Hour)
	m := int(d/time.Minute) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// humanTimeAgo returns a "3m ago" / "2h ago" / "5d ago" style
// relative timestamp. Zero-value input returns "".
func humanTimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
}

// wrapText word-wraps s so no line exceeds width cells. Long tokens
// without whitespace are preserved as a single unwrapped word.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if lipgloss.Width(cur)+1+lipgloss.Width(w) > width {
				out = append(out, cur)
				cur = w
			} else {
				cur = cur + " " + w
			}
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n")
}

// colorizeJSON pretty-prints + token-colors a JSON object or array.
// Returns (raw, false) when raw isn't a JSON object/array.
func colorizeJSON(raw string) (string, bool) {
	trim := strings.TrimSpace(raw)
	if trim == "" {
		return raw, false
	}
	var v interface{}
	if err := json.Unmarshal([]byte(trim), &v); err != nil {
		return raw, false
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}:
	default:
		return raw, false
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw, false
	}
	return colorizeJSONTokens(string(pretty)), true
}

func colorizeJSONTokens(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			out.WriteString(styleJSONPunct.Render(string(c)))
			i++
		case c == '"':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			k := j
			for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			token := s[i:j]
			if k < len(s) && s[k] == ':' {
				out.WriteString(styleJSONKey.Render(token))
			} else {
				out.WriteString(styleJSONString.Render(token))
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(s) {
				cc := s[j]
				if cc == '.' || cc == 'e' || cc == 'E' || cc == '+' || cc == '-' || (cc >= '0' && cc <= '9') {
					j++
					continue
				}
				break
			}
			out.WriteString(styleJSONNum.Render(s[i:j]))
			i = j
		case strings.HasPrefix(s[i:], "true"):
			out.WriteString(styleJSONKeyword.Render("true"))
			i += 4
		case strings.HasPrefix(s[i:], "false"):
			out.WriteString(styleJSONKeyword.Render("false"))
			i += 5
		case strings.HasPrefix(s[i:], "null"):
			out.WriteString(styleJSONKeyword.Render("null"))
			i += 4
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

var (
	styleOk          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#00875F", Dark: "#5FD7AF"})
	styleErr         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#FF5F5F"})
	styleWarn        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleJSONKey     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#87D7FF"})
	styleJSONString  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#008700", Dark: "#87D787"})
	styleJSONNum     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleJSONKeyword = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5F00AF", Dark: "#AF87FF"})
	styleJSONPunct   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#626262"})
)
