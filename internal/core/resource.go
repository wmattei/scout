// Package core defines the data types shared across the indexer, search,
// AWS adapters, and TUI layers. Nothing in this package imports from other
// internal packages — it is the root of the internal dependency graph.
package core

import (
	"sync"
)

// ResourceType enumerates the kinds of AWS resources scout knows about.
// The full set of types is declared here so all layers can match exhaustively.
type ResourceType int

const (
	RTypeBucket ResourceType = iota
	RTypeFolder
	RTypeObject
	RTypeEcsService
	RTypeEcsTaskDefFamily
	RTypeLambdaFunction
	RTypeSSMParameter
	RTypeSecretsManagerSecret
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
	case RTypeLambdaFunction:
		return "lambda_function"
	case RTypeSSMParameter:
		return "ssm_parameter"
	case RTypeSecretsManagerSecret:
		return "secrets_manager_secret"
	default:
		return "unknown"
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

// aliasRegistry is a process-global map from user-typed alias strings
// (e.g. "s3", "ecs", "td") to the ResourceType they resolve to. It is
// populated by the services package's Register function (which calls
// RegisterAlias for every alias on each Provider). Keeping this here
// in core rather than in services avoids the search→services import
// cycle that would arise if search/scope.go imported services directly.
var (
	aliasMu       sync.RWMutex
	aliasRegistry = map[string]ResourceType{}
)

// RegisterAlias adds an alias→type mapping. Called by services.Register
// for each alias on a Provider. Silently overwrites on duplicate (the
// services registry panics on duplicate aliases, so this is only ever
// called once per alias).
func RegisterAlias(alias string, t ResourceType) {
	aliasMu.Lock()
	defer aliasMu.Unlock()
	aliasRegistry[alias] = t
}

// LookupAlias returns the resource type registered under the given alias
// and a boolean reporting whether the lookup succeeded. Used by
// search/scope.go to resolve "<alias>:" prefixes without importing the
// services package (which would create an import cycle).
func LookupAlias(alias string) (ResourceType, bool) {
	aliasMu.RLock()
	defer aliasMu.RUnlock()
	t, ok := aliasRegistry[alias]
	return t, ok
}

