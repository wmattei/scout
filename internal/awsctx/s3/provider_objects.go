package s3

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/format"
	"github.com/wagnermattei/better-aws-cli/internal/search"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&objectProvider{}) }

// objectProvider implements services.Provider for core.RTypeObject.
// Like folderProvider, objects are scoped-only — they're discovered
// by drilling into a bucket and then a folder, so IsTopLevel is
// false and ListAll returns (nil, nil). The Tab completion drops
// the trailing slash because objects are leaves.
type objectProvider struct {
	services.BaseProvider
}

func (objectProvider) Type() core.ResourceType { return core.RTypeObject }
func (objectProvider) Aliases() []string       { return nil }
func (objectProvider) TagLabel() string        { return "OBJ" }

func (objectProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#585858", Dark: "#A8A8A8"})
}

func (objectProvider) SortPriority() int { return 200 }
func (objectProvider) IsTopLevel() bool  { return false }

func (objectProvider) ARN(r core.Resource, _ map[string]string) string {
	return fmt.Sprintf("arn:aws:s3:::%s/%s", r.Meta["bucket"], r.Key)
}

func (objectProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key), true
}

func (objectProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	bucket := r.Meta["bucket"]
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/object/%s?region=%s&prefix=%s",
		url.PathEscape(bucket), region, url.QueryEscape(r.Key))
}

func (objectProvider) RenderMeta(r core.Resource) string {
	var parts []string
	if s, ok := r.Meta["size"]; ok && s != "" {
		if b := format.Bytes(s); b != "" {
			parts = append(parts, b)
		}
	}
	if ts, ok := r.Meta["mtime"]; ok && ts != "" {
		parts = append(parts, formatUnixTimeOrEmpty(ts))
	}
	return strings.Join(parts, "  ")
}

func (objectProvider) TabComplete(scope search.Scope, r core.Resource) string {
	// Object keys never get a trailing slash — they're leaves.
	return scope.Bucket + "/" + r.Key
}

func (objectProvider) ListAll(context.Context, *awsctx.Context, awsctx.ListOptions) ([]core.Resource, error) {
	return nil, nil
}

func (objectProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-uri", Label: "Copy URI"},
		{ID: "copy-arn", Label: "Copy ARN"},
		{ID: "download", Label: "Download"},
		{ID: "preview", Label: "Preview"},
	}
}

