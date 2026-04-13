// Package ecs contains better-aws's thin wrappers around the AWS ECS SDK.
package ecs

import (
	"context"
	"fmt"
	"strings"

	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListServices walks every cluster in the region and returns one Resource
// per service. Implementation steps:
//
//  1. ListClusters (paginated) — gives cluster ARNs.
//  2. For each cluster, ListServices (paginated) — gives service ARNs.
//  3. DescribeServices in batches of 10 (the hard limit for that API) —
//     gives launch type, desired count, and the user-facing service name.
//
// The Key is the full service ARN so actions (Phase 3) can use it directly.
// DisplayName is the bare service name (last segment of the ARN path).
// Meta includes the cluster ARN so the Tail Logs action can resolve tasks.
func ListServices(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
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
				resources = append(resources, core.Resource{
					Type:        core.RTypeEcsService,
					Key:         *svc.ServiceArn,
					DisplayName: *svc.ServiceName,
					Meta: map[string]string{
						"cluster":    clusterShortName(cluster),
						"clusterArn": cluster,
						"launchType": string(svc.LaunchType),
						"desired":    fmt.Sprintf("%d", svc.DesiredCount),
					},
				})
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
