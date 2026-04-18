package ecs

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
	"github.com/wmattei/scout/internal/format"
	"github.com/wmattei/scout/internal/services"
)

func init() { services.Register(&ecsServiceProvider{}) }

// ecsServiceProvider implements services.Provider for ECS services.
// Owns the orange tag, the cluster-name meta column, the ECS console
// service-health URL, and the lazy DescribeServices resolution that
// the Tail Logs action depends on.
type ecsServiceProvider struct {
	services.BaseProvider
}

func (ecsServiceProvider) Type() core.ResourceType { return core.RTypeEcsService }
func (ecsServiceProvider) Aliases() []string {
	return []string{"ecs", "svc", "services"}
}
func (ecsServiceProvider) TagLabel() string { return "ECS" }

func (ecsServiceProvider) TagStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#AF5F00", Dark: "#FFAF5F"})
}

func (ecsServiceProvider) SortPriority() int { return 1 }
func (ecsServiceProvider) IsTopLevel() bool  { return true }

func (ecsServiceProvider) ARN(r core.Resource, _ map[string]string) string {
	// r.Key is the full service ARN — that's what we want to show
	// regardless of what lazyDetails happens to carry. The task-def
	// family ARN (from DescribeFamily) is a separate row in
	// DetailRows, not the service ARN.
	return r.Key
}

func (ecsServiceProvider) ConsoleURL(r core.Resource, region string, _ map[string]string) string {
	cluster := r.Meta[MetaCluster]
	svcName := lastARNSegment(r.Key)
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
		region, url.PathEscape(cluster), url.PathEscape(svcName), region)
}

func (ecsServiceProvider) RenderMeta(r core.Resource) string {
	return r.Meta[MetaCluster]
}

func (ecsServiceProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListServices(ctx, ac, opts)
}

func (ecsServiceProvider) Actions() []services.ActionDef {
	return []services.ActionDef{
		{ID: "open", Label: "Open in Browser"},
		{ID: "force-deploy", Label: "Force new Deployment"},
		{ID: "tail-logs", Label: "Tail Logs"},
	}
}

// PollingInterval — ECS service details auto-refresh every 10s while
// the user is looking at the Details view so running counts,
// deployment state, and recent events stay current.
func (ecsServiceProvider) PollingInterval() time.Duration { return 10 * time.Second }

// AlwaysRefresh — ECS service details are time-sensitive (running
// count, deployment rollout state, recent events). The Details Enter
// handler re-fires ResolveDetails on every entry so the user sees
// fresh numbers every time.
func (ecsServiceProvider) AlwaysRefresh() bool { return true }

// ResolveDetails fires a fresh DescribeServices for the selected
// service plus a DescribeFamily for the current task-def revision
// (needed for log groups and the "<family>:<rev>" row). The result
// lands in m.lazyDetails keyed by (RTypeEcsService, r.Key).
//
// Multi-value fields (events, target groups) are JSON-encoded into
// string slots because the shared lazyDetails map is string-valued.
// DetailRows decodes them on render.
func (ecsServiceProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	clusterArn := r.Meta[MetaClusterArn]
	if clusterArn == "" {
		return nil, nil
	}

	d, err := DescribeService(ctx, ac, clusterArn, r.Key)
	if err != nil {
		return nil, err
	}
	out := map[string]string{
		"status":                   d.Status,
		"desiredCount":             fmt.Sprintf("%d", d.DesiredCount),
		"runningCount":             fmt.Sprintf("%d", d.RunningCount),
		"pendingCount":             fmt.Sprintf("%d", d.PendingCount),
		MetaLaunchType:             d.LaunchType,
		"platformVersion":          d.PlatformVersion,
		"taskDefinition":           d.TaskDefinition,
		"deploymentRolloutState":   d.DeploymentRolloutState,
		"deploymentRolloutReason":  d.DeploymentRolloutStateReason,
		"circuitBreakerEnabled":    boolString(d.CircuitBreakerEnabled),
		"circuitBreakerRollback":   boolString(d.CircuitBreakerRollback),
	}
	if !d.CreatedAt.IsZero() {
		out["createdAt"] = fmt.Sprintf("%d", d.CreatedAt.Unix())
	}
	if !d.UpdatedAt.IsZero() {
		out["updatedAt"] = fmt.Sprintf("%d", d.UpdatedAt.Unix())
	}
	if b, err := json.Marshal(d.TargetGroupArns); err == nil {
		out["targetGroups"] = string(b)
	}
	if b, err := json.Marshal(d.Events); err == nil {
		out["events"] = string(b)
	}

	// Also fetch the task-def family for log groups + the
	// "<family>:<rev>" display row. Best-effort — if it fails we
	// still render the service details we already have.
	if family := taskDefFamilyFromArn(d.TaskDefinition); family != "" {
		if td, err := DescribeFamily(ctx, ac, family); err == nil && td != nil {
			out["familyArn"] = td.ARN
			out["family"] = td.Family
			out["revision"] = fmt.Sprintf("%d", td.Revision)
			if len(td.LogGroups) > 0 {
				out["logGroup"] = td.LogGroups[0]
			}
		}
	}
	return out, nil
}

func (ecsServiceProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}

// DetailRows builds the ~12-row Details panel body for an ECS service.
// All values come from the lazy map that ResolveDetails populated;
// there is no fallback to r.Meta because the user explicitly asked
// for fresh state on every entry. Returning nil during the in-flight
// window (lazy map is empty / missing required keys) triggers the
// "resolving details…" placeholder in details.go.
func (ecsServiceProvider) DetailRows(r core.Resource, lazy map[string]string) []services.DetailRow {
	if lazy == nil || lazy["status"] == "" {
		return nil
	}

	rows := []services.DetailRow{
		// Status zone — three prominent state rows.
		{Zone: services.ZoneStatus, Value: colorStatus(lazy["status"])},
		{Zone: services.ZoneStatus, Value: colorTasks(lazy["desiredCount"], lazy["runningCount"], lazy["pendingCount"])},
	}
	if rs := lazy["deploymentRolloutState"]; rs != "" {
		val := colorRollout(rs)
		if reason := lazy["deploymentRolloutReason"]; reason != "" {
			val = val + "  " + styleDim.Render("("+reason+")")
		}
		if lazy["circuitBreakerEnabled"] == "true" {
			cb := "circuit breaker: armed"
			if lazy["circuitBreakerRollback"] == "true" {
				cb = "circuit breaker: armed (rollback)"
			}
			val = val + "  " + styleDim.Render(cb)
		}
		rows = append(rows, services.DetailRow{Zone: services.ZoneStatus, Value: val})
	}

	// Metadata zone — the key/value bag.
	rows = append(rows, services.DetailRow{Label: "Cluster", Value: r.Meta[MetaCluster]})

	launch := lazy[MetaLaunchType]
	if pv := lazy["platformVersion"]; pv != "" {
		launch = launch + "  ·  " + pv
	}
	rows = append(rows, services.DetailRow{Label: "Launch", Value: launch})

	if family := lazy["family"]; family != "" {
		rev := lazy["revision"]
		td := family
		if rev != "" {
			td = family + ":" + rev
		}
		rows = append(rows, services.DetailRow{
			Label:          "Task def",
			Value:          td,
			Clickable:      true,
			ClipboardValue: td,
		})
	}

	if tgs := format.DecodeJSONSlice(lazy["targetGroups"]); len(tgs) > 0 {
		short := make([]string, 0, len(tgs))
		for _, arn := range tgs {
			short = append(short, lastARNSegment(arn))
		}
		joined := strings.Join(short, ", ")
		rows = append(rows, services.DetailRow{
			Label:          "LB target",
			Value:          joined,
			Clickable:      true,
			ClipboardValue: joined,
		})
	}

	if ts := lazy["createdAt"]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Created", Value: styleDim.Render(format.TimeAge(ts))})
	}
	if ts := lazy["updatedAt"]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Updated", Value: styleDim.Render(format.TimeAge(ts))})
	}

	if lg := lazy["logGroup"]; lg != "" {
		rows = append(rows, services.DetailRow{
			Label:          "Log",
			Value:          lg,
			Clickable:      true,
			ClipboardValue: lg,
		})
	}

	// Events zone — preformatted event lines, newest first.
	if events := format.DecodeJSONSlice(lazy["events"]); len(events) > 0 {
		for _, ev := range events {
			rows = append(rows, services.DetailRow{
				Zone:  services.ZoneEvents,
				Value: styleDim.Render(ev),
			})
		}
	}

	return rows
}

// --- helpers -----------------------------------------------------------

// boolString renders a Go bool as "true"/"false" for the string-valued
// lazyDetails map.
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// colorStatus renders an ECS service status with a red/yellow/green
// foreground.
func colorStatus(status string) string {
	switch status {
	case "ACTIVE":
		return styleGood.Render(status)
	case "DRAINING":
		return styleWarn.Render(status)
	case "INACTIVE":
		return styleBad.Render(status)
	default:
		return status
	}
}

// colorTasks renders the "<desired> desired · <running> running · <pending> pending"
// row with a green/yellow/red cell depending on whether desired == running.
func colorTasks(desiredStr, runningStr, pendingStr string) string {
	var desired, running, pending int
	fmt.Sscanf(desiredStr, "%d", &desired)
	fmt.Sscanf(runningStr, "%d", &running)
	fmt.Sscanf(pendingStr, "%d", &pending)
	txt := fmt.Sprintf("%d desired  ·  %d running  ·  %d pending", desired, running, pending)
	switch {
	case running == desired && pending == 0:
		return styleGood.Render(txt)
	case pending > 0 || running < desired:
		return styleWarn.Render(txt)
	default:
		return txt
	}
}

// colorRollout renders the ECS deployment rollout state with a
// green/yellow/red fg.
func colorRollout(state string) string {
	switch state {
	case "COMPLETED":
		return styleGood.Render(state)
	case "IN_PROGRESS":
		return styleWarn.Render(state)
	case "FAILED":
		return styleBad.Render(state)
	default:
		return state
	}
}

// --- styles ------------------------------------------------------------
//
// Defined at package scope so every DetailRow helper can reuse them.
// Providers own their own styles — no cross-package dependency on
// the tui/styles.go palette.

var (
	styleGood   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#005F00", Dark: "#5FFF5F"})
	styleWarn   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	styleBad    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#870000", Dark: "#FF5F5F"})
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FD7FF"})
)

// lastARNSegment extracts the trailing path segment of an ARN.
// Duplicated from internal/tui/browser.go so the provider doesn't
// need to import tui. 5 lines, not worth a shared package.
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}
