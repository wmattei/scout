package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/services"
)

// Register adds this package's providers to the services registry.
// Called from cmd/scout at startup for commands that need AWS access.
func Register() { services.Register(&documentProvider{}) }

// documentProvider implements services.Provider for SSM Automation
// documents. Runbook executions are NOT a separate resource type in
// scout — they live inside a document's Details view (Events zone +
// Phase 2 execution-details mode).
type documentProvider struct {
	services.BaseProvider
}

func (documentProvider) Type() core.ResourceType { return core.RTypeSSMAutomationDocument }
func (documentProvider) Aliases() []string {
	return []string{"auto", "automation", "runbook"}
}
func (documentProvider) TagLabel() string { return "AUTO" }

func (documentProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#5F00AF", Dark: "#AF87FF"})
}

func (documentProvider) SortPriority() int { return 6 }
func (documentProvider) IsTopLevel() bool  { return true }

// ARN is empty for documents — DescribeDocument doesn't return one
// and constructing it requires the account ID which providers don't
// have. Copy ARN will surface an error toast when invoked.
func (documentProvider) ARN(_ core.Resource, _ map[string]string) string {
	return ""
}

func (documentProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/systems-manager/documents/%s/description?region=%s",
		region, url.PathEscape(r.Key), region)
}

// RenderMeta surfaces the owner (Amazon / Self / <account>) and doc
// type tag so the list row at a glance shows who owns the runbook.
func (documentProvider) RenderMeta(r core.Resource) string {
	owner := r.Meta[MetaOwner]
	if owner == "" {
		return ""
	}
	return owner
}

func (documentProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListDocuments(ctx, ac, opts)
}

func (documentProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "run-automation", Label: "Run"},
		{ID: "open", Label: "Open in Browser"},
		{ID: "copy-arn", Label: "Copy ARN"},
	}
}

// AlwaysRefresh — DescribeDocument is cheap and the Events zone's
// execution list changes after every Run. Re-fetch on every entry.
func (documentProvider) AlwaysRefresh() bool { return true }

func (documentProvider) PollingInterval() time.Duration { return 0 }

func (documentProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := DescribeDocument(ctx, ac, r.Key)
	if err != nil {
		return nil, err
	}

	out := map[string]string{
		"name":            d.Name,
		MetaDescription:   d.Description,
		MetaOwner:         d.Owner,
		MetaDocumentType:  d.DocumentType,
		MetaVersionName:   d.VersionName,
		MetaLatestVersion: d.LatestVersion,
		MetaTargetType:    d.TargetType,
	}
	if len(d.Parameters) > 0 {
		if b, err := json.Marshal(d.Parameters); err == nil {
			out[MetaParameters] = string(b)
		}
	}
	if len(d.PlatformTypes) > 0 {
		out[MetaPlatformTypes] = strings.Join(d.PlatformTypes, ",")
	}

	// Best-effort executions fetch — a failure here shouldn't
	// invalidate the whole document resolve. An empty slice just
	// means the Events zone collapses to nothing.
	if execs, execErr := ListExecutions(ctx, ac, r.Key, 10); execErr == nil && len(execs) > 0 {
		if b, err := json.Marshal(execs); err == nil {
			out[MetaExecutions] = string(b)
		}
	}

	return out, nil
}

// DetailRows assembles the zoned details body for an Automation
// document: latest-execution badge in Status, core metadata in
// Metadata, colorized content in Value, and recent executions in
// Events.
func (documentProvider) DetailRows(_ core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil {
		return nil
	}

	var execs []ExecutionInfo
	if s := lazy[MetaExecutions]; s != "" {
		_ = json.Unmarshal([]byte(s), &execs)
	}

	rows := []services.DetailRow{}

	// Action error — if the last attempted Run surfaced an AWS API
	// error, persist it at the top of Status so the user sees the
	// reason without having to catch the transient toast.
	if errMsg := lazy["actionError"]; errMsg != "" {
		rows = append(rows, services.DetailRow{
			Zone:  services.ZoneStatus,
			Label: "LastRun",
			Value: styleErr.Render("✗ "+errMsg),
		})
	}

	// Status — latest execution state (or "never run").
	if len(execs) > 0 {
		latest := execs[0]
		rows = append(rows, services.DetailRow{
			Zone:  services.ZoneStatus,
			Label: "Latest",
			Value: styleDim.Render("Latest ") + executionStatusBadge(latest.Status) +
				"  " + styleDim.Render(humanTimeAgo(latest.StartTime)),
		})
	} else {
		rows = append(rows, services.DetailRow{
			Zone:  services.ZoneStatus,
			Value: styleDim.Render("never run"),
		})
	}

	// Metadata rows.
	if v := lazy[MetaOwner]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Owner", Value: v})
	}
	if v := lazy[MetaDocumentType]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Type", Value: v})
	}
	if v := lazy[MetaLatestVersion]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Version", Value: v})
	}
	if v := lazy[MetaVersionName]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Name", Value: v})
	}
	if v := lazy[MetaTargetType]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Target", Value: v})
	}
	if v := lazy[MetaPlatformTypes]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Platform", Value: v})
	}
	if s := lazy[MetaParameters]; s != "" {
		var params []ParameterInfo
		if err := json.Unmarshal([]byte(s), &params); err == nil && len(params) > 0 {
			lines := make([]string, 0, len(params))
			for _, p := range params {
				line := p.Name
				if p.Type != "" {
					line += " " + styleDim.Render("("+p.Type+")")
				}
				if p.DefaultValue != "" {
					line += " " + styleDim.Render("= "+p.DefaultValue)
				}
				lines = append(lines, line)
			}
			rows = append(rows, services.DetailRow{
				Label: "Params",
				Value: strings.Join(lines, "\n"),
			})
		}
	}
	if v := lazy[MetaDescription]; v != "" {
		rows = append(rows, services.DetailRow{Label: "Desc", Value: wrapText(v, 60)})
	}

	// Events — recent executions, or a placeholder so the zone
	// always appears (collapsing it on "no executions" hides the
	// context the user's looking for). Each execution row is marked
	// Selectable with the execution ID as the ActivationID so the
	// Details handler can Tab into the zone and drill into the
	// runbook execution details mode on Enter.
	if len(execs) == 0 {
		rows = append(rows, services.DetailRow{
			Zone:  services.ZoneEvents,
			Value: styleDim.Render("no executions yet"),
		})
	} else {
		for _, e := range execs {
			startStr := e.StartTime.Format("2006-01-02 15:04")
			line := executionStatusIcon(e.Status) + " " + styleDim.Render(startStr) +
				"  " + truncateID(e.ExecutionID) + "  " + executionStatusText(e.Status)
			if !e.EndTime.IsZero() {
				line += "  " + styleDim.Render(humanDuration(e.EndTime.Sub(e.StartTime)))
			} else if e.Status == "InProgress" || e.Status == "Pending" || e.Status == "Waiting" {
				line += "  " + styleDim.Render(humanDuration(time.Since(e.StartTime))+"…")
			}
			rows = append(rows, services.DetailRow{
				Zone:         services.ZoneEvents,
				Value:        line,
				Selectable:   true,
				ActivationID: e.ExecutionID,
			})
		}
	}

	return rows
}
