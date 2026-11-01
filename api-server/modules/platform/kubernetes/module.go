// Package kubernetes is the Kubernetes Platform module (k0s): the deploy, uninstall, and exporter
// playbooks under this directory, its cluster API client in kubeapi, and its Go hooks.
package kubernetes

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
)

//go:embed module.json
var manifest []byte

// Module returns the Kubernetes Platform module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{
		platformdomain.HookPlatformReader: kubeapi.ReaderBuilder{},
		platformdomain.HookCredential:     kubeapi.CredentialReader{},
		platformdomain.HookPlatform:       Platform{},
	})
}
