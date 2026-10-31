package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// A Hypervisor (decision 055) is a role of a Server, not a record of its own: a present, deployed
// Server whose system libvirt daemon swallow reaches over SSH with the installation's Deployment
// Key, to read its virtual machines and to give them libvirt Boot Media. Switching a virtual
// machine's power stays with the provisioner's `virsh` driver.

// HypervisorLogin is how swallow logs in to one Hypervisor for one call.
type HypervisorLogin struct {
	ServerID string
	Name     string
	Target   HostLoginTarget
	// Account is the login account, which must be allowed to use the system libvirt daemon.
	Account string
}

// HypervisorOf checks that server can act as a Hypervisor and returns its login. account names
// the login account; empty uses the Server's effective Server Default User. Errors: a
// *HypervisorError wrapping ErrHypervisorNotDeployed or ErrInvalidDefaultUser.
func HypervisorOf(server *Server, account string) (HypervisorLogin, error) {
	name := server.DisplayName()
	address := server.PrimaryAddress()
	if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" || address == "" {
		return HypervisorLogin{}, &HypervisorError{Err: ErrHypervisorNotDeployed, Hypervisor: name}
	}
	account = strings.TrimSpace(account)
	if account == "" {
		account, _ = server.EffectiveDefaultUser()
	}
	if !ValidDefaultUser(account) {
		return HypervisorLogin{}, &HypervisorError{Err: ErrInvalidDefaultUser, Hypervisor: name, Account: account}
	}
	return HypervisorLogin{
		ServerID: server.ID, Name: name, Account: account,
		Target: HostLoginTarget{SiteID: server.Source.SiteID, Address: address, Name: name},
	}, nil
}

// HostMatches reports whether host (an address, hostname, or FQDN, as a libvirt connection URI
// names it) is this Server.
func (s *Server) HostMatches(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	for _, address := range s.Observed.Addresses {
		if strings.EqualFold(address, host) {
			return true
		}
	}
	return strings.EqualFold(s.Observed.Hostname, host) || strings.EqualFold(strings.TrimSuffix(s.Observed.FQDN, "."), host)
}

// VirtualMachine is one libvirt domain on a Hypervisor as swallow reads it.
type VirtualMachine struct {
	Name string
	UUID string
	// State is libvirt's own state words ("running", "shut off", "paused", …).
	State string
	// Architecture is libvirt's guest architecture, for example "x86_64".
	Architecture string
	MACAddresses []string
	// CDROM reports whether the domain's persistent definition has a CD-ROM device.
	CDROM bool
}

// ShutOff reports whether the domain is not running, so its definition may change and the
// provisioner may start it.
func (v VirtualMachine) ShutOff() bool {
	return v.State == "shut off"
}

// maxDomainNameLength bounds a requested domain name; libvirt allows longer, but no operator types
// one.
const maxDomainNameLength = 128

// ValidDomainName reports whether name may be requested as a libvirt domain name: non-empty, no
// surrounding whitespace, no '/' and no control characters, at most 128 characters.
func ValidDomainName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || len([]rune(name)) > maxDomainNameLength || strings.Contains(name, "/") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// BootMediaVolumeName is the storage volume a Boot ISO is uploaded to on a Hypervisor.
func BootMediaVolumeName(isoID string) string {
	return "swallow-ipxe-" + isoID + ".iso"
}

// BootMediaPool is the storage pool Boot ISO volumes go to. A Hypervisor without it gets it as a
// directory pool, as virt-install does.
const BootMediaPool = "default"

// LibvirtHost is the port for what swallow does with a Hypervisor's libvirt (decision 055): read
// domains, stop one the operator allowed to stop, authorize the provisioner's SSH key, and give a
// domain libvirt Boot Media.
//
// Implementations log in with the Deployment Key (see HostAccess for the trust model), run
// `virsh -c qemu:///system`, honour ctx, and never return a secret. Errors are, possibly wrapped:
// ErrDeploymentKeyMissing, ErrHostUnreachable, ErrDeploymentKeyRejected, ErrLibvirtUnavailable
// (virsh is missing or the account may not use libvirt), ErrDomainNotFound, and ErrLibvirtRefused
// (libvirt refused a change; the error carries its explanation).
type LibvirtHost interface {
	ListDomains(ctx context.Context, hypervisor HypervisorLogin) ([]VirtualMachine, error)
	Domain(ctx context.Context, hypervisor HypervisorLogin, name string) (*VirtualMachine, error)
	// DestroyDomain hard-stops a running domain; a domain already shut off is satisfied.
	DestroyDomain(ctx context.Context, hypervisor HypervisorLogin, name string) error
	// AuthorizePublicKey ensures publicKey is in the login account's authorized_keys.
	AuthorizePublicKey(ctx context.Context, hypervisor HypervisorLogin, publicKey string) error
	// ReadBootMedia reports whether the domain's CD-ROM holds the Boot ISO's volume and boots
	// first. MediaImage is the CD-ROM's source path.
	ReadBootMedia(ctx context.Context, hypervisor HypervisorLogin, domain, isoID string) (BootMediaState, error)
	// ApplyBootMedia uploads iso as its volume in BootMediaPool unless the same size is already
	// there, puts the volume on the domain's CD-ROM (adding one when the domain has none), and
	// makes it boot first in the persistent definition. It is idempotent and returns the state read
	// back.
	ApplyBootMedia(ctx context.Context, hypervisor HypervisorLogin, domain string, iso BootISOFile) (BootMediaState, error)
	// ClearBootMedia ejects the domain's CD-ROM and removes its boot order, keeping the order of
	// the other boot devices.
	ClearBootMedia(ctx context.Context, hypervisor HypervisorLogin, domain string) error
}

var (
	// ErrHypervisorNotDeployed means the Server cannot act as a Hypervisor: it is absent, not
	// deployed, or has no address.
	ErrHypervisorNotDeployed = errors.New("the hypervisor server is not deployed")
	// ErrLibvirtUnavailable means virsh is missing on the Hypervisor or the account may not use the
	// system libvirt daemon.
	ErrLibvirtUnavailable = errors.New("libvirt is unavailable to the account")
	// ErrDomainNotFound means the Hypervisor has no domain with the name.
	ErrDomainNotFound = errors.New("no such libvirt domain")
	// ErrLibvirtRefused means libvirt refused a change.
	ErrLibvirtRefused = errors.New("libvirt refused the change")
)

// HypervisorError explains a failed Hypervisor action in operator terms — which Hypervisor, which
// account, which domain, and what libvirt said — while Unwrap keeps the sentinel for status
// mapping.
type HypervisorError struct {
	Err        error
	Hypervisor string
	Account    string
	Domain     string
	Detail     string
}

// Error is the operator-facing sentence returned as error.message.
func (e *HypervisorError) Error() string {
	detail := ""
	if e.Detail != "" {
		detail = " " + e.Detail
	}
	switch {
	case errors.Is(e.Err, ErrHypervisorNotDeployed):
		return fmt.Sprintf("Server %q cannot act as a hypervisor: it must be present and deployed, with an address.", e.Hypervisor)
	case errors.Is(e.Err, ErrInvalidDefaultUser):
		return fmt.Sprintf("Account %q is not a login name swallow can use on hypervisor %q; give an account or set the Server Default User.", e.Account, e.Hypervisor)
	case errors.Is(e.Err, ErrDeploymentKeyMissing):
		return "The installation has no Deployment Key to log in to the hypervisor with; create one in Settings."
	case errors.Is(e.Err, ErrHostUnreachable):
		return fmt.Sprintf("Hypervisor %q could not be reached over SSH.%s", e.Hypervisor, detail)
	case errors.Is(e.Err, ErrDeploymentKeyRejected):
		return fmt.Sprintf("Hypervisor %q rejected the Deployment Key for account %q; set the account as the Server Default User with its password once, so swallow installs the key.", e.Hypervisor, e.Account)
	case errors.Is(e.Err, ErrLibvirtUnavailable):
		return fmt.Sprintf("Account %q on hypervisor %q cannot use libvirt (it must be able to run virsh against qemu:///system, for example as a member of the libvirt group).%s", e.Account, e.Hypervisor, detail)
	case errors.Is(e.Err, ErrDomainNotFound):
		return fmt.Sprintf("Hypervisor %q has no virtual machine named %q.", e.Hypervisor, e.Domain)
	case errors.Is(e.Err, ErrLibvirtRefused):
		return fmt.Sprintf("Libvirt on hypervisor %q refused the change to %q.%s", e.Hypervisor, e.Domain, detail)
	default:
		return fmt.Sprintf("Hypervisor %q failed: %v", e.Hypervisor, e.Err)
	}
}

// Unwrap exposes the sentinel so delivery maps the status with errors.Is.
func (e *HypervisorError) Unwrap() error { return e.Err }
