// Package prefs owns the per-(profile, region) user preferences store:
// favorites (resources the user pinned with `f`) and recents (the
// last-10 resources whose Details view was entered). Stored in a
// separate SQLite file from the AWS resource cache so `scout cache
// clear` doesn't wipe user state.
package prefs

import (
	"time"

	"github.com/wmattei/scout/internal/core"
)

// typeKey is the primary-key composite used internally by State maps
// so different resource types can't collide on the same string key.
type typeKey struct {
	Type core.ResourceType
	Key  string
}

// FavoriteRow is a single row in the favorites list returned to the
// TUI. Name is the DisplayName snapshot taken at insert time so the
// UI can render a row even when the live resource is missing from the
// in-memory cache (e.g. cache not yet populated, or resource deleted
// from AWS).
type FavoriteRow struct {
	Type      core.ResourceType
	Key       string
	Name      string
	CreatedAt time.Time
}

// RecentRow is a single row in the recents list. Same fields as
// FavoriteRow plus the visit timestamp; the TUI orders recents by
// VisitedAt descending.
type RecentRow struct {
	Type      core.ResourceType
	Key       string
	Name      string
	VisitedAt time.Time
}

// parseType maps the SQLite `type` column back to core.ResourceType.
// Mirrors internal/index/db.go's parseType; duplicated here to avoid
// importing internal/index (no cycle today but keeps prefs genuinely
// independent).
//
// TODO: once a third caller appears, promote this to
// core.ParseResourceType(s) (ResourceType, bool) so unknown types
// can be rejected rather than silently coerced to RTypeBucket. For
// this package, the schema-version drop-and-recreate bounds the
// window where an unknown type could land in storage.
func parseType(s string) core.ResourceType {
	switch s {
	case "bucket":
		return core.RTypeBucket
	case "folder":
		return core.RTypeFolder
	case "object":
		return core.RTypeObject
	case "ecs_service":
		return core.RTypeEcsService
	case "ecs_taskdef":
		return core.RTypeEcsTaskDefFamily
	case "lambda_function":
		return core.RTypeLambdaFunction
	case "ssm_parameter":
		return core.RTypeSSMParameter
	default:
		return core.RTypeBucket
	}
}
