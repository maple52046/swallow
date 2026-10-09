package domain

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrInvalidVirtualMachineEnrollment marks a virtual-machine enrollment request that is
	// malformed or names a provisioner that cannot register machines.
	ErrInvalidVirtualMachineEnrollment = errors.New("invalid virtual machine enrollment")
	// ErrVirtualMachineEnrollmentConflict marks a virtual-machine enrollment the current state
	// refuses, such as a hypervisor outside the provisioner's Site.
	ErrVirtualMachineEnrollmentConflict = errors.New("virtual machine enrollment is not possible now")
)

// MachineRegistration is a machine swallow registers with the provisioner itself, without
// commissioning it (decision 055): a libvirt virtual machine whose MAC addresses and architecture
// swallow read from its domain. The provisioner's hardware inspection is left to swallow's own
// inspect-hardware Workflow.
type MachineRegistration struct {
	Hostname string
	// Architecture is provisioner-neutral (MachineArchitectureOf); adapters map it to their own
	// vocabulary.
	Architecture string
	MACAddresses []string
	Power        PowerConfigurationChange
}

// MachineRegistrar registers machines with the provisioner. An adapter that implements it sets
// ProviderCapabilities.MachineRegistration.
type MachineRegistrar interface {
	// FindMachineByMAC returns the id of a machine that has any of macs, "" when none has.
	FindMachineByMAC(ctx context.Context, macs []string) (string, error)
	// RegisterMachine creates the machine without commissioning it and returns its id. A refusal
	// (for example a hostname in use) is a ProviderErrorRejected carrying the provisioner's reason.
	RegisterMachine(ctx context.Context, registration MachineRegistration) (string, error)
}

// Machine architectures swallow registers.
const (
	MachineArchitectureAMD64 = "amd64"
	MachineArchitectureARM64 = "arm64"
)

// MachineArchitectureOf maps a libvirt guest architecture onto the architecture a machine is
// registered with; ok is false for one swallow does not register.
func MachineArchitectureOf(libvirtArch string) (string, bool) {
	switch strings.TrimSpace(libvirtArch) {
	case "x86_64":
		return MachineArchitectureAMD64, true
	case "aarch64":
		return MachineArchitectureARM64, true
	default:
		return "", false
	}
}

// maxHostnameLength is the DNS label limit a provisioner enforces on a machine hostname.
const maxHostnameLength = 63

// MachineHostnameOf derives a machine hostname from a libvirt domain name: lower case, every
// character other than a–z, 0–9, and '-' replaced by '-', at most 63 characters, without leading
// or trailing '-'. It is empty when nothing usable remains.
func MachineHostnameOf(domainName string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(domainName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	hostname := out.String()
	if len(hostname) > maxHostnameLength {
		hostname = hostname[:maxHostnameLength]
	}
	return strings.Trim(hostname, "-")
}
