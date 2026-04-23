// Package s3 is the S3 module. Collapses buckets, folders, and
// objects into a single module with stateful drill-in — the query
// string itself encodes drill position: "" = bucket list, "<bucket>/"
// = bucket root, "<bucket>/path/to/" = deeper drill.
//
// Rows stored in the shared cache use synthetic keys so one table
// serves all three logical row kinds:
//   - Buckets:  Key = "b:<name>"
//   - Folders:  Key = "f:<bucket>/<prefix>"
//   - Objects:  Key = "o:<bucket>/<full-key>"
package s3

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	awss3 "github.com/wmattei/scout/internal/awsctx/s3"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

const packageID = "s3"

// Row-kind prefixes on Key.
const (
	kindBucket = "b:"
	kindFolder = "f:"
	kindObject = "o:"
)

// MetaKind distinguishes row types for Actions/ResolveDetails/etc.
const MetaKind = "kind"

type Module struct{}

func New() *Module { return &Module{} }

func (Module) Manifest() module.Manifest {
	return module.Manifest{
		ID:      packageID,
		Name:    "S3 Buckets",
		Aliases: []string{"s3", "buckets"},
		Tag:     "S3 ",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#5FAFFF"}),
		SortPriority: 1,
	}
}

func (Module) PollingInterval() time.Duration { return 0 }
func (Module) AlwaysRefresh() bool             { return false }

var _ module.Module = (*Module)(nil)

// ---- HandleSearch / drill-in ----

func (m *Module) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	bucket, prefix, drilling := parseDrill(query)
	if !drilling {
		return bucketMode(ctx, query, state)
	}
	return drillMode(ctx, bucket, prefix, state)
}

// parseDrill returns (bucket, prefix, true) when the query contains
// at least one "/". Otherwise returns (query, "", false) — bucket
// list mode.
func parseDrill(query string) (bucket, prefix string, drilling bool) {
	idx := strings.Index(query, "/")
	if idx < 0 {
		return query, "", false
	}
	return query[:idx], query[idx+1:], true
}

func bucketMode(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	listed := len(state.Bytes) > 0 && state.Bytes[0]&1 != 0

	rows := readBuckets(ctx, query)
	if listed {
		return rows, state, nil
	}
	newState := effect.State{Bytes: []byte{1}}
	effects := []effect.Effect{
		effect.Async{
			Label: "listing s3 buckets",
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				raw, err := awss3.ListBuckets(c, ctx.AWSCtx, awsctx.ListOptions{})
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("s3 list failed: %v", err), Level: effect.LevelError}
				}
				return effect.UpsertCache{Rows: bucketsToRows(raw)}
			},
		},
	}
	return rows, newState, effects
}

func drillMode(ctx module.Context, bucket, prefix string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	// Always fire the refresh — S3 contents change out-of-band and the
	// cache is just a warm-start optimization.
	rows := readDrill(ctx, bucket, prefix)
	effects := []effect.Effect{
		effect.Async{
			Label: "listing " + bucket + "/" + prefix,
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				raw, err := awss3.ListAtPrefix(c, ctx.AWSCtx, bucket, prefix, 50)
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("s3 drill failed: %v", err), Level: effect.LevelError}
				}
				return effect.UpsertCache{Rows: drillToRows(bucket, raw)}
			},
		},
	}
	return rows, state, effects
}

func readBuckets(ctx module.Context, query string) []core.Row {
	if ctx.Cache == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	all, err := ctx.Cache.Query(qctx, packageID, kindBucket)
	if err != nil {
		return nil
	}
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	out := make([]core.Row, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}

func readDrill(ctx module.Context, bucket, prefix string) []core.Row {
	if ctx.Cache == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	folderRows, _ := ctx.Cache.Query(qctx, packageID, kindFolder+bucket+"/"+prefix)
	objectRows, _ := ctx.Cache.Query(qctx, packageID, kindObject+bucket+"/"+prefix)
	// Filter to direct children only (skip deeper nesting under prefix).
	direct := make([]core.Row, 0, len(folderRows)+len(objectRows))
	for _, r := range folderRows {
		if isDirectChild(r.Meta["objkey"], prefix) {
			direct = append(direct, r)
		}
	}
	for _, r := range objectRows {
		if isDirectChild(r.Meta["objkey"], prefix) {
			direct = append(direct, r)
		}
	}
	return direct
}

// isDirectChild reports whether objKey is an immediate child of
// prefix — i.e. objKey starts with prefix and the remainder has no
// additional "/" (except a trailing one for folders).
func isDirectChild(objKey, prefix string) bool {
	if !strings.HasPrefix(objKey, prefix) {
		return false
	}
	rest := strings.TrimPrefix(objKey, prefix)
	rest = strings.TrimSuffix(rest, "/")
	return !strings.Contains(rest, "/")
}

func bucketsToRows(raw []core.Resource) []core.Row {
	out := make([]core.Row, 0, len(raw))
	for _, r := range raw {
		meta := map[string]string{MetaKind: "bucket", "bucket": r.Key}
		for k, v := range r.Meta {
			meta[k] = v
		}
		out = append(out, core.Row{
			PackageID: packageID,
			Key:       kindBucket + r.Key,
			Name:      r.Key,
			Meta:      meta,
		})
	}
	return out
}

func drillToRows(bucket string, raw []core.Resource) []core.Row {
	out := make([]core.Row, 0, len(raw))
	for _, r := range raw {
		full := r.Key
		kindTag := "object"
		keyPrefix := kindObject
		if strings.HasSuffix(full, "/") {
			kindTag = "folder"
			keyPrefix = kindFolder
		}
		meta := map[string]string{
			MetaKind: kindTag,
			"bucket": bucket,
			"objkey": full,
		}
		for k, v := range r.Meta {
			meta[k] = v
		}
		out = append(out, core.Row{
			PackageID: packageID,
			Key:       keyPrefix + bucket + "/" + full,
			Name:      r.DisplayName,
			Meta:      meta,
		})
	}
	return out
}

// ---- Identity helpers ----

func (Module) ARN(r core.Row) string {
	switch r.Meta[MetaKind] {
	case "bucket":
		return "arn:aws:s3:::" + r.Meta["bucket"]
	case "object":
		return "arn:aws:s3:::" + r.Meta["bucket"] + "/" + r.Meta["objkey"]
	}
	return ""
}

func (Module) ConsoleURL(r core.Row, region string) string {
	bucket := r.Meta["bucket"]
	switch r.Meta[MetaKind] {
	case "bucket":
		return "https://s3.console.aws.amazon.com/s3/buckets/" + url.PathEscape(bucket) + "?region=" + region
	case "folder":
		return "https://s3.console.aws.amazon.com/s3/buckets/" + url.PathEscape(bucket) +
			"?region=" + region + "&prefix=" + url.QueryEscape(r.Meta["objkey"])
	case "object":
		return "https://s3.console.aws.amazon.com/s3/object/" + url.PathEscape(bucket) +
			"?region=" + region + "&prefix=" + url.QueryEscape(r.Meta["objkey"])
	}
	return ""
}

// ---- Details ----

func (m *Module) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	switch r.Meta[MetaKind] {
	case "bucket":
		return effect.Async{
			Label: "describing bucket",
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				d, err := awss3.DescribeBucket(c, ctx.AWSCtx, r.Meta["bucket"])
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("describe bucket failed: %v", err), Level: effect.LevelError}
				}
				lazy := map[string]string{
					"versioning":   d.Versioning,
					"encryption":   d.Encryption,
					"publicAccess": d.PublicAccess,
					"tags":         strings.Join(d.Tags, "\n"),
				}
				return effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
			},
		}
	case "object":
		return effect.Async{
			Label: "heading object",
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				size, err := awss3.HeadObject(c, ctx.AWSCtx, r.Meta["bucket"], r.Meta["objkey"])
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("head object failed: %v", err), Level: effect.LevelError}
				}
				lazy := map[string]string{
					"size":   fmt.Sprintf("%d", size),
					"bucket": r.Meta["bucket"],
					"key":    r.Meta["objkey"],
				}
				return effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
			},
		}
	}
	return effect.None{}
}

func (Module) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	switch r.Meta[MetaKind] {
	case "bucket":
		if lazy == nil {
			return module.DetailZones{Metadata: widget.Raw{Content: "resolving…"}}
		}
		return module.DetailZones{
			Status: widget.KeyValue{
				Rows: []widget.KVRow{
					{Label: "Versioning", Value: lazy["versioning"]},
					{Label: "Encryption", Value: lazy["encryption"]},
					{Label: "Public access", Value: lazy["publicAccess"]},
				},
			},
			Metadata: widget.Raw{Content: lazy["tags"]},
		}
	case "folder":
		return module.DetailZones{
			Metadata: widget.KeyValue{
				Rows: []widget.KVRow{
					{Label: "Bucket", Value: r.Meta["bucket"]},
					{Label: "Prefix", Value: r.Meta["objkey"]},
				},
			},
		}
	case "object":
		return module.DetailZones{
			Status: widget.KeyValue{
				Rows: []widget.KVRow{
					{Label: "Size", Value: lazyOrMeta(lazy, r, "size")},
				},
			},
			Metadata: widget.KeyValue{
				Rows: []widget.KVRow{
					{Label: "Bucket", Value: r.Meta["bucket"]},
					{Label: "Key", Value: r.Meta["objkey"]},
				},
			},
		}
	}
	return module.DetailZones{}
}

func lazyOrMeta(lazy map[string]string, r core.Row, key string) string {
	if v, ok := lazy[key]; ok {
		return v
	}
	return r.Meta[key]
}

// ---- Actions ----

func (m *Module) Actions(r core.Row) []module.Action {
	actions := []module.Action{
		{
			Label: "Open in Browser",
			Run: func(ctx module.Context, row core.Row) effect.Effect {
				return effect.Browser{URL: m.ConsoleURL(row, ctx.AWSCtx.Region)}
			},
		},
	}
	switch r.Meta[MetaKind] {
	case "bucket", "object":
		actions = append(actions, module.Action{
			Label: "Copy ARN",
			Run: func(ctx module.Context, row core.Row) effect.Effect {
				return effect.Copy{Text: m.ARN(row), Label: "ARN"}
			},
		})
	}
	if r.Meta[MetaKind] == "object" || r.Meta[MetaKind] == "folder" || r.Meta[MetaKind] == "bucket" {
		actions = append(actions, module.Action{
			Label: "Copy URI",
			Run: func(ctx module.Context, row core.Row) effect.Effect {
				uri := "s3://" + row.Meta["bucket"]
				if k := row.Meta["objkey"]; k != "" {
					uri += "/" + k
				}
				return effect.Copy{Text: uri, Label: "S3 URI"}
			},
		})
	}
	return actions
}

func (Module) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	return effect.None{}
}
