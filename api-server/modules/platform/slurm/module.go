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
		platformdomain.HookPlatform:       Platform{},
	})
}

// ID is the Slurm module's id.
const ID = "slurm"

// GPUTestImage is the container image the acceptance suite's container GPU check runs (decision 083):
// AMD's official ROCm development image, in which the check builds its HIP probe with hipcc for
// gfx90a, gfx942, and gfx950, so a host needs only the amdgpu driver. It is user workload, like the
// images jobs run, so it is not a module artifact: each GPU node pulls it straight from its registry,
// never through the Local Repository, and a node that cannot pull it skips the check with a warning.
const GPUTestImage = "docker.io/rocm/dev-ubuntu-24.04:7.2.4"
