// Package services owns the per-resource-type abstraction and the
// global registry that every other layer uses to look up type-specific
// behaviour. Adding a new AWS resource type is "drop a new file that
// implements Provider, blank-import the package from main.go" — no
// edits to switch statements scattered across the codebase.
package services

import (
	"context"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/search"
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

	// ARN returns the canonical AWS ARN for the given resource. The
	// lazy map is the same one ResolveDetails populated; providers
	// that need resolved data to build a fuller ARN (like ECS task
	// def families, whose ARN includes the :revision suffix from a
	// DescribeTaskDefinition call) read from it. Providers that
	// don't need lazy state pass nil through — the map is always
	// safe to ignore. May return "" for types without a meaningful
	// ARN.
	ARN(r core.Resource, lazy map[string]string) string

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
	// first-entry refresh and the `scout preload` subcommand.
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

	// PollingInterval returns how often the Details view should
	// re-fire ResolveDetails while the user is looking at it.
	// Return 0 or a negative value to disable polling entirely
	// (the default via BaseProvider). A positive duration means the
	// handler schedules a background refresh every N seconds so
	// live-state fields (running task count, deployment rollout,
	// recent events) stay current without the user re-entering
	// Details. The in-flight poll does NOT flash the "resolving…"
	// placeholder — the current data stays visible until the fresh
	// result lands and overwrites it.
	PollingInterval() time.Duration

	// AlwaysRefresh reports whether the Details Enter handler should
	// fire ResolveDetails on every entry (ignoring any existing
	// resolved state), or stick with the default "resolve once per
	// session" cache. Types that show live AWS state — the ECS
	// service details page in particular — return true so the user
	// sees fresh running counts and deployment status every time
	// they open Details.
	AlwaysRefresh() bool

	// DetailRows returns the ordered list of label/value rows to
	// render in the Details panel under the shared Name + ARN
	// block. `lazy` is the map ResolveDetails populated; providers
	// that don't render any extra rows should return nil.
	// Return nil AND the in-flight state signals the view to render
	// a centered "resolving details…" placeholder.
	DetailRows(r core.Resource, lazy map[string]string) []DetailRow

	// Actions returns the ordered list of action definitions for this
	// resource type. The TUI maps each ActionDef.ID to an Execute
	// closure via a separate registry. Return nil for types that
	// have no actions.
	Actions() []ActionDef
}

// ActionDef describes an action the TUI can offer for a resource of
// this type. ID is a short machine name used as the key in the TUI's
// Execute registry. Label is the user-visible string.
type ActionDef struct {
	ID    string
	Label string
}

// DetailRow is one row in the Details panel's body. Label is rendered
// dim at a fixed column width; Value is rendered as-is (providers
// may embed lipgloss styling for color-coded health signals).
//
// Within the Metadata zone an empty Label with a non-empty Value
// renders as a section header; both empty inserts a blank spacer row.
// The Status and Events zones ignore Label entirely and render each
// row's Value on its own line.
type DetailRow struct {
	Label string
	Value string

	// Zone controls where this row renders in the zoned Details
	// layout. The zero value (ZoneMetadata) keeps existing providers
	// working unchanged.
	Zone DetailZone

	// Clickable marks this row as copyable-on-click. The Details
	// renderer styles it with an underlined dim-blue foreground and
	// registers a hit region in the mouse hit-map. Default false.
	Clickable bool

	// ClipboardValue is the exact string written to the clipboard
	// when a Clickable row is clicked. Empty means: strip ANSI
	// escapes from Value and copy that. Providers set this when the
	// rendered Value contains formatting or suffixes the user
	// doesn't want on their clipboard (e.g. colored status badges,
	// "(Xd ago)" human-time suffixes).
	ClipboardValue string
}

// DetailZone identifies which region of the zoned Details layout a
// DetailRow belongs to. The zero value is ZoneMetadata so existing
// providers that never assign a Zone keep rendering in the same
// top-right key/value bag they did before the zoned refactor.
type DetailZone int

const (
	// ZoneMetadata is the right-hand key/value bag. Default for all
	// DetailRows that don't explicitly pick a zone.
	ZoneMetadata DetailZone = iota
	// ZoneStatus is the top-center prominent-state box. Collapses
	// entirely when no provider rows target it.
	ZoneStatus
	// ZoneEvents is the bottom-right variable-length stream.
	// Collapses when empty.
	ZoneEvents
	// ZoneValue is the full-width middle row sitting between the
	// top row (Identity/Status/Metadata) and the bottom row
	// (Actions/Events). Reserved for a single large value — secret
	// bodies, decoded blobs — where the dense key/value layout of
	// Metadata would cramp readability. Collapses when empty.
	ZoneValue
)

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

func (BaseProvider) PollingInterval() time.Duration { return 0 }

func (BaseProvider) AlwaysRefresh() bool { return false }

func (BaseProvider) DetailRows(core.Resource, map[string]string) []DetailRow {
	return nil
}

func (BaseProvider) Actions() []ActionDef { return nil }
