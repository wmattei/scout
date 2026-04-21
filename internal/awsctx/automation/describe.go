package automation

import (
	"context"
	"fmt"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/wmattei/scout/internal/awsctx"
)

// DocumentDetails is the resolved view of a single Automation document
// surfaced in the Details panel. Separate from DescribeDocument's raw
// output so the TUI doesn't depend on SDK struct shapes.
type DocumentDetails struct {
	Name          string
	Description   string
	Owner         string
	VersionName   string
	LatestVersion string
	DocumentType  string
	TargetType    string
	PlatformTypes []string
	Parameters    []ParameterInfo
	Content       string
	ContentFormat string // YAML or JSON
}

// ParameterInfo describes one input parameter declared by the
// document. Used to build the editor template the Run action opens.
type ParameterInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // String or StringList
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"default,omitempty"`
}

// DescribeDocument fetches the document description plus its raw
// content. Content retrieval is best-effort — some documents don't
// expose content due to share settings, and that shouldn't fail the
// whole details resolve.
func DescribeDocument(ctx context.Context, ac *awsctx.Context, name string) (*DocumentDetails, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

	out, err := client.DescribeDocument(ctx, &awsssm.DescribeDocumentInput{Name: &name})
	if err != nil {
		return nil, fmt.Errorf("ssm:DescribeDocument (%s): %w", name, err)
	}
	d := out.Document
	if d == nil {
		return nil, fmt.Errorf("ssm:DescribeDocument (%s): nil document in response", name)
	}

	info := &DocumentDetails{
		DocumentType: string(d.DocumentType),
	}
	if d.Name != nil {
		info.Name = *d.Name
	}
	if d.Description != nil {
		info.Description = *d.Description
	}
	if d.Owner != nil {
		info.Owner = *d.Owner
	}
	if d.VersionName != nil {
		info.VersionName = *d.VersionName
	}
	if d.LatestVersion != nil {
		info.LatestVersion = *d.LatestVersion
	}
	if d.TargetType != nil {
		info.TargetType = *d.TargetType
	}
	for _, p := range d.PlatformTypes {
		info.PlatformTypes = append(info.PlatformTypes, string(p))
	}
	for _, p := range d.Parameters {
		pi := ParameterInfo{Type: string(p.Type)}
		if p.Name != nil {
			pi.Name = *p.Name
		}
		if p.Description != nil {
			pi.Description = *p.Description
		}
		if p.DefaultValue != nil {
			pi.DefaultValue = *p.DefaultValue
		}
		info.Parameters = append(info.Parameters, pi)
	}

	return info, nil
}
