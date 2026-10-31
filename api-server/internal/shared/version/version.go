// Package version exposes release metadata injected with Go linker flags (the production image
// build sets them). Any api-server package may read it; it must hold nothing but build metadata.
package version

var (
	// Version is the SemVer release or "dev".
	Version = "dev"
	// Commit is the source commit SHA.
	Commit = "unknown"
	// BuiltAt is the reproducible build timestamp.
	BuiltAt = "unknown"
)
