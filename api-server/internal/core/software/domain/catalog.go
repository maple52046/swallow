package domain

import (
	"errors"
	"fmt"
	"sort"
)

// HookSoftware is the hook name a Software module lists in its module.json for its Software.
const HookSoftware = "software"

// Software is the hook a Software module implements (ADR 075): its kind's catalog entry and the
// logic data cannot express, which is validating and normalizing an install spec and turning it
// into the trusted variables the kind's playbook reads. No method performs I/O.
type Software interface {
	// Entry is the kind's catalog entry.
	Entry() CatalogEntry
	// ValidateSpec checks the kind's spec fields, given the roles each target takes; a failure
	// wraps ErrSpecInvalid.
	ValidateSpec(rolesByServer map[string][]Role, spec map[string]any) error
	// NormalizeSpec returns a copy of a validated spec with the kind's defaults applied. It is what
	// the playbook receives and what the assignment records, so the two never disagree.
	NormalizeSpec(spec map[string]any) map[string]any
	// InstallVars returns the kind's swallow_-prefixed trusted variables for a normalized spec. The
	// core adds swallow_software_kind and swallow_software_roles itself.
	InstallVars(rolesByServer map[string][]Role, spec map[string]any) map[string]any
}

// ServerReference is a Server that a spec field names.
type ServerReference struct {
	Field    string
	ServerID string
}

// ServerReferencer is implemented by a Software whose spec names other Servers. Each must be a
// Server of the target's Site, because the playbook resolves it from that Site's inventory.
type ServerReferencer interface {
	ServerReferences(spec map[string]any) []ServerReference
}

// RequiredProvisioningState is the provisioning state both Workflows of every kind require of
// their targets. Software deployment reaches a host over SSH, so install and uninstall need a
// booted, deployed OS; a Server that has left `deployed` has its assignments swept to `absent`, so
// uninstall is never the path for a wiped host.
const RequiredProvisioningState = "deployed"

// InstallWorkflowKind is the Workflow kind that installs the kind, "configure-<kind>".
func (k Kind) InstallWorkflowKind() string { return "configure-" + string(k) }

// InstallPlaybook is the playbook the install runs, "deploy-<kind>".
func (k Kind) InstallPlaybook() string { return "deploy-" + string(k) }

// UninstallWorkflowKind is the Workflow kind that removes the kind, "uninstall-<kind>".
func (k Kind) UninstallWorkflowKind() string { return "uninstall-" + string(k) }

// UninstallPlaybook is the playbook the removal runs, "uninstall-<kind>".
func (k Kind) UninstallPlaybook() string { return "uninstall-" + string(k) }

// ErrInvalidCatalog means the Software modules of a build do not form a consistent catalog.
var ErrInvalidCatalog = errors.New("invalid software catalog")

// Catalog is the Managed Software of a build: one Software per kind, from the Software modules the
// composition root registers. It is the domain source for the /software/catalog contract.
type Catalog struct {
	kinds  []Kind
	byKind map[Kind]Software
}

// NewCatalog validates the kinds together: each appears once with a label, every kind an entry
// names (mutually exclusive kinds, container runtimes) is in the catalog, and a kind with container
// runtimes offers the container install method.
func NewCatalog(software ...Software) (*Catalog, error) {
	catalog := &Catalog{byKind: make(map[Kind]Software, len(software))}
	for _, item := range software {
		entry := item.Entry()
		if entry.Kind == "" || entry.Label == "" {
			return nil, fmt.Errorf("%w: a kind needs an id and a label", ErrInvalidCatalog)
		}
		if _, exists := catalog.byKind[entry.Kind]; exists {
			return nil, fmt.Errorf("%w: kind %s is registered twice", ErrInvalidCatalog, entry.Kind)
		}
		catalog.byKind[entry.Kind] = item
		catalog.kinds = append(catalog.kinds, entry.Kind)
	}
	for _, kind := range catalog.kinds {
		entry := catalog.byKind[kind].Entry()
		for _, named := range append(append([]Kind(nil), entry.MutuallyExclusiveWith...), entry.ContainerRuntimes...) {
			if _, ok := catalog.byKind[named]; !ok {
				return nil, fmt.Errorf("%w: %s names kind %s, which is not in the catalog", ErrInvalidCatalog, kind, named)
			}
		}
		if len(entry.ContainerRuntimes) > 0 && !entry.Allows(InstallMethodContainer) {
			return nil, fmt.Errorf("%w: %s has container runtimes but no container install method", ErrInvalidCatalog, kind)
		}
	}
	sort.Slice(catalog.kinds, func(i, j int) bool { return catalog.kinds[i] < catalog.kinds[j] })
	return catalog, nil
}

// Entries returns every kind's catalog entry, ordered by kind.
func (c *Catalog) Entries() []CatalogEntry {
	out := make([]CatalogEntry, 0, len(c.kinds))
	for _, kind := range c.kinds {
		out = append(out, c.byKind[kind].Entry())
	}
	return out
}

// Lookup returns the Software of kind, or false when the kind is not in the catalog.
func (c *Catalog) Lookup(kind Kind) (Software, bool) {
	software, ok := c.byKind[kind]
	return software, ok
}

// Label is the operator-facing name of kind ("Docker CE"), or the kind itself when it is not in
// the catalog.
func (c *Catalog) Label(kind Kind) string {
	if software, ok := c.byKind[kind]; ok {
		return software.Entry().Label
	}
	return string(kind)
}
