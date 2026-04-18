package lambda

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/format"
	"github.com/wmattei/scout/internal/services"
)

func init() { services.Register(&lambdaFunctionProvider{}) }

// lambdaFunctionProvider implements services.Provider for Lambda functions.
// The ARN is stored as Key (consistent with ECS services). DisplayName is
// the bare function name.
type lambdaFunctionProvider struct {
	services.BaseProvider
}

func (lambdaFunctionProvider) Type() core.ResourceType { return core.RTypeLambdaFunction }
func (lambdaFunctionProvider) Aliases() []string {
	return []string{"lambda", "fn", "functions"}
}
func (lambdaFunctionProvider) TagLabel() string { return "FN" }

func (lambdaFunctionProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#AF87FF"})
}

func (lambdaFunctionProvider) SortPriority() int { return 3 }
func (lambdaFunctionProvider) IsTopLevel() bool  { return true }

// ARN returns the function ARN which is stored as r.Key.
func (lambdaFunctionProvider) ARN(r core.Resource, _ map[string]string) string {
	return r.Key
}

// ConsoleURL builds the Lambda console deep-link for this function.
func (lambdaFunctionProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/lambda/home?region=%s#/functions/%s",
		region, region, r.DisplayName)
}

// RenderMeta shows the runtime in the right-aligned meta column.
func (lambdaFunctionProvider) RenderMeta(r core.Resource) string {
	return r.Meta[MetaRuntime]
}

// ListAll delegates to ListFunctions.
func (lambdaFunctionProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListFunctions(ctx, ac, opts)
}

func (lambdaFunctionProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-arn", Label: "Copy ARN"},
		{ID: "tail-logs", Label: "Tail Logs"},
		{ID: "run", Label: "Run"},
	}
}

// AlwaysRefresh — Lambda function configuration is relatively static so we
// cache results per session.
func (lambdaFunctionProvider) AlwaysRefresh() bool { return false }

// PollingInterval — no live-state polling needed.
func (lambdaFunctionProvider) PollingInterval() time.Duration { return 0 }

// ResolveDetails calls GetFunction and stores all fields in the lazy map.
// Layers are JSON-encoded (slice); tags are JSON-encoded (map).
func (lambdaFunctionProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := GetFunction(ctx, ac, r.DisplayName)
	if err != nil {
		return nil, err
	}
	out := map[string]string{
		MetaRuntime:      d.Runtime,
		MetaMemorySize:   fmt.Sprintf("%d", d.MemorySize),
		MetaTimeout:      fmt.Sprintf("%d", d.Timeout),
		MetaLastModified: d.LastModified,
		MetaHandler:      d.Handler,
		MetaCodeSize:     fmt.Sprintf("%d", d.CodeSize),
		MetaDescription:  d.Description,
	}
	if layersJSON := marshalStringSlice(d.Layers); layersJSON != "" {
		out["layers"] = layersJSON
	}
	if tagsJSON := marshalStringMap(d.Tags); tagsJSON != "" {
		out["tags"] = tagsJSON
	}
	return out, nil
}

// LogGroup returns the Lambda function's default CloudWatch log group.
// AWS always places Lambda logs at /aws/lambda/<functionName>.
func (lambdaFunctionProvider) LogGroup(r core.Resource, _ map[string]string) string {
	return "/aws/lambda/" + r.DisplayName
}

// DetailRows builds the Details panel body for a Lambda function. Returns nil
// while lazy data is still in-flight so the "resolving details…" placeholder
// is shown.
func (lambdaFunctionProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil || lazy[MetaRuntime] == "" {
		return nil
	}

	rows := []services.DetailRow{
		{Label: "Runtime", Value: lazy[MetaRuntime]},
		{Label: "Handler", Value: lazy[MetaHandler]},
	}

	if mem := lazy[MetaMemorySize]; mem != "" {
		rows = append(rows, services.DetailRow{Label: "Memory", Value: mem + " MB"})
	}
	if tmo := lazy[MetaTimeout]; tmo != "" {
		rows = append(rows, services.DetailRow{Label: "Timeout", Value: tmo + "s"})
	}

	if cs := lazy[MetaCodeSize]; cs != "" {
		var sz int64
		fmt.Sscanf(cs, "%d", &sz)
		rows = append(rows, services.DetailRow{Label: "Code size", Value: humanBytes(sz)})
	}

	if lm := lazy[MetaLastModified]; lm != "" {
		rows = append(rows, services.DetailRow{Label: "Modified", Value: formatLastModified(lm)})
	}

	if desc := lazy[MetaDescription]; desc != "" {
		rows = append(rows, services.DetailRow{Label: "Desc", Value: desc})
	}

	// Log group — convenience row so users know where logs live.
	logGroup := "/aws/lambda/" + r.DisplayName
	rows = append(rows, services.DetailRow{
		Label:          "Log group",
		Value:          logGroup,
		Clickable:      true,
		ClipboardValue: logGroup,
	})

	// Layers.
	if layers := format.DecodeJSONSlice(lazy["layers"]); len(layers) > 0 {
		rows = append(rows, services.DetailRow{})
		rows = append(rows, services.DetailRow{Value: styleHeader.Render(fmt.Sprintf("Layers (%d)", len(layers)))})
		for _, l := range layers {
			rows = append(rows, services.DetailRow{Value: styleDim.Render(l)})
		}
	}

	// Tags — first 5, sorted by key for stable rendering across polls.
	if tags := format.DecodeStringMap(lazy["tags"]); len(tags) > 0 {
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows = append(rows, services.DetailRow{})
		rows = append(rows, services.DetailRow{Value: styleHeader.Render("Tags")})
		for i, k := range keys {
			if i >= 5 {
				break
			}
			rows = append(rows, services.DetailRow{Value: styleDim.Render(k + "=" + tags[k])})
		}
	}

	return rows
}

// --- helpers ---------------------------------------------------------------

// formatLastModified parses the Lambda lastModified string (RFC3339-like)
// and returns a human-readable "YYYY-MM-DD HH:MM  (Xd ago)" string.
// Lambda returns timestamps in the form "2026-04-01T12:34:56.789+0000".
func formatLastModified(s string) string {
	if s == "" {
		return ""
	}
	// Try several common formats that Lambda uses.
	formats := []string{
		"2006-01-02T15:04:05.999-0700",
		time.RFC3339,
		"2006-01-02T15:04:05.999Z07:00",
	}
	var t time.Time
	for _, f := range formats {
		if parsed, err := time.Parse(f, s); err == nil {
			t = parsed.Local()
			break
		}
	}
	if t.IsZero() {
		return s
	}
	age := time.Since(t)
	return fmt.Sprintf("%s  %s", t.Format("2006-01-02 15:04"), styleDim.Render("("+format.HumanDuration(age)+" ago)"))
}

// humanBytes formats a byte count as "1.2 MB", "345 KB", etc.
func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// lastARNSegment extracts the trailing path segment of an ARN.
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// Shared styles for color-coded details panel output.
var (
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875FAF", Dark: "#AF87FF"})
)

// ensure lastARNSegment is used (it may be needed for future helpers)
var _ = lastARNSegment
