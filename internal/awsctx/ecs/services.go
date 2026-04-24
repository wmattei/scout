// Package ecs contains scout's thin wrappers around the AWS ECS SDK.
package ecs

import (
	"context"
	"fmt"
	"strings"

	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListServices walks every cluster in the region and returns one Resource
// per service. Implementation steps:
//
//  1. ListClusters (paginated) — gives cluster ARNs.
//  2. For each cluster, ListServices (paginated) — gives service ARNs.
//  3. DescribeServices in batches of 10 (the hard limit for that API) —
//     gives launch type, desired count, and the user-facing service name.
//
// opts.Limit caps the total number of services returned and short-
// circuits the cluster walk once the cap is reached. opts.Prefix
// applies a case-sensitive client-side filter on the service name —
// ECS has no native server-side prefix on services, so this is the
// best we can do without per-service describes from a different code
// path.
//
// Pass `awsctx.ListOptions{}` for the historical "every service, no
// filter" behaviour.
func ListServices(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	// Step 1: clusters.
	var clusterArns []string
	var clusterNext *string
	for {
		out, err := client.ListClusters(ctx, &awsecs.ListClustersInput{NextToken: clusterNext})
		if err != nil {
			return nil, fmt.Errorf("ecs:ListClusters: %w", err)
		}
		clusterArns = append(clusterArns, out.ClusterArns...)
		if out.NextToken == nil {
			break
		}
		clusterNext = out.NextToken
	}

	var resources []core.Resource
	for _, cluster := range clusterArns {
		if opts.Limit > 0 && len(resources) >= opts.Limit {
			break
		}

		// Step 2: services within this cluster.
		var serviceArns []string
		var svcNext *string
		for {
			out, err := client.ListServices(ctx, &awsecs.ListServicesInput{
				Cluster:   stringPtr(cluster),
				NextToken: svcNext,
			})
			if err != nil {
				return nil, fmt.Errorf("ecs:ListServices (cluster=%s): %w", cluster, err)
			}
			serviceArns = append(serviceArns, out.ServiceArns...)
			if out.NextToken == nil {
				break
			}
			svcNext = out.NextToken
		}

		// Step 3: describe in batches of 10.
		for i := 0; i < len(serviceArns); i += 10 {
			if opts.Limit > 0 && len(resources) >= opts.Limit {
				break
			}
			end := i + 10
			if end > len(serviceArns) {
				end = len(serviceArns)
			}
			batch := serviceArns[i:end]
			out, err := client.DescribeServices(ctx, &awsecs.DescribeServicesInput{
				Cluster:  stringPtr(cluster),
				Services: batch,
			})
			if err != nil {
				return nil, fmt.Errorf("ecs:DescribeServices (cluster=%s): %w", cluster, err)
			}
			for _, svc := range out.Services {
				if svc.ServiceArn == nil || svc.ServiceName == nil {
					continue
				}
				if opts.Prefix != "" && !strings.HasPrefix(*svc.ServiceName, opts.Prefix) {
					continue
				}
				meta := map[string]string{
					MetaCluster:    clusterShortName(cluster),
					MetaClusterArn: cluster,
					MetaLaunchType: string(svc.LaunchType),
					MetaDesired:    fmt.Sprintf("%d", svc.DesiredCount),
				}
				if svc.TaskDefinition != nil {
					meta[MetaTaskDefFamily] = taskDefFamilyFromArn(*svc.TaskDefinition)
				}
				resources = append(resources, core.Resource{
					Key:         *svc.ServiceArn,
					DisplayName: *svc.ServiceName,
					Meta:        meta,
				})
				if opts.Limit > 0 && len(resources) >= opts.Limit {
					break
				}
			}
		}
	}
	return resources, nil
}

// clusterShortName extracts the cluster name from its ARN.
// arn:aws:ecs:us-east-1:123:cluster/prod-cluster -> prod-cluster
func clusterShortName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func stringPtr(s string) *string { return &s }

// taskDefFamilyFromArn extracts the family name from a task-definition
// ARN of the form
// arn:aws:ecs:<region>:<account>:task-definition/<family>:<revision>.
// Returns "" if the ARN doesn't match the expected shape.
func taskDefFamilyFromArn(arn string) string {
	// Find the segment after "task-definition/".
	const marker = "task-definition/"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	rest := arn[idx+len(marker):]
	// Strip ":<revision>" suffix if present.
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		return rest[:colon]
	}
	return rest
}
