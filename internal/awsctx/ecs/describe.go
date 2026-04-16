package ecs

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wmattei/scout/internal/awsctx"
)

// ServiceDetails is the slice of DescribeServices output fields that
// the TUI Details view actually renders. Assembled by DescribeService
// and stored in the provider's lazyDetails map via JSON encoding.
type ServiceDetails struct {
	Status          string    // ACTIVE, DRAINING, INACTIVE
	DesiredCount    int32
	RunningCount    int32
	PendingCount    int32
	LaunchType      string
	PlatformVersion string
	TaskDefinition  string    // full ARN of the currently-deployed revision
	CreatedAt       time.Time // service creation time (zero if unset)
	UpdatedAt       time.Time // last service update time (zero if unset)

	// Primary deployment info. ECS returns a list of deployments but
	// we only surface the PRIMARY (active rolling deploy) one.
	DeploymentRolloutState       string // IN_PROGRESS, COMPLETED, FAILED, or ""
	DeploymentRolloutStateReason string

	// Deployment circuit breaker — ON only if configured AND tripped.
	CircuitBreakerEnabled bool
	CircuitBreakerRollback bool

	// TargetGroupArns is a short list of ARNs attached to the service.
	// Usually 0 or 1 entries.
	TargetGroupArns []string

	// Events is the most recent N service events, newest-first.
	// Each entry is pre-formatted "HH:MM:SS  <message>".
	Events []string
}

// DescribeService fetches the live state of a single ECS service and
// returns a ServiceDetails struct. The caller passes the cluster ARN
// (from Meta["clusterArn"]) and the service ARN (from r.Key). Used by
// the ecsServiceProvider's ResolveDetails — always refire on every
// Details entry so the user sees fresh runningCount / deployment
// state.
func DescribeService(ctx context.Context, ac *awsctx.Context, clusterArn, serviceArn string) (*ServiceDetails, error) {
	client := awsecs.NewFromConfig(ac.Cfg)
	out, err := client.DescribeServices(ctx, &awsecs.DescribeServicesInput{
		Cluster:  aws.String(clusterArn),
		Services: []string{serviceArn},
	})
	if err != nil {
		return nil, fmt.Errorf("ecs:DescribeServices (cluster=%s service=%s): %w", clusterArn, serviceArn, err)
	}
	if len(out.Services) == 0 {
		return nil, fmt.Errorf("ecs:DescribeServices returned no service for %s", serviceArn)
	}

	svc := out.Services[0]
	d := &ServiceDetails{
		Status:          str(svc.Status),
		DesiredCount:    svc.DesiredCount,
		RunningCount:    svc.RunningCount,
		PendingCount:    svc.PendingCount,
		LaunchType:      string(svc.LaunchType),
		PlatformVersion: str(svc.PlatformVersion),
		TaskDefinition:  str(svc.TaskDefinition),
	}
	if svc.CreatedAt != nil {
		d.CreatedAt = *svc.CreatedAt
	}
	// UpdatedAt isn't a direct field; use the primary deployment's
	// UpdatedAt as a proxy since that's what changes when the service
	// config changes.

	// Primary deployment + circuit breaker.
	for _, dep := range svc.Deployments {
		if str(dep.Status) == "PRIMARY" {
			d.DeploymentRolloutState = string(dep.RolloutState)
			d.DeploymentRolloutStateReason = str(dep.RolloutStateReason)
			if dep.UpdatedAt != nil {
				d.UpdatedAt = *dep.UpdatedAt
			}
			break
		}
	}
	if svc.DeploymentConfiguration != nil && svc.DeploymentConfiguration.DeploymentCircuitBreaker != nil {
		d.CircuitBreakerEnabled = svc.DeploymentConfiguration.DeploymentCircuitBreaker.Enable
		d.CircuitBreakerRollback = svc.DeploymentConfiguration.DeploymentCircuitBreaker.Rollback
	}

	// Load balancer target groups.
	for _, lb := range svc.LoadBalancers {
		if lb.TargetGroupArn != nil {
			d.TargetGroupArns = append(d.TargetGroupArns, *lb.TargetGroupArn)
		}
	}

	// Recent events, newest first, cap at 5.
	const maxEvents = 5
	for i, ev := range svc.Events {
		if i >= maxEvents {
			break
		}
		msg := str(ev.Message)
		ts := ""
		if ev.CreatedAt != nil {
			ts = ev.CreatedAt.Local().Format("15:04:05")
		}
		if ts != "" {
			d.Events = append(d.Events, ts+"  "+msg)
		} else {
			d.Events = append(d.Events, msg)
		}
	}
	return d, nil
}

// str dereferences an *string, returning "" when the pointer is nil.
// Keeps the DescribeService body readable.
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// CountRunningTasks walks every cluster and counts tasks that belong
// to the given task-definition family and are in the RUNNING desired
// status. Returns the total count across all clusters. Used by the
// task-def Details page to show a live "N running" row.
func CountRunningTasks(ctx context.Context, ac *awsctx.Context, family string) (int, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	// Step 1: list all clusters.
	var clusterArns []string
	var clusterNext *string
	for {
		out, err := client.ListClusters(ctx, &awsecs.ListClustersInput{NextToken: clusterNext})
		if err != nil {
			return 0, fmt.Errorf("ecs:ListClusters: %w", err)
		}
		clusterArns = append(clusterArns, out.ClusterArns...)
		if out.NextToken == nil {
			break
		}
		clusterNext = out.NextToken
	}

	// Step 2: per-cluster ListTasks filtered by family + RUNNING.
	total := 0
	for _, cluster := range clusterArns {
		var taskNext *string
		for {
			out, err := client.ListTasks(ctx, &awsecs.ListTasksInput{
				Cluster:       aws.String(cluster),
				Family:        aws.String(family),
				DesiredStatus: "RUNNING",
				NextToken:     taskNext,
			})
			if err != nil {
				// Best-effort — some clusters may reject (e.g. if
				// the caller doesn't have ecs:ListTasks permission
				// in that cluster). Skip silently so the rest still
				// resolve.
				break
			}
			total += len(out.TaskArns)
			if out.NextToken == nil {
				break
			}
			taskNext = out.NextToken
		}
	}
	return total, nil
}

// TaskDefDetails holds the fields the TUI renders in the Details
// panel when a task definition family is selected. Assembled by
// DescribeFamily from a single DescribeTaskDefinition call.
type TaskDefDetails struct {
	Family    string
	Revision  int32
	ARN       string
	LogGroups []string // one entry per container that has an awslogs log group

	// Extended fields for the Details panel.
	CPU                    string   // e.g. "256", "1024"
	Memory                 string   // e.g. "512", "2048"
	NetworkMode            string   // awsvpc, bridge, host, none
	TaskRoleArn            string
	ExecutionRoleArn       string
	RequiresCompatibilities []string // FARGATE, EC2
	ContainerImages        []string // one "<name>=<image>" per container
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

	if td.Cpu != nil {
		details.CPU = *td.Cpu
	}
	if td.Memory != nil {
		details.Memory = *td.Memory
	}
	details.NetworkMode = string(td.NetworkMode)
	if td.TaskRoleArn != nil {
		details.TaskRoleArn = *td.TaskRoleArn
	}
	if td.ExecutionRoleArn != nil {
		details.ExecutionRoleArn = *td.ExecutionRoleArn
	}
	for _, compat := range td.RequiresCompatibilities {
		details.RequiresCompatibilities = append(details.RequiresCompatibilities, string(compat))
	}

	for _, c := range td.ContainerDefinitions {
		name := ""
		if c.Name != nil {
			name = *c.Name
		}
		image := ""
		if c.Image != nil {
			image = *c.Image
		}
		if name != "" {
			details.ContainerImages = append(details.ContainerImages, name+"="+image)
		}

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
