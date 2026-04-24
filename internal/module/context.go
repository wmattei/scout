// Package module defines the contract every scout feature module
// implements. The Module interface shields core from feature-specific
// code; adding a new AWS service means creating a module package and
// registering it in one line.
package module

import (
	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/cache"
	"github.com/wmattei/scout/internal/effect"
)

// Context is threaded to every module entry point. AWSCtx carries
// credentials and region; Cache is a read-only handle for cross-
// module peeks (e.g. the S3 module checking whether a bucket name
// matches something cached elsewhere); State is a read-only view of
// the module's own opaque state (mutations go through effect.SetState).
type Context struct {
	AWSCtx *awsctx.Context
	Cache  *cache.DB
	State  effect.State
}
