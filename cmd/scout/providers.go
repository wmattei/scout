package main

import (
	"github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/awsctx/ecs"
	"github.com/wmattei/scout/internal/awsctx/lambda"
	"github.com/wmattei/scout/internal/awsctx/s3"
	"github.com/wmattei/scout/internal/awsctx/secretsmanager"
	"github.com/wmattei/scout/internal/awsctx/ssm"
)

// registerAWSProviders populates the services registry with every
// AWS provider known to scout. Called by subcommands that need AWS
// access (runTUI, preload); deliberately NOT called by purely-local
// subcommands like `cache clear` so they don't pay the registration
// cost or declare a dependency they don't use.
//
// Adding a new AWS service means: create the provider package with a
// Register() function, then add a call here.
func registerAWSProviders() {
	s3.Register()
	ecs.Register()
	lambda.Register()
	ssm.Register()
	secretsmanager.Register()
	automation.Register()
}
