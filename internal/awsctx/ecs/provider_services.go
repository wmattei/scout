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
	"github.com/wagnermattei/better-aws-cli/internal/services"
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
	cluster := r.Meta["cluster"]
	svcName := lastARNSegment(r.Key)
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
		region, url.PathEscape(cluster), url.PathEscape(svcName), region)
}

func (ecsServiceProvider) RenderMeta(r core.Resource) string {
	return r.Meta["cluster"]
}

func (ecsServiceProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListServices(ctx, ac, opts)
}

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
	clusterArn := r.Meta["clusterArn"]
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
		"launchType":               d.LaunchType,
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
		{Label: "Cluster", Value: r.Meta["cluster"]},
		{Label: "Status", Value: colorStatus(lazy["status"])},
		{Label: "Tasks", Value: colorTasks(lazy["desiredCount"], lazy["runningCount"], lazy["pendingCount"])},
	}

	// Launch + platform.
	launch := lazy["launchType"]
	if pv := lazy["platformVersion"]; pv != "" {
		launch = launch + "  ·  " + pv
	}
	rows = append(rows, services.DetailRow{Label: "Launch", Value: launch})

	// Task def family:revision.
	if family := lazy["family"]; family != "" {
		rev := lazy["revision"]
		td := family
		if rev != "" {
			td = family + ":" + rev
		}
		rows = append(rows, services.DetailRow{Label: "Task def", Value: td})
	}

	// Deployment rollout state + circuit breaker.
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
		rows = append(rows, services.DetailRow{Label: "Deployment", Value: val})
	}

	// Load balancer target groups (short names only).
	if tgs := decodeJSONSlice(lazy["targetGroups"]); len(tgs) > 0 {
		short := make([]string, 0, len(tgs))
		for _, arn := range tgs {
			short = append(short, lastARNSegment(arn))
		}
		rows = append(rows, services.DetailRow{Label: "LB target", Value: strings.Join(short, ", ")})
	}

	// Created / Updated with relative age.
	if ts := lazy["createdAt"]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Created", Value: formatTimeAge(ts)})
	}
	if ts := lazy["updatedAt"]; ts != "" {
		rows = append(rows, services.DetailRow{Label: "Updated", Value: formatTimeAge(ts)})
	}

	// Log group (already shown in the existing renderDetails caller;
	// duplicated here so a DetailRow-based renderer has the full row
	// set without special-casing log groups).
	if lg := lazy["logGroup"]; lg != "" {
		rows = append(rows, services.DetailRow{Label: "Log", Value: lg})
	}

	// Recent events — blank separator row, section header, then up to
	// 5 event lines.
	if events := decodeJSONSlice(lazy["events"]); len(events) > 0 {
		rows = append(rows, services.DetailRow{}) // blank spacer
		rows = append(rows, services.DetailRow{Value: styleHeader.Render("Recent events")})
		for _, ev := range events {
			rows = append(rows, services.DetailRow{Label: "", Value: styleDim.Render(ev)})
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

// decodeJSONSlice unmarshals a JSON-encoded []string out of a lazy
// map slot. Returns nil on empty input or decode failure so callers
// can treat "missing" and "empty" identically.
func decodeJSONSlice(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// formatTimeAge returns "2026-04-13 15:42  (2h ago)" given a Unix
// seconds string. Empty / unparseable input returns "".
func formatTimeAge(s string) string {
	var unix int64
	_, err := fmt.Sscanf(s, "%d", &unix)
	if err != nil || unix <= 0 {
		return ""
	}
	t := time.Unix(unix, 0).Local()
	age := time.Since(t)
	return fmt.Sprintf("%s  %s", t.Format("2006-01-02 15:04"), styleDim.Render("("+humanDuration(age)+" ago)"))
}

// humanDuration renders a time.Duration as "34d", "6h", "12m", "45s".
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
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
