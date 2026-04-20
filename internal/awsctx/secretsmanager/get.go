package secretsmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/wmattei/scout/internal/awsctx"
)

// ErrBinarySecret is returned by GetSecretValue when the resolved secret
// stores a SecretBinary payload instead of a SecretString. scout's TUI
// surfaces this as an informational toast rather than attempting to
// render raw bytes or base64 in the Details value cell.
var ErrBinarySecret = errors.New("secretsmanager: secret holds binary data (SecretBinary), not a string")

// SecretDetails is the resolved view of a single Secrets Manager secret,
// populated by GetSecretValue. Value is the decrypted SecretString.
// VersionID identifies the AWSCURRENT version at the time of the call.
type SecretDetails struct {
	Name        string
	ARN         string
	Value       string
	VersionID   string
	CreatedDate time.Time
}

// GetSecretValue calls secretsmanager:GetSecretValue and returns the
// decrypted SecretString plus identifying metadata. Binary-only secrets
// return ErrBinarySecret so callers can surface a clear message.
func GetSecretValue(ctx context.Context, ac *awsctx.Context, secretID string) (*SecretDetails, error) {
	client := awssm.NewFromConfig(ac.Cfg)

	out, err := client.GetSecretValue(ctx, &awssm.GetSecretValueInput{
		SecretId: &secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("secretsmanager:GetSecretValue (%s): %w", secretID, err)
	}

	d := &SecretDetails{}
	if out.Name != nil {
		d.Name = *out.Name
	}
	if out.ARN != nil {
		d.ARN = *out.ARN
	}
	if out.VersionId != nil {
		d.VersionID = *out.VersionId
	}
	if out.CreatedDate != nil {
		d.CreatedDate = *out.CreatedDate
	}
	if out.SecretString != nil {
		d.Value = *out.SecretString
		return d, nil
	}
	if len(out.SecretBinary) > 0 {
		return d, ErrBinarySecret
	}
	return d, nil
}
