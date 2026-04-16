package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&folderProvider{}) }

// folderProvider implements services.Provider for core.RTypeFolder.
// Folders are virtual S3 prefixes — they only exist inside a
// drilled-in bucket scope, so this provider is NOT top-level
// (IsTopLevel returns false) and ListAll returns (nil, nil) —
// folders are populated by ListAtPrefix in the scoped search code
// path, not here.
type folderProvider struct {
	services.BaseProvider
}

func (folderProvider) Type() core.ResourceType { return core.RTypeFolder }
func (folderProvider) Aliases() []string       { return nil }
func (folderProvider) TagLabel() string        { return "DIR" }

func (folderProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#008787", Dark: "#5FFFFF"})
}

func (folderProvider) SortPriority() int { return 100 }
func (folderProvider) IsTopLevel() bool  { return false }

func (folderProvider) ARN(r core.Resource, _ map[string]string) string {
	return fmt.Sprintf("arn:aws:s3:::%s/%s", r.Meta["bucket"], r.Key)
}

func (folderProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key), true
}

func (folderProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	bucket := r.Meta["bucket"]
	prefix := r.Key
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s&prefix=%s&showversions=false",
		url.PathEscape(bucket), region, url.QueryEscape(prefix))
}

func (folderProvider) RenderMeta(r core.Resource) string {
	if ts, ok := r.Meta["mtime"]; ok && ts != "" {
		return formatUnixTimeOrEmpty(ts)
	}
	return ""
}

func (folderProvider) TabComplete(scope search.Scope, r core.Resource) string {
	// row.Key is the full key relative to the bucket (e.g.
	// "logs/2026/01/"). Reconstruct "<bucket>/<key>" so the next
	// recompute parses the input as a deeper S3 drill-in scope.
	return scope.Bucket + "/" + r.Key
}

func (folderProvider) ListAll(context.Context, *awsctx.Context, awsctx.ListOptions) ([]core.Resource, error) {
	// Folders are populated by the scoped ListAtPrefix path inside
	// the TUI. There is no top-level "list every folder under every
	// bucket" semantics, so return (nil, nil) and let the UI never
	// call this for folders.
	return nil, nil
}

func (folderProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-uri", Label: "Copy URI"},
		{ID: "copy-arn", Label: "Copy ARN"},
	}
}

// formatUnixTimeOrEmpty mirrors the helper in internal/tui/results.go.
// Duplicated here (3 lines) so the provider doesn't need to import
// the tui package, which would create a cycle. If formatting becomes
// more elaborate we promote this to a shared helper.
func formatUnixTimeOrEmpty(s string) string {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return ""
	}
	return formatUnixTimeFmt(n)
}
