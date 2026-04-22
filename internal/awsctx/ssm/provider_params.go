package ssm

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/format"
	"github.com/wmattei/scout/internal/services"
)

// Register adds this package's providers to the services registry.
// Called from cmd/scout at startup for commands that need AWS access.
func Register() { services.Register(&ssmParameterProvider{}) }

// ssmParameterProvider implements services.Provider for SSM Parameter Store
// parameters. Parameter names are their own keys (can contain "/" for
// hierarchy, but parameters are flat resources — not drillable like S3).
type ssmParameterProvider struct {
	services.BaseProvider
}

func (ssmParameterProvider) Type() core.ResourceType { return core.RTypeSSMParameter }
func (ssmParameterProvider) Aliases() []string {
	return []string{"ssm", "param", "params", "parameter"}
}
func (ssmParameterProvider) TagLabel() string { return "SSM" }

func (ssmParameterProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#008787", Dark: "#00D7D7"})
}

func (ssmParameterProvider) SortPriority() int { return 4 }
func (ssmParameterProvider) IsTopLevel() bool  { return true }

// ARN returns the resolved ARN from the lazy map (populated by GetParameter),
// or falls back to the empty string. The ARN is only available after
// ResolveDetails has run.
func (ssmParameterProvider) ARN(_ core.Resource, lazy map[string]string) string {
	if lazy != nil {
		return lazy["arn"]
	}
	return ""
}

// ConsoleURL builds the Systems Manager console deep-link for the parameter.
// The parameter name may contain "/" so we URL-encode it.
func (ssmParameterProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	encoded := url.PathEscape(r.Key)
	return fmt.Sprintf("https://%s.console.aws.amazon.com/systems-manager/parameters/%s/description?region=%s",
		region, encoded, region)
}

// RenderMeta shows the parameter type (String / SecureString / StringList).
func (ssmParameterProvider) RenderMeta(r core.Resource) string {
	return r.Meta[MetaType]
}

// ListAll delegates to ListParameters.
func (ssmParameterProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListParameters(ctx, ac, opts)
}

func (ssmParameterProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-arn", Label: "Copy ARN"},
		{ID: "copy-value", Label: "Copy Value"},
		{ID: "update-value", Label: "Update Value"},
	}
}

// AlwaysRefresh — SSM parameters may be updated by automation so we re-fetch
// on every Details entry to show the fresh value.
func (ssmParameterProvider) AlwaysRefresh() bool { return true }

// PollingInterval — no continuous polling; the value doesn't change that fast.
func (ssmParameterProvider) PollingInterval() time.Duration { return 0 }

// ResolveDetails calls GetParameter and stores all fields in the lazy map.
func (ssmParameterProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := GetParameter(ctx, ac, r.Key)
	if err != nil {
		return nil, err
	}
	out := map[string]string{
		"name":            d.Name,
		MetaType:          d.Type,
		"value":           d.Value,
		MetaVersion:       fmt.Sprintf("%d", d.Version),
		"dataType":        d.DataType,
		"arn":             d.ARN,
	}
	if !d.LastModified.IsZero() {
		out[MetaLastModified] = fmt.Sprintf("%d", d.LastModified.Unix())
	}
	// Propagate description from Meta if GetParameter didn't return one.
	if out[MetaDescription] == "" {
		out[MetaDescription] = r.Meta[MetaDescription]
	}
	return out, nil
}

// DetailRows builds the Details panel body for an SSM parameter.
// Returns nil while lazy data is still in-flight.
func (ssmParameterProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil || lazy[MetaType] == "" {
		return nil
	}

	rows := []services.DetailRow{
		{Label: "Type", Value: colorParamType(lazy[MetaType])},
	}

	// Value — truncate very long values for display but copy the
	// full raw value on click.
	rawValue := lazy["value"]
	value := rawValue
	if len(value) > 200 {
		value = value[:200] + styleDim.Render("  … (truncated)")
	}
	rows = append(rows, services.DetailRow{
		Label:          "Value",
		Value:          value,
		Clickable:      true,
		ClipboardValue: rawValue,
	})

	if v := lazy[MetaVersion]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Version", Value: v})
	}

	if ts := lazy[MetaLastModified]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Modified", Value: styleDim.Render(format.TimeAge(ts))})
	}

	if dt := lazy["dataType"]; dt != "" {
		rows = append(rows, services.DetailRow{Label: "Data type", Value: dt})
	}

	if desc := lazy[MetaDescription]; desc != "" {
		rows = append(rows, services.DetailRow{Label: "Desc", Value: desc})
	}

	return rows
}

// --- helpers ---------------------------------------------------------------

// colorParamType color-codes the parameter type: SecureString in yellow,
// others in the default color.
func colorParamType(t string) string {
	switch t {
	case "SecureString":
		return styleWarn.Render(t)
	default:
		return t
	}
}

// Shared styles for color-coded signals.
var (
	styleWarn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleDim  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
)
