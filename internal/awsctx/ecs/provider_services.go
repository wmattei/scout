package ecs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

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

func (ecsServiceProvider) ARN(r core.Resource) string {
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

// ResolveDetails resolves the latest task-def revision + log groups
// for the service, by way of its task-def family. The TUI calls this
// once on entering modeDetails; the result lands in m.lazyDetails
// keyed by (RTypeEcsService, r.Key) and is consumed by LogGroup.
func (ecsServiceProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	family := r.Meta["taskDefFamily"]
	if family == "" {
		return nil, nil
	}
	d, err := DescribeFamily(ctx, ac, family)
	if err != nil || d == nil {
		return nil, err
	}
	out := map[string]string{
		"familyArn": d.ARN,
	}
	if len(d.LogGroups) > 0 {
		out["logGroup"] = d.LogGroups[0]
	}
	return out, nil
}

func (ecsServiceProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}

// lastARNSegment extracts the trailing path segment of an ARN.
// Duplicated from internal/tui/browser.go so the provider doesn't
// need to import tui. 5 lines, not worth a shared package.
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}
