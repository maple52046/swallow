package application

import (
	"context"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// SettingVirshSSHPublicKey is the provisioner Integration setting holding the public key of the
// SSH identity the provisioner's virsh driver connects to a Hypervisor with (contract
// sites-integrations.md, decision 055). The installation sets it; virtual-machine enrollment
// authorizes it on the Hypervisor.
const SettingVirshSSHPublicKey = "virshSshPublicKey"

// VirshSSHPublicKey is integration's virsh SSH public key, "" when the installation set none.
func VirshSSHPublicKey(integration *sitedomain.Integration) string {
	return strings.TrimSpace(integration.Settings[SettingVirshSSHPublicKey])
}

// VirtualMachineEnrollmentRequest is the intent to enroll libvirt domains of a Hypervisor into a
// provisioner (contract server-enrollment.md).
type VirtualMachineEnrollmentRequest struct {
	IntegrationID      string
	HypervisorServerID string
	Domains            []string
	BootISOID          string
	Account            string
	PowerOffRunning    bool
	RequestedBy        string
	RequestID          string
}

// Normalize trims the identifiers and checks the shape: an Integration, a Hypervisor, at least one
// domain without duplicates, each a valid domain name, and an account that is a login name when
// given. Errors wrap ErrInvalidVirtualMachineEnrollment.
func (r VirtualMachineEnrollmentRequest) Normalize() (VirtualMachineEnrollmentRequest, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{provisioningdomain.ErrInvalidVirtualMachineEnrollment}, args...)...)
	}
	r.IntegrationID = strings.TrimSpace(r.IntegrationID)
	r.HypervisorServerID = strings.TrimSpace(r.HypervisorServerID)
	r.BootISOID = strings.TrimSpace(r.BootISOID)
	r.Account = strings.TrimSpace(r.Account)
	switch {
	case r.IntegrationID == "":
		return r, invalid("integrationId is required")
	case r.HypervisorServerID == "":
		return r, invalid("hypervisorServerId is required")
	case len(r.Domains) == 0:
		return r, invalid("domains must name at least one virtual machine")
	case r.Account != "" && !serverdomain.ValidDefaultUser(r.Account):
		return r, invalid("account %q is not a login name", r.Account)
	}
	seen := make(map[string]bool, len(r.Domains))
	for _, domain := range r.Domains {
		if !serverdomain.ValidDomainName(domain) {
			return r, invalid("domain %q must be non-empty, without surrounding spaces, '/', or control characters, and at most 128 characters", domain)
		}
		if seen[domain] {
			return r, invalid("domain %q is named twice", domain)
		}
		seen[domain] = true
	}
	return r, nil
}

// VirtualMachineEnrollmentAccepted is the Workflow an enrollment request started.
type VirtualMachineEnrollmentAccepted struct {
	WorkflowID string `json:"workflowId"`
}

// VirtualMachineEnrollmentLauncher starts virtual-machine enrollment as a durable
// enroll-virtual-machines Workflow (decision 055) without making the provisioning context depend on
// Workflow persistence. Implementations normalize the request, check the provisioner, Hypervisor,
// and Boot ISO, and refuse with the errors RespondError maps.
type VirtualMachineEnrollmentLauncher interface {
	LaunchVirtualMachineEnrollment(ctx context.Context, request VirtualMachineEnrollmentRequest) (*VirtualMachineEnrollmentAccepted, error)
}
