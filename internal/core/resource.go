// Package core defines the data types shared across the indexer, search,
// AWS adapters, and TUI layers. Nothing in this package imports from other
// internal packages — it is the root of the internal dependency graph.
package core

import "fmt"

// ResourceType enumerates the kinds of AWS resources better-aws knows about.
// Phase 1 only uses RTypeBucket, RTypeEcsService, and RTypeEcsTaskDefFamily.
// RTypeFolder and RTypeObject exist for later phases and are declared here so
// the TUI and index layers can pattern-match on the complete set.
type ResourceType int

const (
	RTypeBucket ResourceType = iota
	RTypeFolder
	RTypeObject
	RTypeEcsService
	RTypeEcsTaskDefFamily
)

// String returns a short machine name used in the SQLite schema's `type`
// column and in debug output. Stable — do not change without a migration.
func (r ResourceType) String() string {
	switch r {
	case RTypeBucket:
		return "bucket"
	case RTypeFolder:
		return "folder"
	case RTypeObject:
		return "object"
	case RTypeEcsService:
		return "ecs_service"
	case RTypeEcsTaskDefFamily:
		return "ecs_taskdef"
	default:
		return "unknown"
	}
}

// Tag is the short label shown as a colored chip in the TUI.
func (r ResourceType) Tag() string {
	switch r {
	case RTypeBucket:
		return "S3"
	case RTypeFolder:
		return "DIR"
	case RTypeObject:
		return "OBJ"
	case RTypeEcsService:
		return "ECS"
	case RTypeEcsTaskDefFamily:
		return "TASK"
	default:
		return "???"
	}
}

// Resource is the unified record for anything browsable in the TUI.
//
// Key uniquely identifies the resource within (profile, region, type). For
// buckets it is the bucket name; for ecs services it is the service ARN; for
// task def families it is the family name; for folders/objects it is the
// key path (with trailing '/' for folders).
//
// DisplayName is what the TUI renders and what the fuzzy matcher searches
// against. For most resources it equals Name; for ECS services we strip the
// ARN and keep the bare service name.
//
// Meta is a free-form bag carrying render hints (region, cluster, size,
// mtime). Values are strings to keep serialization trivial. Callers that
// need typed access parse on read.
type Resource struct {
	Type        ResourceType
	Key         string
	DisplayName string
	Meta        map[string]string
}

// ARN returns a canonical AWS ARN for the resource. For folders and objects
// a pseudo-ARN of the form arn:aws:s3:::<bucket>/<key> is used so the
// details panel can always show an "ARN" row. Phase 1 only calls this for
// buckets, services, and task def families — the folder/object branches are
// pre-wired for Phase 2.
func (r Resource) ARN() string {
	switch r.Type {
	case RTypeBucket:
		return fmt.Sprintf("arn:aws:s3:::%s", r.Key)
	case RTypeFolder, RTypeObject:
		bucket := r.Meta["bucket"]
		return fmt.Sprintf("arn:aws:s3:::%s/%s", bucket, r.Key)
	case RTypeEcsService:
		// Key is the full service ARN for ecs services.
		return r.Key
	case RTypeEcsTaskDefFamily:
		// Latest revision is resolved lazily in later phases; for Phase 1
		// we surface the family name so the details panel (when added) can
		// show "…resolving" until DescribeTaskDefinition returns.
		return fmt.Sprintf("arn:aws:ecs:*:*:task-definition/%s", r.Key)
	default:
		return ""
	}
}
