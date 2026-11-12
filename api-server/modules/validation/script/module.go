// Package script is the script Validation Module: the run-script template and playbook that run every
// user-defined Validation Suite, judging the suite's one check by the script's exit code
// (decision 081). It declares no suite of its own; user suites are data in the store.
package script

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the script Validation Module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
