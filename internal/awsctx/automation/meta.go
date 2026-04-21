package automation

// Meta keys shared between the adapter and provider packages. Values
// stored in core.Resource.Meta are plain strings; multi-value fields
// (parameters, executions) carry JSON-encoded slice/struct payloads
// that the provider unmarshals on render.
const (
	MetaOwner         = "owner"
	MetaVersionName   = "versionName"
	MetaDocumentType  = "documentType"
	MetaLatestVersion = "latestVersion"
	MetaDescription   = "description"
	MetaPlatformTypes = "platformTypes"
	MetaTargetType    = "targetType"
	MetaParameters    = "parameters" // JSON-encoded []ParameterInfo
	MetaExecutions    = "executions" // JSON-encoded []ExecutionInfo
	MetaContent       = "content"    // raw document content (YAML or JSON)
	MetaContentFormat = "contentFormat"
)
