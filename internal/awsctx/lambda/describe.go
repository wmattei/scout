package lambda

import (
	"context"
	"encoding/json"
	"fmt"

	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/wmattei/scout/internal/awsctx"
)

// FunctionDetails holds the extended details for a Lambda function
// as returned by GetFunction. These are the fields shown in the Details
// panel and used to populate the lazy detail map.
type FunctionDetails struct {
	Runtime      string
	MemorySize   int32
	Timeout      int32
	LastModified string
	Handler      string
	CodeSize     int64
	Description  string
	Layers       []string          // layer ARNs
	Tags         map[string]string // resource tags
}

// GetFunction calls lambda:GetFunction for the named function and
// returns a FunctionDetails with the fields needed for the Details panel.
func GetFunction(ctx context.Context, ac *awsctx.Context, functionName string) (*FunctionDetails, error) {
	client := awslambda.NewFromConfig(ac.Cfg)

	out, err := client.GetFunction(ctx, &awslambda.GetFunctionInput{
		FunctionName: &functionName,
	})
	if err != nil {
		return nil, fmt.Errorf("lambda:GetFunction (%s): %w", functionName, err)
	}

	d := &FunctionDetails{}
	if cfg := out.Configuration; cfg != nil {
		d.Runtime = string(cfg.Runtime)
		if cfg.MemorySize != nil {
			d.MemorySize = *cfg.MemorySize
		}
		if cfg.Timeout != nil {
			d.Timeout = *cfg.Timeout
		}
		if cfg.LastModified != nil {
			d.LastModified = *cfg.LastModified
		}
		if cfg.Handler != nil {
			d.Handler = *cfg.Handler
		}
		d.CodeSize = cfg.CodeSize
		if cfg.Description != nil {
			d.Description = *cfg.Description
		}
		for _, layer := range cfg.Layers {
			if layer.Arn != nil {
				d.Layers = append(d.Layers, *layer.Arn)
			}
		}
	}

	// Tags come from the top-level Tags map in the GetFunction response.
	if len(out.Tags) > 0 {
		d.Tags = make(map[string]string, len(out.Tags))
		for k, v := range out.Tags {
			d.Tags[k] = v
		}
	}

	return d, nil
}

// marshalStringSlice JSON-encodes a string slice. Returns "" on empty input
// or encode failure so callers can treat "missing" and "empty" identically.
func marshalStringSlice(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return ""
	}
	return string(b)
}

// marshalStringMap JSON-encodes a string map. Returns "" on empty input.
func marshalStringMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
