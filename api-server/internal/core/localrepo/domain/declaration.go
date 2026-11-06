package domain

// ModuleDeclaration is what one compiled module declares its deployments fetch, in the Local
// Repository's terms (the `artifacts` of its manifest, decision 075): Repository Sources, Files,
// container image references (fully qualified, "<registry host>/<path>:<tag>"), and Python
// requirements ("<name>==<version>"). ModuleID is the module's id, recorded as DeclaredByModule.
// SigningKeys holds, by source name, the key each of Sources of an OS package format (deb, rpm)
// is signed with; the module registry requires one for each such source.
//
// The module registry has already refused declarations that contradict each other across modules
// (one file path with two URLs or checksums, one source with two signing keys); merging them with
// the Site's spec is the Local Repository application's rule.
type ModuleDeclaration struct {
	ModuleID    string
	Sources     []Source
	SigningKeys map[string]SigningKey
	Files       []File
	Images      []string
	Python      []string
}

// SigningKey is the public key a module declares an upstream OS package source's metadata is
// signed with: URL, an https address a host downloads it from, and Fingerprint, its 40 upper-case
// hex digits, which the host verifies the download against. Only a deployment that fetches from
// the upstreams uses it; the Local Repository signs what it serves with its own key. The zero
// SigningKey means none was declared (a user's source).
type SigningKey struct {
	URL         string
	Fingerprint string
}
