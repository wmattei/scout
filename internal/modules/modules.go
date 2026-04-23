// Package modules wires every scout feature module into the process
// registry. Adding a new module means creating its package under
// internal/modules/<name>/ and adding a call here.
package modules

import (
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/modules/lambda"
	"github.com/wmattei/scout/internal/modules/secrets"
	"github.com/wmattei/scout/internal/modules/ssm"
)

// RegisterAll populates the registry. Called from cmd/scout at
// startup for commands that need module awareness (runTUI,
// preload). cache-clear skips it.
func RegisterAll(r *module.Registry) {
	r.Register(lambda.New())
	r.Register(ssm.New())
	r.Register(secrets.New())
}
