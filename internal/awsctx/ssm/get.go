package ssm

import (
	"context"
	"fmt"
	"time"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/wmattei/scout/internal/awsctx"
)

// ParameterDetails holds the full resolved details for an SSM parameter,
// including the decrypted value (for SecureString parameters).
type ParameterDetails struct {
	Name         string
	Type         string // String, StringList, SecureString
	Value        string // decrypted
	Version      int64
	LastModified time.Time
	DataType     string // text, aws:ec2:image, aws:ssm:integration
	ARN          string
}

// GetParameter calls ssm:GetParameter with WithDecryption=true and returns
// the full parameter details including the decrypted value.
func GetParameter(ctx context.Context, ac *awsctx.Context, name string) (*ParameterDetails, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

	withDecryption := true
	out, err := client.GetParameter(ctx, &awsssm.GetParameterInput{
		Name:           &name,
		WithDecryption: &withDecryption,
	})
	if err != nil {
		return nil, fmt.Errorf("ssm:GetParameter (%s): %w", name, err)
	}

	if out.Parameter == nil {
		return nil, fmt.Errorf("ssm:GetParameter (%s): nil parameter in response", name)
	}

	p := out.Parameter
	d := &ParameterDetails{
		Version: p.Version,
	}
	if p.DataType != nil {
		d.DataType = *p.DataType
	}
	if p.Name != nil {
		d.Name = *p.Name
	}
	d.Type = string(p.Type)
	if p.Value != nil {
		d.Value = *p.Value
	}
	if p.LastModifiedDate != nil {
		d.LastModified = *p.LastModifiedDate
	}
	if p.ARN != nil {
		d.ARN = *p.ARN
	}
	return d, nil
}
