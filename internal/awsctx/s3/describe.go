package s3

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// BucketDetails holds the result of the parallel Get* calls that the
// bucket provider fires on entering modeDetails. Each field is
// best-effort — individual calls may fail (e.g. missing encryption
// config) without poisoning the whole struct.
type BucketDetails struct {
	Versioning   string   // "Enabled", "Suspended", "Disabled"
	Encryption   string   // "AES256", "aws:kms", "aws:kms:dsse", "None"
	PublicAccess string   // "All blocked", "Partially open", "Not configured"
	Tags         []string // "key=value" pairs, first 5
}

// DescribeBucket fires four lightweight S3 Get* calls in parallel and
// returns a BucketDetails. Each sub-call that fails is logged (if
// debug logging is enabled) and the corresponding field stays at its
// zero value, which the provider renders as "–" or omits.
func DescribeBucket(ctx context.Context, ac *awsctx.Context, bucket string) (*BucketDetails, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	d := &BucketDetails{
		Versioning:   "Disabled",
		Encryption:   "None",
		PublicAccess: "Not configured",
	}
	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(4)

	// 1. Versioning.
	go func() {
		defer wg.Done()
		out, err := client.GetBucketVersioning(ctx, &awss3.GetBucketVersioningInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch string(out.Status) {
		case "Enabled":
			d.Versioning = "Enabled"
		case "Suspended":
			d.Versioning = "Suspended"
		}
	}()

	// 2. Encryption.
	go func() {
		defer wg.Done()
		out, err := client.GetBucketEncryption(ctx, &awss3.GetBucketEncryptionInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			// "ServerSideEncryptionConfigurationNotFoundError" means
			// no default encryption — leave as "None".
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if out.ServerSideEncryptionConfiguration != nil {
			for _, rule := range out.ServerSideEncryptionConfiguration.Rules {
				if rule.ApplyServerSideEncryptionByDefault != nil {
					d.Encryption = string(rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm)
					break
				}
			}
		}
	}()

	// 3. Public access block.
	go func() {
		defer wg.Done()
		out, err := client.GetPublicAccessBlock(ctx, &awss3.GetPublicAccessBlockInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			// "NoSuchPublicAccessBlockConfiguration" means the block
			// was never configured — leave as "Not configured".
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if out.PublicAccessBlockConfiguration != nil {
			c := out.PublicAccessBlockConfiguration
			allBlocked := ptrBool(c.BlockPublicAcls) &&
				ptrBool(c.IgnorePublicAcls) &&
				ptrBool(c.BlockPublicPolicy) &&
				ptrBool(c.RestrictPublicBuckets)
			if allBlocked {
				d.PublicAccess = "All blocked"
			} else {
				d.PublicAccess = "Partially open"
			}
		}
	}()

	// 4. Tags (first 5).
	go func() {
		defer wg.Done()
		out, err := client.GetBucketTagging(ctx, &awss3.GetBucketTaggingInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			// "NoSuchTagSet" means no tags — leave empty.
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for i, t := range out.TagSet {
			if i >= 5 {
				break
			}
			if t.Key != nil && t.Value != nil {
				d.Tags = append(d.Tags, fmt.Sprintf("%s=%s", *t.Key, *t.Value))
			}
		}
	}()

	wg.Wait()
	return d, nil
}

// ptrBool dereferences a *bool, treating nil as false.
func ptrBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
