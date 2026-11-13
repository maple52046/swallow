// Package sssd is the directory client Software module (decision 082): it makes a Server read its
// accounts, SSH public keys, Host Access, and sudo rules from the installation's Directory through
// SSSD, so a User logs in with their Access Keys and gets the sudo the Sudo Policy gives them, with
// no account or key written to the Server. Its install and uninstall playbooks live under this
// directory; the Directory settings arrive from the core, which marks the catalog entry a
// directory client.
package sssd

import (
	_ "embed"
	"fmt"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Kind is the Managed Software kind this module provides. The worker's directory client controller
// keeps it installed on every covered Server, so the composition root names it.
const Kind softwaredomain.Kind = "sssd"

// Module returns the directory client Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is the directory client's Software hook.
type Software struct{}

// Entry is the directory client's catalog entry: no spec, the OS packages decide the version, and
// the install takes the Directory's settings, which the core adds because DirectoryClient is set.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:            Kind,
		Label:           "Directory client (SSSD)",
		SpecFields:      []string{},
		DirectoryClient: true,
	}
}

// ValidateSpec refuses every field: everything the client needs comes from the Directory.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if len(spec) > 0 {
		return fmt.Errorf("%w: %s takes no spec fields", softwaredomain.ErrSpecInvalid, s.Entry().Kind)
	}
	return nil
}

// NormalizeSpec records an empty spec.
func (Software) NormalizeSpec(map[string]any) map[string]any {
	return map[string]any{}
}

// InstallVars adds nothing: the core adds the Directory's settings to every directory client
// install.
func (Software) InstallVars(map[string][]softwaredomain.Role, map[string]any) map[string]any {
	return map[string]any{}
}
