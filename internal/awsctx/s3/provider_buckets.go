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

func init() { services.Register(&bucketProvider{}) }

// bucketProvider implements services.Provider for the
// core.RTypeBucket type. Wraps the existing ListBuckets adapter and
// owns the bucket-row presentation: blue tag, region in the meta
// column, https console URL, s3:// URI for Copy URI, and a Tab
// completion that drops a trailing "/" so the user immediately
// drills into the bucket's S3 drill-in scope.
type bucketProvider struct {
	services.BaseProvider
}

func (bucketProvider) Type() core.ResourceType { return core.RTypeBucket }
func (bucketProvider) Aliases() []string       { return []string{"s3", "buckets"} }
func (bucketProvider) TagLabel() string        { return "S3" }

func (bucketProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
}

func (bucketProvider) SortPriority() int { return 0 }
func (bucketProvider) IsTopLevel() bool  { return true }

func (bucketProvider) ARN(r core.Resource) string {
	return fmt.Sprintf("arn:aws:s3:::%s", r.Key)
}

func (bucketProvider) URI(r core.Resource) (string, bool) {
	return fmt.Sprintf("s3://%s", r.Key), true
}

func (bucketProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s",
		url.PathEscape(r.Key), region)
}

func (bucketProvider) RenderMeta(r core.Resource) string {
	return r.Meta["region"]
}

func (bucketProvider) TabComplete(_ search.Scope, r core.Resource) string {
	// Drop a trailing slash so the next recompute parses the input
	// as an S3 drill-in scope rooted at this bucket.
	return r.Key + "/"
}

func (bucketProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListBuckets(ctx, ac, opts)
}
