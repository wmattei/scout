package search

import (
	"strings"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// Scope describes how the TUI should interpret the current input value.
//
// Three modes are possible:
//
//  1. Service scope — the input begins with "<alias>:" where <alias> is
//     a registered name in core.serviceAliases. Everything after the
//     colon is ServiceQuery and is used as a fuzzy filter against the
//     in-memory index restricted to the matching resource type. This
//     mode is flat; "/" after the colon is not reparsed.
//
//  2. S3 drill-in — the input contains "/" and is NOT a service scope.
//     Bucket is the bucket name (everything before the first "/"),
//     Prefix is the folder path passed to ListObjectsV2 (ending in "/"
//     or empty), and Leaf is the characters after the last "/".
//
//  3. Top level — neither of the above. The whole input is Leaf and is
//     used as a fuzzy filter against every cached top-level resource.
type Scope struct {
	Raw string // the original input string, verbatim

	// Service-scope fields. HasService is set when the input was
	// recognized as "<alias>:<query>". In that case Service, ServiceAlias,
	// and ServiceQuery are populated and the other fields are empty.
	HasService   bool
	Service      core.ResourceType
	ServiceAlias string
	ServiceQuery string

	// S3 drill-in fields. Populated only when HasService is false and
	// the input contains at least one "/".
	Bucket string
	Prefix string
	Leaf   string
}

// IsTopLevel reports whether neither a service filter nor an S3 bucket
// drill-in is active, meaning the search should fuzzy-match against
// every cached top-level resource.
func (s Scope) IsTopLevel() bool {
	return !s.HasService && s.Bucket == ""
}

// ParseScope converts an input string into a Scope.
//
// Examples:
//
//	""                          -> top-level, Leaf=""
//	"my-bu"                     -> top-level, Leaf="my-bu"
//	"s3:"                       -> service scope RTypeBucket, ServiceQuery=""
//	"s3:prod"                   -> service scope RTypeBucket, ServiceQuery="prod"
//	"ecs:api"                   -> service scope RTypeEcsService, ServiceQuery="api"
//	"td:worker"                 -> service scope RTypeEcsTaskDefFamily, ServiceQuery="worker"
//	"my-bucket/"                -> bucket=my-bucket, Prefix="", Leaf=""
//	"my-bucket/logs/"           -> bucket=my-bucket, Prefix="logs/", Leaf=""
//	"my-bucket/logs/2026"       -> bucket=my-bucket, Prefix="logs/", Leaf="2026"
//	"my-bucket/logs/2026/file"  -> bucket=my-bucket, Prefix="logs/2026/", Leaf="file"
//
// Service scope takes precedence over S3 drill-in: an input like
// "s3:prod/bar" is a flat service filter with ServiceQuery="prod/bar",
// NOT an attempt to drill into a bucket named "s3:prod".
func ParseScope(input string) Scope {
	s := Scope{Raw: input}

	// 1. Service-scope parsing. If the part before the first ":" is a
	//    registered alias, the whole input is a service-scoped filter
	//    and we return early.
	if colon := strings.IndexByte(input, ':'); colon >= 0 {
		prefix := input[:colon]
		if rt, ok := core.ResourceTypeForAlias(prefix); ok {
			s.HasService = true
			s.Service = rt
			s.ServiceAlias = prefix
			s.ServiceQuery = input[colon+1:]
			return s
		}
	}

	// 2. S3 drill-in parsing. The first "/" splits bucket from rest;
	//    the last "/" in the remainder splits prefix from leaf.
	slash := strings.IndexByte(input, '/')
	if slash < 0 {
		// No slash means we are still at the top level. The whole input is
		// the leaf used for fuzzy matching.
		s.Leaf = input
		return s
	}

	s.Bucket = input[:slash]
	rest := input[slash+1:]

	lastSlash := strings.LastIndexByte(rest, '/')
	if lastSlash < 0 {
		// No additional slash past the bucket name. The entire remainder
		// is a leaf search under the bucket root.
		s.Prefix = ""
		s.Leaf = rest
		return s
	}

	// Prefix includes the trailing '/' so it is safe to hand directly to
	// ListObjectsV2 as a prefix.
	s.Prefix = rest[:lastSlash+1]
	s.Leaf = rest[lastSlash+1:]
	return s
}
