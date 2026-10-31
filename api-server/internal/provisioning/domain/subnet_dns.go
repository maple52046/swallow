package domain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// This file holds the Subnet DNS model (glossary Subnet DNS, decision 058): the DNS servers a
// provisioner subnet hands to machines deployed on it with an automatic or static address. It is
// pure model code; adapters under internal/adapters/osprovisioning translate it to their provider's subnet setting.

// MaxSubnetDNSServers bounds one subnet's Subnet DNS. A resolver configuration rarely uses more
// than three; eight leaves room without letting a request carry an unbounded list into the
// provider.
const MaxSubnetDNSServers = 8

// ErrInvalidSubnetDNS marks a Subnet DNS swallow refuses before calling the provisioner: an entry
// that is empty, not an IP address, or scoped to a zone; a repeated address; or too many entries.
// Errors wrapping it carry the operator-facing reason.
var ErrInvalidSubnetDNS = errors.New("invalid subnet DNS servers")

// ErrSubnetNotFound means the provisioner has no subnet with the requested id. It is a domain miss
// on the provisioner's live catalog, not an infrastructure failure.
var ErrSubnetNotFound = errors.New("subnet not found")

// NormalizeSubnetDNSServers validates an operator's Subnet DNS and returns it in canonical form:
// each entry trimmed and rewritten as netip prints it, in the operator's order. A nil or empty
// input is valid and means "clear the setting"; the result is then nil. A refusal wraps
// ErrInvalidSubnetDNS with the reason.
//
// A zone ("fe80::1%eth0") is refused because it names an interface of the host that typed it,
// which means nothing to the machines the provisioner deploys. Duplicates are compared after
// canonicalization, so "10.0.0.1" and " 10.0.0.1" are the same server.
func NormalizeSubnetDNSServers(raw []string) ([]string, error) {
	if len(raw) > MaxSubnetDNSServers {
		return nil, fmt.Errorf("%w: at most %d DNS servers are allowed", ErrInvalidSubnetDNS, MaxSubnetDNSServers)
	}
	var servers []string
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: a DNS server must not be empty", ErrInvalidSubnetDNS)
		}
		address, err := netip.ParseAddr(trimmed)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not an IP address", ErrInvalidSubnetDNS, trimmed)
		}
		if address.Zone() != "" {
			return nil, fmt.Errorf("%w: %q must not carry a zone", ErrInvalidSubnetDNS, trimmed)
		}
		canonical := address.String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, fmt.Errorf("%w: %s is listed more than once", ErrInvalidSubnetDNS, canonical)
		}
		seen[canonical] = struct{}{}
		servers = append(servers, canonical)
	}
	return servers, nil
}

// SubnetDNSWriter writes a Subnet DNS through to the provisioner. An adapter that sets
// ProviderCapabilities.SubnetDNS must implement it, and its NetworkConfigurationProvider must
// report each subnet's DNSServers.
//
// servers has already been normalized; nil clears the setting. Implementations replace the whole
// list and change no other subnet setting, so a following ListNetworkSubnets reports servers. An
// unknown subnetID is ErrSubnetNotFound, a refusal by the provider is a *ProviderError{Kind:
// ProviderErrorRejected} carrying its explanation, a credential that may not change subnets is
// ProviderErrorAuth, and transport failures are ProviderErrorUnavailable. A write only affects
// deployments the provider starts afterwards.
type SubnetDNSWriter interface {
	SetSubnetDNSServers(ctx context.Context, subnetID string, servers []string) error
}

// SubnetAttachmentReader tells which of a provisioner's subnets are attached — real networks — by
// the provisioner's own markers: a provisioner controller interface is on it, the provisioner
// serves its DHCP, a machine's boot interface is on it, or a machine NIC with hardware identity
// (or a bond, VLAN, or bridge built on one) is on it. Every other subnet is one the provisioner
// recorded from host-internal interfaces — container, Kubernetes, or VM bridges an enrolled host
// reported — or one nothing is on any more; deployments never use those, so consumers may list
// them apart. An adapter that sets ProviderCapabilities.SubnetDNS must implement it.
//
// The result is keyed by provider subnet id and holds only attached subnets. It is a live read
// over the provider's subnets, controllers, and machines, so callers read it once per request;
// transport failures are the usual *ProviderError kinds.
type SubnetAttachmentReader interface {
	AttachedSubnetIDs(ctx context.Context) (map[string]bool, error)
}
