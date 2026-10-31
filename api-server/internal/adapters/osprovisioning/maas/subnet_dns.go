package maas

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// This file implements the Subnet DNS capability against MAAS (decision 058). MAAS holds a
// subnet's DNS servers as its dns_servers attribute, listed by GET /subnets/ (see
// ListNetworkSubnets) and replaced by the subnet's update, PUT /subnets/{id}/.

// SetSubnetDNSServers implements provisioningdomain.SubnetDNSWriter with one subnet update that
// carries only dns_servers. MAAS's API forms fill every omitted field from the stored subnet, so
// the update leaves its name, VLAN, managed flag, and MAAS DNS and proxy permissions as they were.
// The servers are sent as one space-separated value, which MAAS splits; an explicit empty value
// clears the list, so the exact multipart encoding is used. Updating a subnet is admin-only.
func (p *Provider) SetSubnetDNSServers(ctx context.Context, subnetID string, servers []string) error {
	path := "/subnets/" + url.PathEscape(strings.TrimSpace(subnetID)) + "/"
	fields := map[string]string{"dns_servers": strings.Join(servers, " ")}
	if err := p.client.putMultipartExact(ctx, path, fields, nil); err != nil {
		return translateSubnetWriteError(err)
	}
	return nil
}

// nodeInterfacesJSON is the part of a MAAS machine or controller AttachedSubnetIDs reads.
// Controllers have no boot interface.
type nodeInterfacesJSON struct {
	BootInterface *struct {
		ID int `json:"id"`
	} `json:"boot_interface"`
	InterfaceSet []interfaceJSON `json:"interface_set"`
}

// AttachedSubnetIDs implements provisioningdomain.SubnetAttachmentReader from MAAS's own network
// markers, read live in four calls:
//
//   - every subnet a rack or region controller interface links, which is how MAAS reaches or
//     serves a network, even over a virtual bridge such as a libvirt network;
//   - every subnet whose VLAN MAAS serves DHCP on, directly or through a relay;
//   - every subnet a machine's boot interface links: the NIC MAAS booted or enrolled the machine
//     through, so a hypervisor network counts once a managed VM boots from it, whatever its NIC;
//   - every subnet a machine interface with hardware identity links: one with a vendor, or a
//     bond, VLAN, or bridge with such an interface among its parents.
//
// Interface type is deliberately not a signal: MAAS records veth pairs as physical, so docker
// and Kubernetes bridges would pass. Names are not either; they differ per runtime and can be
// changed. MAAS reports machine interfaces as recorded by the last commissioning or enrollment.
func (p *Provider) AttachedSubnetIDs(ctx context.Context) (map[string]bool, error) {
	attached := make(map[string]bool)
	subnets, err := p.listNetworkSubnets(ctx)
	if err != nil {
		return nil, err
	}
	for _, subnet := range subnets {
		if subnet.VLAN.providerDHCP() {
			attached[strconv.Itoa(subnet.ID)] = true
		}
	}
	for _, path := range []string{"/rackcontrollers/", "/regioncontrollers/"} {
		var controllers []nodeInterfacesJSON
		if err := p.client.get(ctx, path, nil, &controllers); err != nil {
			return nil, translateError(err, "")
		}
		for _, controller := range controllers {
			for _, iface := range controller.InterfaceSet {
				addLinkedSubnets(attached, iface)
			}
		}
	}
	var machines []nodeInterfacesJSON
	if err := p.client.get(ctx, "/machines/", nil, &machines); err != nil {
		return nil, translateError(err, "")
	}
	for _, machine := range machines {
		byName := make(map[string]interfaceJSON, len(machine.InterfaceSet))
		for _, iface := range machine.InterfaceSet {
			byName[iface.Name] = iface
		}
		for _, iface := range machine.InterfaceSet {
			boot := machine.BootInterface != nil && machine.BootInterface.ID == iface.ID
			if boot || hasHardwareIdentity(iface, byName, map[string]bool{}) {
				addLinkedSubnets(attached, iface)
			}
		}
	}
	return attached, nil
}

// addLinkedSubnets marks every subnet one interface has a link on.
func addLinkedSubnets(attached map[string]bool, iface interfaceJSON) {
	for _, link := range iface.Links {
		if link.Subnet != nil {
			attached[strconv.Itoa(link.Subnet.ID)] = true
		}
	}
}

// hasHardwareIdentity reports whether iface is a NIC MAAS knows the vendor of, or is built on one
// through its parents. seen guards against a parent cycle in malformed data; a parent MAAS did not
// list does not count.
func hasHardwareIdentity(iface interfaceJSON, byName map[string]interfaceJSON, seen map[string]bool) bool {
	if strings.TrimSpace(iface.Vendor) != "" {
		return true
	}
	if seen[iface.Name] {
		return false
	}
	seen[iface.Name] = true
	for _, name := range iface.Parents {
		if parent, ok := byName[name]; ok && hasHardwareIdentity(parent, byName, seen) {
			return true
		}
	}
	return false
}

// translateSubnetWriteError keeps two refusals of a subnet update explicit: 404 is a subnet MAAS
// no longer has, and 401/403 is an Integration account that is not a MAAS administrator, which the
// operator must fix in the Integration rather than in the value.
func translateSubnetWriteError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusNotFound:
			return provisioningdomain.ErrSubnetNotFound
		case http.StatusUnauthorized, http.StatusForbidden:
			return &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorAuth,
				Detail: "The provisioner credential may not change MAAS subnets; its MAAS account must be an administrator.",
				Err:    err,
			}
		}
	}
	return translateError(err, "")
}
