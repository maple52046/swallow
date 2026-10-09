package domain

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// maxPowerParameterLength bounds an operator-supplied power parameter. Real BMC addresses, libvirt
// URIs, and domain names are far shorter; the bound only keeps a malformed request from carrying an
// unbounded string to the provisioner.
const maxPowerParameterLength = 255

// bmcPowerAdapter serves the bmc family: a physical machine's BMC, switched by the provisioner over
// IPMI or Redfish. It implements BMCPowerAdapter because a BMC can also be reached out of band, for
// Redfish Boot Media, whichever of the two drivers the provisioner uses.
type bmcPowerAdapter struct{}

// Family implements PowerAdapter.
func (bmcPowerAdapter) Family() PowerFamily { return PowerFamilyBMC }

// Drivers implements PowerAdapter.
func (bmcPowerAdapter) Drivers() []PowerDriver {
	return []PowerDriver{PowerDriverIPMI, PowerDriverRedfish}
}

// Normalize implements PowerAdapter. A BMC needs an address; the account is optional because some
// provisioners default it, and a power ID belongs to virtual machines only.
func (bmcPowerAdapter) Normalize(change PowerConfigurationChange) (PowerConfigurationChange, error) {
	change.Address = strings.TrimSpace(change.Address)
	change.Username = strings.TrimSpace(change.Username)
	change.PowerID = strings.TrimSpace(change.PowerID)
	if err := requireToken("address", change.Address); err != nil {
		return change, err
	}
	if change.PowerID != "" {
		return change, fmt.Errorf("%w: powerId does not apply to driver %q", ErrInvalidPowerConfiguration, change.Driver)
	}
	if err := optionalToken("username", change.Username); err != nil {
		return change, err
	}
	return change, nil
}

// BMCAccess implements BMCPowerAdapter.
func (bmcPowerAdapter) BMCAccess(config PowerConfiguration) (BMCAccess, bool) {
	address := strings.TrimSpace(config.Address)
	if address == "" {
		return BMCAccess{}, false
	}
	return BMCAccess{
		Driver: config.Driver, Address: address, Username: config.Username,
		Password: config.Password, SystemHint: config.NodeID,
	}, true
}

// virshPowerAdapter serves the virsh family: a libvirt virtual machine that the provisioner powers
// by connecting to its hypervisor. It has no BMC, so it implements no extension.
type virshPowerAdapter struct{}

// Family implements PowerAdapter.
func (virshPowerAdapter) Family() PowerFamily { return PowerFamilyVirsh }

// Drivers implements PowerAdapter.
func (virshPowerAdapter) Drivers() []PowerDriver { return []PowerDriver{PowerDriverVirsh} }

// Normalize implements PowerAdapter. The address must be a clean `qemu+ssh://[user@]host[:port]/system`
// URI: MAAS refuses extra URI parameters ("Supplying extra parameters to the Virsh address is not
// supported") and a session URI or local socket would be resolved on whichever rack controller runs
// the action, so SSH options belong in the provisioner's own SSH configuration, not in the URI. A
// password in the URI is refused so it can never be read back through the address.
func (virshPowerAdapter) Normalize(change PowerConfigurationChange) (PowerConfigurationChange, error) {
	change.Address = strings.TrimSpace(change.Address)
	change.PowerID = strings.TrimSpace(change.PowerID)
	change.Username = strings.TrimSpace(change.Username)
	if err := requireToken("address", change.Address); err != nil {
		return change, err
	}
	if err := validateVirshAddress(change.Address); err != nil {
		return change, err
	}
	if err := requireToken("powerId", change.PowerID); err != nil {
		return change, err
	}
	if change.Username != "" {
		return change, fmt.Errorf("%w: username does not apply to driver %q; put the SSH user in the address", ErrInvalidPowerConfiguration, change.Driver)
	}
	return change, nil
}

// validateVirshAddress enforces the libvirt URI shape the provisioner can use.
func validateVirshAddress(address string) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: address must be qemu+ssh://[user@]host[:port]/system; %s", ErrInvalidPowerConfiguration, reason)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return invalid("it is not a URI")
	}
	switch {
	case parsed.Scheme != "qemu+ssh":
		return invalid(fmt.Sprintf("scheme %q is not qemu+ssh", parsed.Scheme))
	case parsed.Opaque != "" || parsed.Hostname() == "":
		return invalid("the host is missing")
	case parsed.Path != "/system":
		return invalid(fmt.Sprintf("path %q is not /system", parsed.Path))
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return invalid("query parameters are not supported; configure SSH options in the provisioner")
	case parsed.Fragment != "":
		return invalid("a fragment is not supported")
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return invalid("a password in the URI is not allowed; use the password field or an SSH key")
	}
	return nil
}

// requireToken refuses an empty parameter and one that optionalToken refuses.
func requireToken(name, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidPowerConfiguration, name)
	}
	return optionalToken(name, value)
}

// optionalToken refuses whitespace, control characters, and overlong values in a single-token
// parameter. None of the provisioner's power parameters may contain them, and refusing them here
// gives the operator a clear reason instead of a provisioner validation dump.
func optionalToken(name, value string) error {
	if len(value) > maxPowerParameterLength {
		return fmt.Errorf("%w: %s is longer than %d characters", ErrInvalidPowerConfiguration, name, maxPowerParameterLength)
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: %s must not contain spaces or control characters", ErrInvalidPowerConfiguration, name)
	}
	return nil
}
