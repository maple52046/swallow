package domain

// ModuleDeclaration is what one compiled module declares its deployments fetch, in the Local
// Repository's terms (the `artifacts` of its manifest, decision 075): Repository Sources, Files,
// container image references (fully qualified, "<registry host>/<path>:<tag>"), and Python
// requirements ("<name>==<version>"). ModuleID is the module's id, recorded as DeclaredByModule.
//
// The module registry has already refused declarations that contradict each other across modules
// (one file path with two URLs or checksums); merging them with the Site's spec is the Local
// Repository application's rule.
type ModuleDeclaration struct {
	ModuleID string
	Sources  []Source
	Files    []File
	Images   []string
	Python   []string
}
