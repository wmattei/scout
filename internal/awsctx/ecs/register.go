package ecs

import "github.com/wmattei/scout/internal/services"

// Register adds every ECS provider (services, task-def families) to
// the services registry. Called from cmd/scout at startup for commands
// that need AWS access. Cache-management subcommands skip this and
// avoid paying the registration cost.
func Register() {
	services.Register(&ecsServiceProvider{})
	services.Register(&ecsTaskDefProvider{})
}
