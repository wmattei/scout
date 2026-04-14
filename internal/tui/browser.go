package tui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// consoleURL builds an AWS web-console deep link for the given resource.
// The region is always added as a query parameter so the console opens in
// the right place even if the user's browser session is set to a different
// default.
//
// For ECS task-def families the caller MUST pass the latest revision ARN
// in `taskDefArn` (resolved lazily on entering Details). If empty, the URL
// points at the family-level route which 404s on some consoles — the
// Details view blocks Open until the lazy resolution lands.
func consoleURL(r core.Resource, region string, taskDefArn string) string {
	switch r.Type {
	case core.RTypeBucket:
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s",
			url.PathEscape(r.Key), region)

	case core.RTypeFolder:
		bucket := r.Meta["bucket"]
		prefix := strings.TrimSuffix(r.Key, "/")
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s&prefix=%s&showversions=false",
			url.PathEscape(bucket), region, url.QueryEscape(prefix+"/"))

	case core.RTypeObject:
		bucket := r.Meta["bucket"]
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/object/%s?region=%s&prefix=%s",
			url.PathEscape(bucket), region, url.QueryEscape(r.Key))

	case core.RTypeEcsService:
		cluster := r.Meta["cluster"]
		// r.Key is the full service ARN. Extract the service name.
		svcName := lastARNSegment(r.Key)
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
			region, url.PathEscape(cluster), url.PathEscape(svcName), region)

	case core.RTypeEcsTaskDefFamily:
		// Prefer the resolved revision ARN when available.
		family := r.Key
		rev := ""
		if taskDefArn != "" {
			// arn:aws:ecs:...:task-definition/family:42
			if i := strings.LastIndexByte(taskDefArn, ':'); i > 0 {
				rev = taskDefArn[i+1:]
			}
		}
		if rev != "" {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s/%s?region=%s",
				region, url.PathEscape(family), url.PathEscape(rev), region)
		}
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s?region=%s",
			region, url.PathEscape(family), region)
	}
	return ""
}

// lastARNSegment returns the segment after the last `/` in an ARN. For
// "arn:aws:ecs:us-east-1:123:service/cluster/svc" it returns "svc".
func lastARNSegment(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// openInBrowser hands off a URL to the OS's default browser launcher.
// Returns an error describing the problem so the caller can surface it
// via a toast. Windows is intentionally unsupported in v0 — see the
// "Cross-OS limitations" section of the Phase 3 plan.
func openInBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return fmt.Errorf("open-in-browser not supported on %s (v0 limitation)", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching browser: %w", err)
	}
	// We intentionally do not Wait() — xdg-open returns quickly but the
	// browser process may be long-lived, and we don't want to block.
	return nil
}
