// Package version exposes release metadata injected with Go linker flags.
package version

var (
	// Version is the SemVer release or "dev".
	Version = "dev"
	// Commit is the source commit SHA.
	Commit = "unknown"
	// BuiltAt is the reproducible build timestamp.
	BuiltAt = "unknown"
)
