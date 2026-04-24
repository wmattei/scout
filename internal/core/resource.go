package core

// Resource is the shape returned by the AWS adapters in internal/awsctx/*.
// Modules map Resource records into core.Row via their per-module toRows
// helpers. The TUI and cache layers never see a Resource directly —
// only core.Row.
//
// Key uniquely identifies the resource within its module. DisplayName is
// a short human-friendly label (typically the bare name). Meta is a
// free-form per-module bag carrying render hints (region, cluster, size,
// mtime). Values are strings to keep serialization trivial.
type Resource struct {
	Key         string
	DisplayName string
	Meta        map[string]string
}
