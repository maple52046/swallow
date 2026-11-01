// Package slurm is the Slurm Platform module: the deploy and uninstall playbooks under this directory,
// its slurmrestd client in slurmapi, and its Go hooks.
package slurm

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
)

//go:embed module.json
var manifest []byte

// Module returns the Slurm Platform module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{
		platformdomain.HookPlatformReader: slurmapi.ReaderBuilder{},
		platformdomain.HookCredential:     slurmapi.CredentialReader{},
	})
}
