package s3

import "github.com/wmattei/scout/internal/services"

// Register adds every S3 provider (buckets, folders, objects) to the
// services registry. Called from cmd/scout at startup for commands
// that need AWS access. Cache-management subcommands skip this and
// avoid paying the registration cost.
func Register() {
	services.Register(&bucketProvider{})
	services.Register(&folderProvider{})
	services.Register(&objectProvider{})
}
