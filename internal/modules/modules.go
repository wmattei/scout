// Package modules wires every scout feature module into the process
// registry. Adding a new module means creating its package under
// internal/modules/<name>/ and adding a call here.
package modules

import "github.com/wmattei/scout/internal/module"

// RegisterAll populates the registry. Called from cmd/scout at
// startup for commands that need module awareness (runTUI,
// preload). cache-clear skips it.
func RegisterAll(r *module.Registry) {
	// Registrations land here as each module is migrated in Phase 2:
	//   r.Register(lambda.New())
	//   r.Register(ssm.New())
	//   ...
	_ = r
}
