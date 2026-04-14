package search

import "strings"

// Scope describes how the TUI should interpret the current input value.
//
// A top-level scope (Bucket == "") means the search engine should run
// fuzzy against the in-memory index of cached S3 buckets, ECS services,
// and ECS task-def families.
//
// A scoped value (Bucket != "") means the search should be a prefix-based
// lookup inside that S3 bucket. Prefix is the full key prefix passed to
// ListObjectsV2 (always ending on a `/` boundary or at the very start).
// Leaf is the characters after the last `/` in the input — the part that
// gets highlighted in result rows.
type Scope struct {
	Raw    string // the original input string, verbatim
	Bucket string
	Prefix string
	Leaf   string
}

// IsTopLevel reports whether the scope has no bucket selected yet.
func (s Scope) IsTopLevel() bool { return s.Bucket == "" }

// ParseScope converts an input string into a Scope.
//
// Examples:
//
//	""                          -> top-level, Leaf=""
//	"my-bu"                     -> top-level, Leaf="my-bu"
//	"my-bucket/"                -> bucket=my-bucket, Prefix="", Leaf=""
//	"my-bucket/logs/"           -> bucket=my-bucket, Prefix="logs/", Leaf=""
//	"my-bucket/logs/2026"       -> bucket=my-bucket, Prefix="logs/", Leaf="2026"
//	"my-bucket/logs/2026/file"  -> bucket=my-bucket, Prefix="logs/2026/", Leaf="file"
func ParseScope(input string) Scope {
	s := Scope{Raw: input}

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
