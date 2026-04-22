package secretsmanager

const (
	MetaDescription    = "description"
	MetaLastChanged    = "lastChanged"
	MetaLastAccessed   = "lastAccessed"
	MetaLastRotated    = "lastRotated"
	MetaRotationEnabled = "rotationEnabled"
	MetaKmsKeyID       = "kmsKeyId"
	MetaARN            = "arn"
	MetaVersionID      = "versionId"

	// SensitiveRevealedKey is a UI-only sentinel written into the lazy
	// details map when the user has explicitly revealed a secret's
	// value. It is not produced by ResolveDetails; it is toggled by the
	// "reveal-secret-value" action and wiped whenever lazyDetails is
	// refetched (on refresh or re-entry into Details).
	SensitiveRevealedKey = "_revealed"
)
