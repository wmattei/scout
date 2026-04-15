// Package services owns the per-resource-type abstraction and the
// global registry that every other layer uses to look up type-specific
// behaviour. Adding a new AWS resource type is "drop a new file that
// implements Provider, blank-import the package from main.go" — no
// edits to switch statements scattered across the codebase.
package services

import (
	"context"

	"github.com/charmbracelet/lipgloss"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// Provider is the per-resource-type strategy interface. Every method
// either describes the type ("what tag color does this use?", "what
// aliases trigger a manual scope?") or operates on a single Resource
// of that type ("build a console URL for this row", "what's the next
// scope after Tab on this row?").
//
// Implementations are typically tiny structs (no state) that live
// next to the SDK adapters they wrap, e.g.
// internal/awsctx/s3/provider_buckets.go. Each implementation file
// has an init() that calls services.Register.
type Provider interface {
	// --- Identity --------------------------------------------------

	// Type returns the core.ResourceType this provider is registered
	// under. The registry uses this as the lookup key.
	Type() core.ResourceType

	// Aliases returns the set of "<alias>:" strings the user can
	// type in the input bar to scope-search this resource type.
	// May be empty for types that aren't directly addressable from
	// the top level (e.g. S3 folder/object providers, which the
	// user reaches by drilling into a bucket).
	Aliases() []string

	// TagLabel is the short label rendered inside the colored chip
	// at the start of every result row (e.g. "S3", "ECS", "TASK").
	TagLabel() string

	// TagStyle returns the lipgloss style used to render TagLabel.
	// Providers own their own colors so the styles package never
	// needs a per-type switch.
	TagStyle() lipgloss.Style

	// SortPriority controls the order this type appears in mixed
	// top-level result lists (lower = earlier). Stable across runs.
	SortPriority() int

	// IsTopLevel reports whether resources of this type should be
	// included in the unified top-level fuzzy index returned by
	// index.Memory.All(). S3 buckets, ECS services, and ECS
	// task-def families are top-level today; folders and objects
	// are not (they are reached via S3 drill-in).
	IsTopLevel() bool

	// --- Per-resource accessors -----------------------------------

	// ARN returns the canonical AWS ARN for the given resource.
	// May return "" for types without a meaningful ARN.
	ARN(r core.Resource) string

	// URI returns the user-facing URI form for the Copy URI action.
	// The bool is false when the type has no URI form (e.g. ECS
	// services don't have a URI scheme).
	URI(r core.Resource) (string, bool)

	// ConsoleURL builds an AWS web-console deep link for the
	// resource. `lazy` is the per-resource lazy-details map (see
	// ResolveDetails) so providers like the ECS task-def one can
	// substitute the resolved revision into the URL.
	ConsoleURL(r core.Resource, region string, lazy map[string]string) string

	// RenderMeta returns the right-aligned meta column for a result
	// row in PLAIN string form (no styling). The caller wraps the
	// return value in styleRowDim. Provider must not embed lipgloss
	// styles in the returned string — the caller owns the dim look.
	RenderMeta(r core.Resource) string

	// TabComplete returns the new value the input bar should take
	// when the user hits Tab on a row of this type. `scope` is the
	// current parsed input so providers can reach across scopes
	// (folder/object providers prepend the bucket name from
	// scope.Bucket). The default in BaseProvider is to return
	// row.DisplayName, which works for leaf rows.
	TabComplete(scope search.Scope, r core.Resource) string

	// --- Live data ------------------------------------------------

	// ListAll fetches every resource of this type that matches the
	// optional ListOptions filters. Used by both the manual scope
	// first-entry refresh and the `better-aws preload` subcommand.
	// Providers for derived/scoped types (S3 folders, S3 objects)
	// return (nil, nil) — they have no top-level list semantics.
	ListAll(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error)

	// --- Lazy details --------------------------------------------

	// ResolveDetails is called once when the user enters the
	// Details view for a resource of this type. The returned map
	// gets stored in the model's lazyDetails store keyed by
	// (type, resource key). Return (nil, nil) if the type has no
	// extra details to resolve. The map is opaque to the TUI; it
	// only flows back into ConsoleURL / LogGroup hooks on the
	// same provider.
	ResolveDetails(ctx context.Context, ac *awsctx.Context, r core.Resource) (map[string]string, error)

	// LogGroup returns the CloudWatch log group name to tail when
	// the user activates Tail Logs on this resource. The provider
	// reads from the same lazy map ResolveDetails populated. Return
	// "" when no log group is configured (the action shows an
	// informational toast).
	LogGroup(r core.Resource, lazy map[string]string) string
}

// BaseProvider is an embeddable zero-default that gives providers
// sensible no-op implementations of the optional methods. Embed it
// to avoid hand-writing five empty method stubs per provider:
//
//	type bucketProvider struct{ services.BaseProvider }
//
// Then override only the methods that aren't no-ops.
type BaseProvider struct{}

func (BaseProvider) URI(core.Resource) (string, bool) { return "", false }

func (BaseProvider) TabComplete(_ search.Scope, r core.Resource) string {
	// Default to dropping the selected row's display name back into
	// the input bar — works for ECS services / task defs and any
	// other leaf type.
	return r.DisplayName
}

func (BaseProvider) ResolveDetails(context.Context, *awsctx.Context, core.Resource) (map[string]string, error) {
	return nil, nil
}

func (BaseProvider) LogGroup(core.Resource, map[string]string) string { return "" }
