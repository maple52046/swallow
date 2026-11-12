// Package network is the network Validation Module: the network-mesh suite, which probes the paths
// between the target Servers with the playbook and role under this directory.
package network

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the network Validation Module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
