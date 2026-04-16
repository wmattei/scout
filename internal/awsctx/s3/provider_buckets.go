package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/format"
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

func (bucketProvider) ARN(r core.Resource, _ map[string]string) string {
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

func (bucketProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-uri", Label: "Copy URI"},
		{ID: "copy-arn", Label: "Copy ARN"},
	}
}

// ResolveDetails fires the parallel DescribeBucket helper (4 Get*
// calls) and stores the results in the lazy map. Also grabs
// CreatedAt from r.Meta if ListBuckets captured it.
func (bucketProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := DescribeBucket(ctx, ac, r.Key)
	if err != nil {
		return nil, err
	}
	out := map[string]string{
		"versioning":   d.Versioning,
		"encryption":   d.Encryption,
		"publicAccess": d.PublicAccess,
	}
	// Pass through CreatedAt from the adapter — it was captured at
	// ListBuckets time and is authoritative without an extra call.
	if ts := r.Meta["createdAt"]; ts != "" {
		out["createdAt"] = ts
	}
	if len(d.Tags) > 0 {
		if b, err := json.Marshal(d.Tags); err == nil {
			out["tags"] = string(b)
		}
	}
	return out, nil
}

// DetailRows renders the bucket details panel: region, created,
// versioning, encryption, public access, and first 5 tags.
func (bucketProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil || lazy["versioning"] == "" {
		return nil
	}

	rows := []services.DetailRow{
		{Label: "Region", Value: r.Meta["region"]},
	}

	if ts := lazy["createdAt"]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Created", Value: styleDim.Render(format.TimeAge(ts))})
	}

	rows = append(rows,
		services.DetailRow{Label: "Versioning", Value: colorVersioning(lazy["versioning"])},
		services.DetailRow{Label: "Encryption", Value: lazy["encryption"]},
		services.DetailRow{Label: "Public", Value: colorPublicAccess(lazy["publicAccess"])},
	)

	if tags := format.DecodeJSONSlice(lazy["tags"]); len(tags) > 0 {
		rows = append(rows, services.DetailRow{}) // spacer
		rows = append(rows, services.DetailRow{Value: styleHeader.Render("Tags")})
		for _, t := range tags {
			rows = append(rows, services.DetailRow{Label: "", Value: styleDim.Render(t)})
		}
	}

	return rows
}

func colorVersioning(v string) string {
	switch v {
	case "Enabled":
		return styleGood.Render(v)
	case "Suspended":
		return styleWarn.Render(v)
	default:
		return styleDim.Render(v)
	}
}

func colorPublicAccess(v string) string {
	switch v {
	case "All blocked":
		return styleGood.Render(v)
	case "Partially open":
		return styleBad.Render(v)
	default:
		return styleWarn.Render(v)
	}
}

// Shared styles for color-coded signals. Mirrors the palette in
// provider_services.go — duplicated so each file is self-contained.
var (
	styleGood   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#005F00", Dark: "#5FFF5F"})
	styleWarn   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleBad    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#870000", Dark: "#FF5F5F"})
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
)
