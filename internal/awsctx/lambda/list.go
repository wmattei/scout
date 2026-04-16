// Package lambda contains scout's thin wrappers around the AWS Lambda SDK.
package lambda

import (
	"context"
	"fmt"
	"strings"

	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListFunctions lists Lambda functions in the region. ListFunctions does not
// support a native name-prefix filter, so if opts.Prefix is set the filter
// is applied client-side on the function name after each page is fetched.
// When no limit is specified the adapter caps at 200 internally so the TUI's
// service-scope first-entry path doesn't paginate through hundreds of
// functions.
func ListFunctions(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awslambda.NewFromConfig(ac.Cfg)

	limit := opts.Limit
	if limit <= 0 {
		limit = 200 // sane default for interactive use
	}

	var resources []core.Resource
	var nextMarker *string

	for {
		input := &awslambda.ListFunctionsInput{
			Marker: nextMarker,
		}
		// The Lambda API uses MaxItems (int32) as page size cap.
		remaining := limit - len(resources)
		if remaining <= 0 {
			break
		}
		max := int32(remaining)
		if max > 50 {
			max = 50
		}
		input.MaxItems = &max

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
				MetaRuntime: string(fn.Runtime),
			}
			if fn.MemorySize != nil {
				meta[MetaMemorySize] = fmt.Sprintf("%d", *fn.MemorySize)
			}
			if fn.Timeout != nil {
				meta[MetaTimeout] = fmt.Sprintf("%d", *fn.Timeout)
			}
			if fn.LastModified != nil {
				meta[MetaLastModified] = *fn.LastModified
			}
			if fn.Handler != nil {
				meta[MetaHandler] = *fn.Handler
			}
			meta[MetaCodeSize] = fmt.Sprintf("%d", fn.CodeSize)
			if fn.Description != nil {
				meta[MetaDescription] = *fn.Description
			}

			resources = append(resources, core.Resource{
				Type:        core.RTypeLambdaFunction,
				Key:         *fn.FunctionArn,
				DisplayName: *fn.FunctionName,
				Meta:        meta,
			})

			if len(resources) >= limit {
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
