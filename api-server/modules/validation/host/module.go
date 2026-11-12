// Package host is the host Validation Module: the host-baseline suite, which checks each Server's
// clock, CPUs, memory, and root filesystem with the playbook under this directory.
package host

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the host Validation Module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
