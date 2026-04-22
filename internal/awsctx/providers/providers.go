// Package providers wires every AWS provider package into the services
// registry. Callers that need AWS access (the TUI, preload) invoke
// RegisterAll at startup; callers that don't (cache-management
// subcommands) skip it and avoid both the registration cost and the
// dependency on every provider's code.
//
// This package lives alongside the provider packages rather than in
// cmd/scout so it can import all of them without being mistaken for a
// CLI subcommand, and so the wiring lives near the code it wires.
//
// Adding a new AWS service: create the provider package with an
// exported Register() function, then add one call here.
package providers

import (
	"github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/awsctx/ecs"
	"github.com/wmattei/scout/internal/awsctx/lambda"
	"github.com/wmattei/scout/internal/awsctx/s3"
	"github.com/wmattei/scout/internal/awsctx/secretsmanager"
	"github.com/wmattei/scout/internal/awsctx/ssm"
)

// RegisterAll populates the services registry with every AWS provider
// known to scout. Safe to call more than once — each underlying
// services.Register handles its own idempotency.
func RegisterAll() {
	s3.Register()
	ecs.Register()
	lambda.Register()
	ssm.Register()
	secretsmanager.Register()
	automation.Register()
}
