// Package nfs is the NFS Software module: its install and uninstall playbooks under this directory
// and its Software hook.
package nfs

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the NFS Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// NFS spec keys, published language of the software contract.
const (
	specExportPath    = "exportPath"
	specExportOptions = "exportOptions"
	specSource        = "source"
	specMountPath     = "mountPath"
	specMountOptions  = "mountOptions"
)

// Software is NFS's Software hook.
type Software struct{}

// Entry is NFS's catalog entry: a Server can be an NFS server, a client, or both.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:       softwaredomain.KindNFS,
		Label:      "NFS",
		Roles:      []softwaredomain.Role{softwaredomain.RoleServer, softwaredomain.RoleClient},
		SpecFields: []string{specExportPath, specExportOptions, specSource, specMountPath, specMountOptions},
	}
}

// ValidateSpec requires an export path when any target takes the server role, and a source and
// mount path when any target takes the client role.
func (Software) ValidateSpec(rolesByServer map[string][]softwaredomain.Role, spec map[string]any) error {
	servers, clients := serversWithRole(rolesByServer)
	if len(servers) > 0 && strings.TrimSpace(softwaredomain.SpecString(spec, specExportPath)) == "" {
		return fmt.Errorf("%w: an NFS server role requires spec.exportPath", softwaredomain.ErrSpecInvalid)
	}
	if len(clients) > 0 {
		if strings.TrimSpace(softwaredomain.SpecString(spec, specSource)) == "" {
			return fmt.Errorf("%w: an NFS client role requires spec.source", softwaredomain.ErrSpecInvalid)
		}
		if strings.TrimSpace(softwaredomain.SpecString(spec, specMountPath)) == "" {
			return fmt.Errorf("%w: an NFS client role requires spec.mountPath", softwaredomain.ErrSpecInvalid)
		}
	}
	return nil
}

// NormalizeSpec applies no defaults.
func (Software) NormalizeSpec(spec map[string]any) map[string]any {
	return softwaredomain.CloneSpec(spec)
}

// InstallVars names the server and client Servers and passes the export and mount settings.
func (Software) InstallVars(rolesByServer map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	servers, clients := serversWithRole(rolesByServer)
	vars := map[string]any{
		"swallow_nfs_server_ids": servers,
		"swallow_nfs_client_ids": clients,
	}
	softwaredomain.SetIfPresent(vars, "swallow_nfs_export_path", spec, specExportPath)
	softwaredomain.SetIfPresent(vars, "swallow_nfs_export_options", spec, specExportOptions)
	softwaredomain.SetIfPresent(vars, "swallow_nfs_client_source", spec, specSource)
	softwaredomain.SetIfPresent(vars, "swallow_nfs_client_mount_path", spec, specMountPath)
	softwaredomain.SetIfPresent(vars, "swallow_nfs_client_mount_options", spec, specMountOptions)
	return vars
}

// serversWithRole returns the sorted ids of the Servers that take the server role and of those that
// take the client role.
func serversWithRole(rolesByServer map[string][]softwaredomain.Role) (servers, clients []string) {
	servers, clients = []string{}, []string{}
	for serverID, roles := range rolesByServer {
		for _, role := range roles {
			switch role {
			case softwaredomain.RoleServer:
				servers = append(servers, serverID)
			case softwaredomain.RoleClient:
				clients = append(clients, serverID)
			}
		}
	}
	sort.Strings(servers)
	sort.Strings(clients)
	return servers, clients
}
