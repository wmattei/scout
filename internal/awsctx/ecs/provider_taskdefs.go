package ecs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/format"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

func init() { services.Register(&ecsTaskDefProvider{}) }

// ecsTaskDefProvider implements services.Provider for the
// core.RTypeEcsTaskDefFamily type. Owns the yellow tag and the
// lazy DescribeTaskDefinition flow that resolves a family name
// into a concrete revision ARN + container log groups.
type ecsTaskDefProvider struct {
	services.BaseProvider
}

func (ecsTaskDefProvider) Type() core.ResourceType { return core.RTypeEcsTaskDefFamily }
func (ecsTaskDefProvider) Aliases() []string {
	return []string{"td", "task", "taskdef"}
}
func (ecsTaskDefProvider) TagLabel() string { return "TASK" }

func (ecsTaskDefProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#AF8700", Dark: "#FFD75F"})
}

func (ecsTaskDefProvider) SortPriority() int { return 2 }
func (ecsTaskDefProvider) IsTopLevel() bool  { return true }

// ARN returns the resolved revision ARN if available in the lazy
// map, otherwise a wildcard family-only pseudo-ARN. The Details
// view renders "…resolving" while the lazy lookup is in flight; by
// the time Copy ARN runs, the lazy map is populated and we return
// the real revision ARN.
func (p ecsTaskDefProvider) ARN(r core.Resource, lazy map[string]string) string {
	if lazy != nil {
		if arn := lazy["familyArn"]; arn != "" {
			return arn
		}
	}
	return fmt.Sprintf("arn:aws:ecs:*:*:task-definition/%s", r.Key)
}

func (ecsTaskDefProvider) ConsoleURL(r core.Resource, region string, lazy map[string]string) string {
	family := r.Key
	rev := ""
	if lazy != nil {
		if arn := lazy["familyArn"]; arn != "" {
			if i := strings.LastIndexByte(arn, ':'); i > 0 {
				rev = arn[i+1:]
			}
		}
	}
	if rev != "" {
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s/%s?region=%s",
			region, url.PathEscape(family), url.PathEscape(rev), region)
	}
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s?region=%s",
		region, url.PathEscape(family), region)
}

func (ecsTaskDefProvider) RenderMeta(_ core.Resource) string { return "" }

// PollingInterval — task def details auto-refresh every 10s so the
// running-task count stays current.
func (ecsTaskDefProvider) PollingInterval() time.Duration { return 10 * time.Second }

// AlwaysRefresh — always fetch fresh state on every Details entry.
func (ecsTaskDefProvider) AlwaysRefresh() bool { return true }

func (ecsTaskDefProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListTaskDefFamilies(ctx, ac, opts)
}

// ResolveDetails fires DescribeTaskDefinition for the family and
// CountRunningTasks to get a live task count. Both run sequentially
// (the task count depends on the family name, which is already in
// r.Key, so no dependency on the Describe response).
func (ecsTaskDefProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := DescribeFamily(ctx, ac, r.Key)
	if err != nil || d == nil {
		return nil, err
	}
	out := map[string]string{
		"familyArn":   d.ARN,
		"revision":    fmt.Sprintf("%d", d.Revision),
		"cpu":         d.CPU,
		"memory":      d.Memory,
		"networkMode": d.NetworkMode,
		"taskRole":    d.TaskRoleArn,
		"execRole":    d.ExecutionRoleArn,
	}
	if len(d.LogGroups) > 0 {
		out["logGroup"] = d.LogGroups[0]
	}
	if len(d.RequiresCompatibilities) > 0 {
		out["compatibilities"] = strings.Join(d.RequiresCompatibilities, ", ")
	}
	if len(d.ContainerImages) > 0 {
		if b, err := json.Marshal(d.ContainerImages); err == nil {
			out["containers"] = string(b)
		}
	}

	// Running task count — best-effort, non-fatal.
	if count, err := CountRunningTasks(ctx, ac, r.Key); err == nil {
		out["runningTasks"] = fmt.Sprintf("%d", count)
	}

	return out, nil
}

func (ecsTaskDefProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}

// DetailRows renders the task-definition details panel: revision,
// CPU/memory, network mode, compatibilities, containers + images,
// IAM roles, and log groups.
func (ecsTaskDefProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil || lazy["revision"] == "" {
		return nil
	}

	rows := []services.DetailRow{
		{Label: "Revision", Value: lazy["revision"]},
	}

	// Running task count.
	if rt := lazy["runningTasks"]; rt != "" {
		label := rt + " running"
		var n int
		fmt.Sscanf(rt, "%d", &n)
		if n > 0 {
			rows = append(rows, services.DetailRow{Label: "Tasks", Value: styleGood.Render(label)})
		} else {
			rows = append(rows, services.DetailRow{Label: "Tasks", Value: styleDim.Render(label)})
		}
	}

	// CPU / Memory on one row.
	if cpu := lazy["cpu"]; cpu != "" {
		mem := lazy["memory"]
		val := cpu + " CPU"
		if mem != "" {
			val = val + "  ·  " + mem + " MiB"
		}
		rows = append(rows, services.DetailRow{Label: "Resources", Value: val})
	}

	if nm := lazy["networkMode"]; nm != "" {
		rows = append(rows, services.DetailRow{Label: "Network", Value: nm})
	}
	if c := lazy["compatibilities"]; c != "" {
		rows = append(rows, services.DetailRow{Label: "Platform", Value: c})
	}

	// IAM roles — show just the role name (last segment of the ARN)
	// for readability, or the full ARN if it doesn't parse.
	if role := lazy["taskRole"]; role != "" {
		rows = append(rows, services.DetailRow{Label: "Task role", Value: shortRole(role)})
	}
	if role := lazy["execRole"]; role != "" {
		rows = append(rows, services.DetailRow{Label: "Exec role", Value: shortRole(role)})
	}

	// Log group.
	if lg := lazy["logGroup"]; lg != "" {
		rows = append(rows, services.DetailRow{Label: "Log", Value: lg})
	}

	// Containers + images as a sub-section.
	if containers := format.DecodeJSONSlice(lazy["containers"]); len(containers) > 0 {
		rows = append(rows, services.DetailRow{}) // spacer
		if len(containers) <= 3 {
			rows = append(rows, services.DetailRow{Value: styleHeader.Render("Containers")})
			for _, c := range containers {
				rows = append(rows, services.DetailRow{Label: "", Value: styleDim.Render(c)})
			}
		} else {
			rows = append(rows, services.DetailRow{
				Value: styleHeader.Render(fmt.Sprintf("Containers (%d)", len(containers))),
			})
			for _, c := range containers[:3] {
				rows = append(rows, services.DetailRow{Label: "", Value: styleDim.Render(c)})
			}
			rows = append(rows, services.DetailRow{
				Label: "",
				Value: styleDim.Render(fmt.Sprintf("… and %d more", len(containers)-3)),
			})
		}
	}

	return rows
}

// shortRole extracts the role name from an IAM role ARN.
// "arn:aws:iam::123:role/MyRole" → "MyRole".
func shortRole(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// styleHeader and styleDim are declared in provider_services.go
// (same package) — no redeclaration needed here.
