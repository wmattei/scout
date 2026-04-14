package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// TaskDefDetails is the minimal set of task-definition fields the TUI
// needs after lazy resolution: the full revision ARN, the family name,
// and each container's CloudWatch log group (when configured).
type TaskDefDetails struct {
	Family    string
	Revision  int32
	ARN       string
	LogGroups []string // one entry per container that has an awslogs log group
}

// DescribeFamily fetches the latest ACTIVE revision of a task definition
// family and returns a TaskDefDetails. The `family` argument is the bare
// family name (same as core.Resource.Key for RTypeEcsTaskDefFamily).
func DescribeFamily(ctx context.Context, ac *awsctx.Context, family string) (*TaskDefDetails, error) {
	client := awsecs.NewFromConfig(ac.Cfg)
	out, err := client.DescribeTaskDefinition(ctx, &awsecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(family),
	})
	if err != nil {
		return nil, fmt.Errorf("ecs:DescribeTaskDefinition (family=%s): %w", family, err)
	}
	td := out.TaskDefinition
	if td == nil {
		return nil, fmt.Errorf("ecs:DescribeTaskDefinition returned nil TaskDefinition for %s", family)
	}

	details := &TaskDefDetails{Family: family}
	if td.TaskDefinitionArn != nil {
		details.ARN = *td.TaskDefinitionArn
	}
	details.Revision = td.Revision

	for _, c := range td.ContainerDefinitions {
		if c.LogConfiguration == nil {
			continue
		}
		if string(c.LogConfiguration.LogDriver) != "awslogs" {
			continue
		}
		if group, ok := c.LogConfiguration.Options["awslogs-group"]; ok && group != "" {
			details.LogGroups = append(details.LogGroups, group)
		}
	}
	return details, nil
}
