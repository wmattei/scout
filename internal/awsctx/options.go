package awsctx

// ListOptions holds the optional knobs every list-style adapter
// supports. The zero value means "no limit, no prefix filter" and
// matches the historical behaviour, so existing callers that don't
// care about either knob can pass `awsctx.ListOptions{}` and get
// the unchanged path.
//
// Limit is interpreted as an upper bound on the number of items the
// adapter returns. Adapters that have a native MaxResults / MaxBuckets
// parameter pass it server-side; the others enforce the cap client-
// side via an early break out of their pagination loop.
//
// Prefix is interpreted as a name-prefix filter. S3 buckets and ECS
// task-def families pass it server-side via the SDK input fields;
// ECS services have no native prefix support and apply the filter to
// each service name client-side after DescribeServices.
type ListOptions struct {
	Limit  int    // 0 means "no limit"
	Prefix string // "" means "no prefix filter"
}
