// Package lambda contains better-aws's thin wrappers around the AWS Lambda SDK.
package lambda

import (
	"context"
	"fmt"
	"strings"

	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListFunctions lists Lambda functions in the region. ListFunctions does not
// support a native name-prefix filter, so if opts.Prefix is set the filter
// is applied client-side on the function name after each page is fetched.
func ListFunctions(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awslambda.NewFromConfig(ac.Cfg)

	var resources []core.Resource
	var nextMarker *string

	for {
		input := &awslambda.ListFunctionsInput{
			Marker: nextMarker,
		}
		// The Lambda API uses MaxItems (int32) as page size cap.
		if opts.Limit > 0 {
			remaining := opts.Limit - len(resources)
			if remaining <= 0 {
				break
			}
			max := int32(remaining)
			if max > 50 {
				max = 50
			}
			input.MaxItems = &max
		}

		out, err := client.ListFunctions(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("lambda:ListFunctions: %w", err)
		}

		for _, fn := range out.Functions {
			if fn.FunctionArn == nil || fn.FunctionName == nil {
				continue
			}
			if opts.Prefix != "" && !strings.HasPrefix(*fn.FunctionName, opts.Prefix) {
				continue
			}

			meta := map[string]string{
				"runtime": string(fn.Runtime),
			}
			if fn.MemorySize != nil {
				meta["memorySize"] = fmt.Sprintf("%d", *fn.MemorySize)
			}
			if fn.Timeout != nil {
				meta["timeout"] = fmt.Sprintf("%d", *fn.Timeout)
			}
			if fn.LastModified != nil {
				meta["lastModified"] = *fn.LastModified
			}
			if fn.Handler != nil {
				meta["handler"] = *fn.Handler
			}
			meta["codeSize"] = fmt.Sprintf("%d", fn.CodeSize)
			if fn.Description != nil {
				meta["description"] = *fn.Description
			}

			resources = append(resources, core.Resource{
				Type:        core.RTypeLambdaFunction,
				Key:         *fn.FunctionArn,
				DisplayName: *fn.FunctionName,
				Meta:        meta,
			})

			if opts.Limit > 0 && len(resources) >= opts.Limit {
				break
			}
		}

		if opts.Limit > 0 && len(resources) >= opts.Limit {
			break
		}
		if out.NextMarker == nil {
			break
		}
		nextMarker = out.NextMarker
	}

	return resources, nil
}
