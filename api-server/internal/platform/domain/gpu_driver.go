package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// GPUDriverSettings is the operator's amdgpu driver intent for a Platform's GPU nodes (decision
// 063). ModuleOptions are module parameters applied before the driver loads; Swallow adds the
// options a topology rule derives on each node (for example pcie_p2p=0 when KFD reports duplicate
// peer links), and an option named here always wins over a rule. No field names a GPU model or a
// virtualization mode.
type GPUDriverSettings struct {
	ModuleOptions map[string]string
}

// Empty reports whether the settings request nothing.
func (s GPUDriverSettings) Empty() bool {
	return len(s.ModuleOptions) == 0
}

// MaxGPUDriverModuleOptions bounds the options one Platform may request.
const MaxGPUDriverModuleOptions = 16

var (
	gpuDriverOptionName  = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	gpuDriverOptionValue = regexp.MustCompile(`^[A-Za-z0-9_.,:-]{1,128}$`)
)

// NormalizeGPUDriverSettings trims names and values and validates them: a name is lower-case
// letters, digits, and underscores; a value is letters, digits, and "_.,:-". The grammar keeps an
// option on one modprobe.d line and out of any shell. The result is never nil.
func NormalizeGPUDriverSettings(settings GPUDriverSettings) (GPUDriverSettings, error) {
	if len(settings.ModuleOptions) > MaxGPUDriverModuleOptions {
		return GPUDriverSettings{}, fmt.Errorf("%w: at most %d moduleOptions, got %d", ErrInvalidGPUDriver,
			MaxGPUDriverModuleOptions, len(settings.ModuleOptions))
	}
	options := make(map[string]string, len(settings.ModuleOptions))
	for name, value := range settings.ModuleOptions {
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !gpuDriverOptionName.MatchString(name) {
			return GPUDriverSettings{}, fmt.Errorf("%w: moduleOptions name %q must be lower-case letters, digits, or underscores", ErrInvalidGPUDriver, name)
		}
		if !gpuDriverOptionValue.MatchString(value) {
			return GPUDriverSettings{}, fmt.Errorf("%w: moduleOptions %s value %q must be letters, digits, or _.,:-", ErrInvalidGPUDriver, name, value)
		}
		if _, duplicate := options[name]; duplicate {
			return GPUDriverSettings{}, fmt.Errorf("%w: moduleOptions names %s twice", ErrInvalidGPUDriver, name)
		}
		options[name] = value
	}
	return GPUDriverSettings{ModuleOptions: options}, nil
}

// ErrInvalidGPUDriver means a GPU driver request cannot apply.
var ErrInvalidGPUDriver = errors.New("invalid GPU driver request")
