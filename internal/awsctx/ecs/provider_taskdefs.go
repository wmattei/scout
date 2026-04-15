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

func (ecsTaskDefProvider) ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	return ListTaskDefFamilies(ctx, ac, opts)
}

// ResolveDetails fires DescribeTaskDefinition for the family and
// returns the resolved revision ARN + log group list. The keys
// match what ConsoleURL and LogGroup read.
func (ecsTaskDefProvider) ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error) {
	d, err := DescribeFamily(ctx, ac, r.Key)
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

func (ecsTaskDefProvider) LogGroup(_ core.Resource, lazy map[string]string) string {
	if lazy == nil {
		return ""
	}
	return lazy["logGroup"]
}
