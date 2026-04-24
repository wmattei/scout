package s3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListAtPrefix lists folders (virtual, via CommonPrefixes) and objects
// directly under `prefix` in the given bucket. The call uses delimiter "/"
// so we only walk one level at a time, matching the TUI's breadcrumb
// navigation model.
//
// `limit` caps how many total rows (folders + objects combined) are
// returned. It is also passed through as `MaxKeys` on the S3 request so
// the server stops early — critical for buckets with millions of keys
// where we only want the top N results anyway. A single page is fetched;
// no pagination. Callers that need a full crawl should build a dedicated
// function for it.
//
// DisplayName for folders is the trailing segment including the `/`
// (e.g. "logs/"); for objects, it's the trailing segment without a slash
// (e.g. "2026-04-13.csv"). Key for folders is the full key relative to
// the bucket root (e.g. "app/logs/"); for objects, it's the full key
// (e.g. "app/logs/2026-04-13.csv"). Meta carries bucket, plus size/mtime
// for objects.
func ListAtPrefix(ctx context.Context, ac *awsctx.Context, bucket, prefix string, limit int) ([]core.Resource, error) {
	if limit <= 0 {
		limit = 20
	}
	client := awss3.NewFromConfig(ac.Cfg)

	page, err := client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(int32(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("s3:ListObjectsV2 (bucket=%s prefix=%s): %w", bucket, prefix, err)
	}

	out := make([]core.Resource, 0, limit)
	for _, p := range page.CommonPrefixes {
		if p.Prefix == nil {
			continue
		}
		full := *p.Prefix
		out = append(out, core.Resource{
			Key:         full,
			DisplayName: lastSegmentWithSlash(full),
			Meta: map[string]string{
				MetaBucket: bucket,
			},
		})
	}
	for _, o := range page.Contents {
		if o.Key == nil {
			continue
		}
		full := *o.Key
		// Skip the "placeholder" row that equals the prefix itself —
		// some tools create a zero-byte marker at the folder key.
		if full == prefix {
			continue
		}
		meta := map[string]string{MetaBucket: bucket}
		if o.Size != nil {
			meta[MetaSize] = fmt.Sprintf("%d", *o.Size)
		}
		if o.LastModified != nil {
			meta[MetaMtime] = fmt.Sprintf("%d", o.LastModified.Unix())
		}
		out = append(out, core.Resource{
			Key:         full,
			DisplayName: lastSegment(full),
			Meta:        meta,
		})
	}
	return out, nil
}

// lastSegmentWithSlash returns the final path segment of a CommonPrefix,
// preserving the trailing slash. "a/b/c/" -> "c/".
func lastSegmentWithSlash(s string) string {
	trimmed := strings.TrimSuffix(s, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[i+1:] + "/"
	}
	return trimmed + "/"
}

// lastSegment returns the final path segment of an object key. "a/b/c.txt"
// -> "c.txt". If the key has no slash, it returns the whole key.
func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// formatUnixTimeFmt renders a Unix-second timestamp into the same
// "YYYY-MM-DD HH:MM" shape used by the TUI's results view. Lives
// here rather than in the TUI so providers can reuse it without
// pulling in the tui package.
func formatUnixTimeFmt(n int64) string {
	return time.Unix(n, 0).Local().Format("2006-01-02 15:04")
}
