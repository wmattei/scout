package secretsmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/format"
	"github.com/wmattei/scout/internal/services"
)

func init() { services.Register(&secretProvider{}) }

// secretProvider implements services.Provider for Secrets Manager
// secrets. The resource Key is the secret's bare Name (without the
// ARN's 6-char randomised suffix); the ARN is carried in Meta so the
// Provider hooks can return it without re-fetching.
type secretProvider struct {
	services.BaseProvider
}

func (secretProvider) Type() core.ResourceType { return core.RTypeSecretsManagerSecret }
func (secretProvider) Aliases() []string {
	return []string{"secrets", "secret", "sm", "sec"}
}
func (secretProvider) TagLabel() string { return "SEC" }

func (secretProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#D75F00", Dark: "#FF8700"})
}

func (secretProvider) SortPriority() int { return 5 }
func (secretProvider) IsTopLevel() bool  { return true }

// ARN prefers the lazy map (populated by GetSecretValue, which returns
// the full ARN with randomised suffix) and falls back to the Meta copy
// written at ListSecrets time. Both are full ARNs.
func (secretProvider) ARN(r core.Resource, lazy map[string]string) string {
	if lazy != nil {
		if v := lazy[MetaARN]; v != "" {
			return v
		}
	}
	return r.Meta[MetaARN]
}

// ConsoleURL builds the Secrets Manager console deep-link. The name
// may contain "/" so it is URL-encoded.
func (secretProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	encoded := url.QueryEscape(r.Key)
	return fmt.Sprintf("https://%s.console.aws.amazon.com/secretsmanager/secret?name=%s&region=%s",
		region, encoded, region)
}

// RenderMeta shows "(rotates)" when rotation is enabled and the first
// KMS key segment otherwise. Short so the column stays readable.
func (secretProvider) RenderMeta(r core.Resource) string {
	if r.Meta[MetaRotationEnabled] == "true" {
		return "rotates"
	}
	if k := r.Meta[MetaKmsKeyID]; k != "" {
		return path.Base(k)
	}
	return ""
}

func (secretProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListSecrets(ctx, ac, opts)
}

func (secretProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-arn", Label: "Copy ARN"},
		{ID: "copy-secret-value", Label: "Copy Value"},
		{ID: "update-secret-value", Label: "Update Value"},
	}
}

// AlwaysRefresh — secret values change out-of-band (rotation, manual
// updates) so we re-fetch on every Details entry.
func (secretProvider) AlwaysRefresh() bool { return true }

func (secretProvider) PollingInterval() time.Duration { return 0 }

// ResolveDetails calls GetSecretValue. Binary-only secrets surface a
// sentinel marker in the lazy map so DetailRows renders an explanatory
// row instead of an empty Value.
func (secretProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := GetSecretValue(ctx, ac, r.Key)
	if err != nil && err != ErrBinarySecret {
		return nil, err
	}
	out := map[string]string{
		"name":         d.Name,
		MetaARN:        d.ARN,
		"value":        d.Value,
		MetaVersionID:  d.VersionID,
	}
	if err == ErrBinarySecret {
		out["binary"] = "true"
	}
	// Carry forward Meta-sourced fields (ListSecrets populated them).
	for _, k := range []string{MetaDescription, MetaKmsKeyID, MetaLastChanged, MetaLastAccessed, MetaLastRotated, MetaRotationEnabled} {
		if v := r.Meta[k]; v != "" {
			out[k] = v
		}
	}
	return out, nil
}

// DetailRows renders the Secrets Manager details body. Rotation goes to
// the Status zone as a prominent badge; the value and supporting
// metadata fields share the Metadata zone.
func (secretProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil {
		return nil
	}

	rows := []services.DetailRow{
		{
			Zone:  services.ZoneStatus,
			Label: "Rotation",
			Value: styleDim.Render("Rotation ") + rotationBadge(lazy[MetaRotationEnabled] == "true"),
		},
	}

	if lazy["binary"] == "true" {
		rows = append(rows, services.DetailRow{
			Zone:  services.ZoneValue,
			Label: "Value",
			Value: styleDim.Render("<binary — use the console to view>"),
		})
	} else {
		rawValue := lazy["value"]
		rendered := renderSecretValue(rawValue)
		rows = append(rows, services.DetailRow{
			Zone:           services.ZoneValue,
			Label:          "Value",
			Value:          rendered,
			Clickable:      true,
			ClipboardValue: rawValue,
		})
	}

	if v := lazy[MetaVersionID]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Version", Value: v})
	}
	if ts := lazy[MetaLastChanged]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Changed", Value: styleDim.Render(format.TimeAge(ts))})
	}
	if ts := lazy[MetaLastRotated]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Rotated", Value: styleDim.Render(format.TimeAge(ts))})
	}
	if ts := lazy[MetaLastAccessed]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Accessed", Value: styleDim.Render(format.TimeAge(ts))})
	}
	if k := lazy[MetaKmsKeyID]; k != "" {
		rows = append(rows, services.DetailRow{Label: "KMS", Value: k})
	}
	if desc := lazy[MetaDescription]; desc != "" {
		rows = append(rows, services.DetailRow{Label: "Desc", Value: wrapText(desc, 60)})
	}

	return rows
}

// wrapText word-wraps s so no line exceeds width cells. Paragraphs
// (newline-separated spans) are wrapped independently; sequences with
// no whitespace are kept intact even when they exceed width rather
// than being char-chopped in the middle of a token.
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

// rotationBadge returns a green "enabled" badge when rotation is on and
// a dim "disabled" marker otherwise.
func rotationBadge(enabled bool) string {
	if enabled {
		return styleOk.Render("enabled")
	}
	return styleDim.Render("disabled")
}

var (
	styleOk         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#00875F", Dark: "#5FD7AF"})
	styleDim        = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleJSONKey    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#87D7FF"})
	styleJSONString = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#008700", Dark: "#87D787"})
	styleJSONNum    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleJSONKeyword = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5F00AF", Dark: "#AF87FF"})
	styleJSONPunct  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#626262"})
)

// renderSecretValue returns the secret value prepared for display in
// the Value zone: pretty-printed JSON with per-token coloring when the
// payload parses cleanly, otherwise the raw string unchanged. Callers
// still copy the original raw value on click — the coloring is visual
// only.
func renderSecretValue(raw string) string {
	if colored, ok := colorizeJSON(raw); ok {
		return colored
	}
	return raw
}

// colorizeJSON parses raw as JSON and returns a pretty-printed,
// token-colored version. Returns (raw, false) when raw is not valid
// JSON — numbers and bare strings parse successfully as JSON so we
// gate on the pretty output actually starting with '{' or '['.
func colorizeJSON(raw string) (string, bool) {
	trim := strings.TrimSpace(raw)
	if trim == "" {
		return raw, false
	}
	var v interface{}
	if err := json.Unmarshal([]byte(trim), &v); err != nil {
		return raw, false
	}
	// Only treat objects and arrays as "JSON" — plain quoted strings
	// and numbers should render as themselves, not as a colored literal.
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

// colorizeJSONTokens walks an already pretty-printed JSON string and
// wraps each token in its lipgloss style. Keys are distinguished from
// string values by peeking past trailing whitespace for a ':'.
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
			// Peek past whitespace for ':' to distinguish keys.
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
