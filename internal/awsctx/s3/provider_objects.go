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
		parts = append(parts, formatBytesOrEmpty(s))
	}
	if ts, ok := r.Meta["mtime"]; ok && ts != "" {
		parts = append(parts, formatUnixTimeOrEmpty(ts))
	}
	return joinNonEmpty(parts, "  ")
}

func (objectProvider) TabComplete(scope search.Scope, r core.Resource) string {
	// Object keys never get a trailing slash — they're leaves.
	return scope.Bucket + "/" + r.Key
}

func (objectProvider) ListAll(context.Context, *awsctx.Context, awsctx.ListOptions) ([]core.Resource, error) {
	return nil, nil
}

// formatBytesOrEmpty mirrors the helper in internal/tui/results.go.
// Duplicated for the same reason as formatUnixTimeOrEmpty in
// provider_folders.go: providers must not import the tui package.
func formatBytesOrEmpty(s string) string {
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

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
