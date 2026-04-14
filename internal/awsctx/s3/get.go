package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// StreamObject streams the object at (bucket, key) into dst. Returns the
// number of bytes copied, plus any error from the SDK call or the copy.
//
// Callers that just want the size without downloading should use
// HeadObject instead.
func StreamObject(ctx context.Context, ac *awsctx.Context, bucket, key string, dst io.Writer) (int64, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("s3:GetObject (bucket=%s key=%s): %w", bucket, key, err)
	}
	defer out.Body.Close()

	n, err := io.Copy(dst, out.Body)
	if err != nil {
		return n, fmt.Errorf("copying object body: %w", err)
	}
	return n, nil
}

// HeadObject returns the object's size in bytes. Used by Preview to check
// the size cap before deciding whether to download.
func HeadObject(ctx context.Context, ac *awsctx.Context, bucket, key string) (int64, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("s3:HeadObject (bucket=%s key=%s): %w", bucket, key, err)
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}
