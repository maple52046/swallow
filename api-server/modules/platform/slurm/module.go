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

// GPUProbeImage is the container image the deployment gate's GPU checks run (decision 083): a HIP probe
// built for every Instinct gfx target on the official ROCm development image, so a host needs only
// the amdgpu driver. It is declared in module.json's artifacts.images and pulled from where the
// Site's Local Repository mode sends it.
const GPUProbeImage = "ghcr.io/maple52046/swallow-gpu-probe:1.0.0-rocm7.2.4"
