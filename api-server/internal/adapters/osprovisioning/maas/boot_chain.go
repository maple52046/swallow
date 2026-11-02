package maas

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// DefaultRackPort is the MAAS rack controller's HTTP boot port that serves ipxe.cfg and the GRUB
// configurations (grub/grub.cfg-<mac>).
const DefaultRackPort = 5248

// BootChainRenderer renders a Boot ISO that chains to a MAAS rack (decision 047): the user
// gives the rack as an IPv4 address or hostname with an optional ":port".
type BootChainRenderer struct{}

var _ provisioningdomain.BootChainRenderer = BootChainRenderer{}

// RenderBootChain validates the rack address and renders both boot paths that chain to it.
func (BootChainRenderer) RenderBootChain(address string) (provisioningdomain.BootChain, error) {
	host, port, err := parseRackAddress(address)
	if err != nil {
		return provisioningdomain.BootChain{}, err
	}
	return provisioningdomain.BootChain{
		ChainURL: chainURL(host, port), UEFIChainURL: uefiChainURL(host, port),
		Content: provisioningdomain.BootISOContent{
			IPXEScript: renderBootScript(host, port), GRUBConfig: renderGrubConfig(host, port),
		},
	}, nil
}

// parseRackAddress validates a rack address as a user types it — an IPv4 address or a
// hostname, optionally followed by ":port" — and returns the host and port (DefaultRackPort
// when none is given). The result is safe to place into an iPXE script and a URL: only
// letters, digits, '-', '.', and a numeric port pass. It neither resolves nor contacts the host.
func parseRackAddress(raw string) (string, int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", 0, fmt.Errorf("%w: rackAddress is required", provisioningdomain.ErrInvalidBootISO)
	}
	host, port := value, DefaultRackPort
	if i := strings.LastIndex(value, ":"); i >= 0 {
		host = value[:i]
		parsed, err := strconv.Atoi(value[i+1:])
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", 0, fmt.Errorf("%w: rackAddress port must be a number from 1 to 65535", provisioningdomain.ErrInvalidBootISO)
		}
		port = parsed
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return "", 0, fmt.Errorf("%w: rackAddress must be an IPv4 address or a hostname", provisioningdomain.ErrInvalidBootISO)
		}
		return ip.To4().String(), port, nil
	}
	if !validHostname(host) {
		return "", 0, fmt.Errorf("%w: rackAddress must be an IPv4 address or a hostname", provisioningdomain.ErrInvalidBootISO)
	}
	return strings.ToLower(host), port, nil
}

// validHostname accepts RFC 1123 hostnames: dot-separated labels of letters, digits, and '-',
// each 1 to 63 characters, not starting or ending with '-', 253 characters in total.
func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// chainURL is the URL a Boot ISO chains to after DHCP on BIOS firmware: the rack's iPXE
// pre-loader.
func chainURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/ipxe.cfg", host, port)
}

// uefiChainURL is where a Boot ISO's UEFI path loads its MAAS configuration from: the rack's
// per-MAC GRUB configuration, with "<mac>" standing for the MAC of the interface that took the
// DHCP lease (decision 047).
func uefiChainURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/grub/grub.cfg-<mac>", host, port)
}

// renderBootScript renders the Boot ISO's iPXE script for a MAAS rack (decision 047); only
// BIOS firmware runs it, because UEFI firmware boots the ISO's GRUB instead (decision 047,
// renderGrubConfig). It is the template verified on networks whose DHCP is not MAAS's
// (MAAS knowledge base: iPXE 2.0.0, MAAS 3.7): take a DHCP lease, set next-server to the rack
// because MAAS's follow-up scripts build their URLs from it, and chain the rack's ipxe.cfg without
// using the DHCP boot filename, so a site PXE server never takes over. A failed DHCP retries
// (Ctrl-B opens a shell); a chain that returns retries too. host and port must come from
// parseRackAddress.
func renderBootScript(host string, port int) string {
	return fmt.Sprintf(`#!ipxe

set maas_rack %s

:start
dhcp || goto retry
set next-server ${maas_rack}
chain http://${next-server}:%d/ipxe.cfg || goto returned

:retry
prompt --key 0x02 --timeout 10000 Press Ctrl-B for shell, or wait to retry... && shell ||
goto start

:returned
iseq ${platform} efi && exit 1 || goto retry
`, host, port)
}

// bootISOGrubCards bounds how many network cards the UEFI path looks at for the interface that took
// the DHCP lease; GRUB names them efinet0, efinet1, and so on.
const bootISOGrubCards = 16

// renderGrubConfig renders the GRUB configuration a Boot ISO boots on UEFI firmware
// (decision 047). It is embedded in the ISO's own GRUB, which replaces iPXE on UEFI: a GRUB that
// iPXE chains receives none of iPXE's network state and stops at its prompt.
//
// GRUB takes a DHCP lease from the site network itself (net_bootp, retried every ten seconds until
// a card has one), picks the first card with a lease, and loads the rack's per-MAC configuration
// over HTTP — MAAS's own UEFI boot path, so MAAS records a UEFI boot, identifies the Machine by
// that MAC (or enlists an unknown one), and gives it the EFI system partition a UEFI deployment
// needs.
//
// MAAS's configuration passes the kernel BOOTIF=01-${net_default_mac}. GRUB derives that variable
// from net_<net_default_interface>_mac, while the variables net_bootp creates name the
// "<card>:dhcp" interface with "_" for ":", so net_default_interface is set to "<card>_dhcp";
// both it and net_default_server are exported into MAAS's configuration. MAAS's local-boot
// configuration finds the installed bootloader by searching every disk's EFI system partition;
// GRUB autoloads filesystem modules but never partition maps, so part_gpt and part_msdos are
// loaded up front or no partition is visible to that search. If MAAS returns without booting, GRUB
// exits to the firmware's next boot device. Secure Boot must be off: the GRUB is built by swallow
// and unsigned. host and port must come from parseRackAddress.
func renderGrubConfig(host string, port int) string {
	cards := make([]string, bootISOGrubCards)
	for i := range cards {
		cards[i] = fmt.Sprintf("efinet%d", i)
	}
	return fmt.Sprintf(`set timeout=0
insmod efinet
insmod http
insmod eval
insmod part_gpt
insmod part_msdos
set maas_rack=%s
set maas_port=%d
echo "swallow Boot ISO: taking a DHCP lease, then booting MAAS rack ${maas_rack}:${maas_port}"

set mac=""
set iface=""
while [ -z "$mac" ]; do
  net_bootp
  for card in %s; do
    eval "set candidate=\$net_${card}_dhcp_mac"
    if [ -z "$mac" -a -n "$candidate" ]; then
      set mac="$candidate"
      set iface="${card}_dhcp"
    fi
  done
  if [ -z "$mac" ]; then
    echo "No DHCP lease yet; retrying in 10 seconds."
    sleep 10
  fi
done

set net_default_interface="$iface"
set net_default_server="$maas_rack"
export net_default_interface
export net_default_server
set root=(http,${maas_rack}:${maas_port})
echo "Loading MAAS configuration for $mac"
configfile /grub/grub.cfg-$mac

echo "MAAS did not boot this machine; returning to the firmware."
sleep 10
exit 1
`, host, port, strings.Join(cards, " "))
}
