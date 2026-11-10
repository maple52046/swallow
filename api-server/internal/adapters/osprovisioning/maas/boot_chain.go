package maas

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"text/template"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// DefaultRackPort is the MAAS rack controller's HTTP boot port that serves ipxe.cfg and the GRUB
// configurations (grub/grub.cfg-<mac>).
const DefaultRackPort = 5248

// BootChainRenderer renders a Boot ISO that chains to a MAAS rack (decision 047): the user
// gives the rack as an IPv4 address or hostname with an optional ":port". Both boot paths open
// with the same boot mode menu — a DHCP lease by default, or a static IPv4 address typed at the
// console — and the static values never leave the machine's console.
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

// bootModeMenuSeconds is how long both boot paths show the boot mode menu before they take a
// DHCP lease on their own, so an unattended boot (inspection, deployment) only waits this long.
const bootModeMenuSeconds = 10

// bootChainTemplateData is what the boot path templates are filled with. Host and Port must come
// from parseRackAddress. RackIsHostname adds the DNS server to the UEFI path's static settings,
// because a static address has no DHCP to supply one and the rack cannot be resolved without it.
type bootChainTemplateData struct {
	Host           string
	Port           int
	RackIsHostname bool
	// MenuSeconds and MenuMilliseconds are bootModeMenuSeconds in the units GRUB's timeout and
	// iPXE's choose take.
	MenuSeconds      int
	MenuMilliseconds int
	// Cards are the GRUB network card names the UEFI path probes, efinet0 first; OverflowCard is
	// the first name past them, probed only to report that cards were ignored.
	Cards        []string
	OverflowCard string
	// Masks maps every subnet mask the UEFI path accepts to its prefix length.
	Masks []subnetMask
}

// subnetMask pairs a dotted subnet mask with its prefix length, for the GRUB route that needs the
// prefix while users type the mask.
type subnetMask struct {
	Mask   string
	Prefix int
}

// newBootChainTemplateData fills the template data for a rack. The masks run from /32 down to /1:
// a zero mask is not a subnet, and reaching other networks is what the gateway is for.
func newBootChainTemplateData(host string, port int) bootChainTemplateData {
	cards := make([]string, bootISOGrubCards)
	for i := range cards {
		cards[i] = fmt.Sprintf("efinet%d", i)
	}
	masks := make([]subnetMask, 0, 32)
	for prefix := 32; prefix >= 1; prefix-- {
		masks = append(masks, subnetMask{Mask: net.IP(net.CIDRMask(prefix, 32)).String(), Prefix: prefix})
	}
	return bootChainTemplateData{
		Host: host, Port: port, RackIsHostname: net.ParseIP(host) == nil,
		MenuSeconds: bootModeMenuSeconds, MenuMilliseconds: bootModeMenuSeconds * 1000,
		Cards: cards, OverflowCard: fmt.Sprintf("efinet%d", bootISOGrubCards), Masks: masks,
	}
}

// renderTemplate fills a boot path template. The templates are fixed and the data comes from
// newBootChainTemplateData, so an execution error is a programming error caught by the tests.
func renderTemplate(tmpl *template.Template, host string, port int) string {
	var out strings.Builder
	if err := tmpl.Execute(&out, newBootChainTemplateData(host, port)); err != nil {
		panic(fmt.Sprintf("render %s: %v", tmpl.Name(), err))
	}
	return out.String()
}

// renderBootScript renders the Boot ISO's iPXE script for a MAAS rack (decision 047); only
// BIOS firmware runs it, because UEFI firmware boots the ISO's GRUB instead (renderGrubConfig).
// host and port must come from parseRackAddress.
//
// It opens with iPXE's own boot mode menu, which picks DHCP after bootModeMenuSeconds. The DHCP
// branch is the template verified on networks whose DHCP is not MAAS's (MAAS knowledge base:
// iPXE 2.0.0, MAAS 3.7): take a DHCP lease, set next-server to the rack because MAAS's follow-up
// scripts build their URLs from it, and chain the rack's ipxe.cfg without using the DHCP boot
// filename, so a site PXE server never takes over. A failed DHCP, or a chain that returns, shows
// why for five seconds and goes back to the menu, whose countdown retries DHCP, so an unattended
// boot keeps retrying.
//
// The static branch lists the network interfaces with their MACs, then presents an iPXE form for
// the address, subnet mask, and gateway; iPXE rejects a value that is not an IPv4 address. It has
// no DNS server field, because a hostname rack cannot be stored in next-server, an IPv4 setting,
// so this path needs the rack as an IPv4 address in either branch. It closes every interface
// before opening the chosen one, because MAAS's ipxe.cfg and per-MAC configuration read the
// unqualified ${mac} for the URL and the kernel's BOOTIF, and iPXE resolves it through netX, the
// most recently opened interface. Choosing DHCP afterwards clears what the static branch set,
// since a value stored on an interface outranks the DHCP lease's. The static address only
// reaches the rack: MAAS's ephemeral environment still takes its own DHCP lease.
func renderBootScript(host string, port int) string {
	return renderTemplate(bootScriptTemplate, host, port)
}

var bootScriptTemplate = template.Must(template.New("iPXE boot script").Parse(`#!ipxe

set maas_rack {{.Host}}

:menu
menu swallow Boot ISO for MAAS rack ${maas_rack}:{{.Port}}
item --gap How should this machine reach the rack? DHCP starts in {{.MenuSeconds}} seconds.
item --gap
item dhcp DHCP (default)
item static Static IPv4 address
item shell iPXE shell
choose --default dhcp --timeout {{.MenuMilliseconds}} mode || goto dhcp
goto ${mode}

:dhcp
isset ${iface} || goto dhcp_start
clear ${iface}/ip
clear ${iface}/netmask
clear ${iface}/gateway
clear iface
:dhcp_start
ifclose
dhcp || goto dhcp_failed
goto chain

:dhcp_failed
prompt --timeout 5000 No DHCP lease; back to the menu in 5 seconds. ||
goto menu

:static
ifclose
set idx:int32 0
menu Network interface for the static IPv4 address
:static_nic
isset ${net${idx}/mac} || goto static_choose
item net${idx} net${idx}    ${net${idx}/mac}    ${net${idx}/chip}
inc idx
goto static_nic
:static_choose
choose iface || goto menu

:static_form
form Static IPv4 address for ${iface} (${${iface}/mac})
item ${iface}/ip IPv4 address
item ${iface}/netmask Subnet mask
item ${iface}/gateway Gateway (empty when the rack is on this subnet)
present || goto static
isset ${${iface}/ip} || goto static_incomplete
isset ${${iface}/netmask} || goto static_incomplete
ifopen ${iface} || goto static_failed
goto chain

:static_incomplete
prompt The IPv4 address and subnet mask are both required. Press any key to edit them.
goto static_form

:static_failed
prompt ${iface} could not be opened. Press any key to edit the settings.
goto static_form

:shell
shell
goto menu

:chain
set next-server ${maas_rack}
chain http://${next-server}:{{.Port}}/ipxe.cfg || goto returned

:returned
iseq ${platform} efi && exit 1 ||
prompt --timeout 5000 MAAS rack ${maas_rack}:{{.Port}} did not boot this machine; back to the menu in 5 seconds. ||
goto menu
`))

// bootISOGrubCards bounds how many network cards the UEFI path looks at; GRUB names them efinet0,
// efinet1, and so on, without gaps, in the firmware's handle order rather than PCI order. The
// bound must exceed every card a GPU server's firmware can hand GRUB: one with eight dual-port
// NICs, an onboard pair, and the BMC's USB NIC already has seventeen, and a card past the bound
// is never tried. A card at the bound is reported as ignored rather than dropped silently.
const bootISOGrubCards = 64

// renderGrubConfig renders the GRUB configuration a Boot ISO boots on UEFI firmware
// (decision 047). It is embedded in the ISO's own GRUB, which replaces iPXE on UEFI: a GRUB that
// iPXE chains receives none of iPXE's network state and stops at its prompt. host and port must
// come from parseRackAddress.
//
// It opens with a GRUB boot mode menu that picks DHCP after bootModeMenuSeconds. The DHCP branch
// first lists the cards GRUB can use: efinet drops, without a message, a card the firmware does
// not hand to boot loaders or whose SNP fails to start or initialize, so the list is the only
// sign of a missing card.
//
// Its first pass asks every card at once (net_bootp), because a GPU server has many uncabled
// ports and each costs a full DHCP timeout (about twelve seconds) when asked alone. The cards that
// answered are told apart by their net_<card>_dhcp_ip variables, which are trustworthy only then:
// GRUB 2.06 keeps a write hook on them after the interface is removed, so they hold the first
// lease's values for the rest of the context, and the exported swallow_dhcp_asked flag that marks
// the first pass lives in the same contexts as they do. net_bootp pushes one error per card that
// did not answer onto GRUB's error stack, which holds ten, so a server with many uncabled ports
// also prints "error stack overflow"; the branch says that this is harmless. The first pass's
// leases are removed and each card that answered is tried; later passes try every card.
//
// A card is tried by taking a lease on it alone (net_bootp <card>), in card order, so a card that
// fails — no DHCP answer, or a driver that cannot send — shows its own errors and the next card is
// still tried, only that card's routes decide where traffic to the rack goes, and the lease is
// judged by net_bootp's status rather than the sticky variables. A card with a lease must fetch
// the rack's static ipxe.cfg, which records nothing in MAAS (unlike grub.cfg-<mac>), before MAAS's
// configuration is loaded with that card's MAC — MAAS's own UEFI boot path, so MAAS records a
// UEFI boot, identifies the Machine by that MAC, and gives it the EFI system partition a UEFI
// deployment needs. A card that cannot reach the rack has its routes and interface removed
// (net_del_route of a route that does not exist, when DHCP gave no router, succeeds silently)
// and the next card is tried; when none can, every card is tried again after ten seconds, and Esc
// during that wait shows the menu again.
//
// The static branch is a set of GRUB menus: the cards with their MACs, then a settings screen
// whose rows show the address, subnet mask, gateway, and — when the rack is a hostname — DNS
// server; selecting a row reads its value at the console (GRUB's read has no line editing, so a
// typo is fixed by selecting the row again). Continue validates the values, adds them as the
// swallow_static interface with its local and default routes, and loads the same per-MAC
// configuration; a failure removes what it added and shows the settings screen with the reason.
// The static address only reaches the rack: MAAS's ephemeral environment still takes its own
// DHCP lease.
//
// Both branches load MAAS's per-MAC configuration and then, as MAAS's own grub/grub.cfg
// pre-loader does, grub.cfg-default-${grub_cpu}: the rack answers a MAC it does not know with
// 404, and only the default configuration enlists a new machine. configfile reports success even
// when the configuration cannot be loaded, so returning from both is the only sign that MAAS did
// not boot the machine: the DHCP branch then exits to the firmware as before, while the static
// branch, whose user is at the console, shows the settings again.
// net_add_addr also adds a classful "<name>:local" route (a /8 for 10.x) that would outrank the
// default route for a rack in another subnet of the same class, and net_del_addr leaves that
// route pointing at the freed interface, so both the static interface and the card probes
// delete it themselves.
//
// Every screen after the first is a submenu whose body defines the next menu. GRUB runs a
// submenu's body before showing its menu, so moving between screens never deepens script
// recursion, which GRUB caps at a few dozen levels; Esc returns to the previous screen with the
// values it had. Values cross screens only as exported variables, the only ones a submenu
// inherits. The cards are probed once at start, stopping at the first missing one, because a
// failed probe prints an error and an error printed in a submenu's body makes GRUB wait for a
// key.
//
// MAAS's configuration passes the kernel BOOTIF=01-${net_default_mac}. GRUB derives that variable
// from net_<net_default_interface>_mac, while the variables net_bootp creates name the
// "<card>:dhcp" interface with "_" for ":", so net_default_interface is set to "<card>_dhcp"
// (or swallow_static); both it and net_default_server are exported into MAAS's configuration.
// MAAS's local-boot configuration finds the installed bootloader by searching every disk's EFI
// system partition; GRUB autoloads filesystem modules but never partition maps, so part_gpt and
// part_msdos are loaded up front or no partition is visible to that search. If MAAS returns
// without booting a machine that took a DHCP lease, GRUB exits to the firmware's next boot
// device. Secure Boot must be off: the GRUB is built by swallow and unsigned.
func renderGrubConfig(host string, port int) string {
	return renderTemplate(grubConfigTemplate, host, port)
}

var grubConfigTemplate = template.Must(template.New("GRUB configuration").Parse(`insmod efinet
insmod http
insmod eval
insmod part_gpt
insmod part_msdos
insmod read
insmod regexp
insmod sleep
set maas_rack={{.Host}}
set maas_port={{.Port}}
export maas_rack
export maas_port

set swallow_probing=y
{{- range .Cards}}
if [ "$swallow_probing" = y ]; then
  if net_add_addr swallow_probe {{.}} 0.0.0.0; then
    set swallow_mac_{{.}}="$net_swallow_probe_mac"
    export swallow_mac_{{.}}
    net_del_route swallow_probe:local
    net_del_addr swallow_probe
  else
    set swallow_probing=n
  fi
fi
{{- end}}
set swallow_cards_ignored=n
if [ "$swallow_probing" = y ]; then
  if net_add_addr swallow_probe {{.OverflowCard}} 0.0.0.0; then
    set swallow_cards_ignored=y
    net_del_route swallow_probe:local
    net_del_addr swallow_probe
  fi
fi
export swallow_cards_ignored

function swallow_boot_maas {
  set net_default_interface="$1"
  set net_default_server="$maas_rack"
  export net_default_interface
  export net_default_server
  set root=(http,${maas_rack}:${maas_port})
  echo "Loading MAAS configuration for $2"
  configfile /grub/grub.cfg-$2
  echo "MAAS has no configuration for $2 (a machine MAAS does not know yet); loading its default"
  echo "configuration, which enlists the machine."
  configfile /grub/grub.cfg-default-${grub_cpu}
}

function swallow_mode_entries {
  submenu "DHCP (default) - MAAS rack ${maas_rack}:${maas_port}" --id dhcp {
    swallow_dhcp
  }
  submenu "Static IPv4 address - MAAS rack ${maas_rack}:${maas_port}" --id static {
    swallow_nic_entries
  }
}

function swallow_dhcp {
  echo "swallow Boot ISO: DHCP, then MAAS rack ${maas_rack}:${maas_port}"
  echo ""
  echo "Network cards GRUB can use:"
  set swallow_any_card=n
{{- range .Cards}}
  if [ -n "$swallow_mac_{{.}}" ]; then
    echo "  {{.}}  ${swallow_mac_{{.}}}"
    set swallow_any_card=y
  fi
{{- end}}
  if [ "$swallow_any_card" = n ]; then
    echo "  none"
  fi
  if [ "$swallow_cards_ignored" = y ]; then
    echo "  GRUB has more cards from {{.OverflowCard}} on; this Boot ISO ignores them."
  fi
  echo "A card is not listed when the firmware does not give it to boot loaders (for example its"
  echo "UEFI network stack or UEFI PXE is off) or its UEFI driver failed to start."
  while true; do
    if [ "$swallow_dhcp_asked" != y ]; then
      set swallow_dhcp_asked=y
      export swallow_dhcp_asked
      echo ""
      echo "Asking every card for a DHCP lease at once (about 15 seconds)..."
      net_bootp
      echo "(GRUB prints an error for each card that did not answer; past ten of them it also reports"
      echo "an error stack overflow, which is harmless.)"
      echo "Cards that answered:"
{{- range .Cards}}
      swallow_dhcp_answered {{.}}
{{- end}}
{{- range .Cards}}
      swallow_dhcp_try_answered {{.}}
{{- end}}
      echo ""
      echo "No card that answered reached MAAS rack ${maas_rack}:${maas_port}; next, every card is tried"
      echo "one at a time in 10 seconds."
    else
{{- range .Cards}}
      swallow_dhcp_try {{.}}
{{- end}}
      echo ""
      echo "No card reached MAAS rack ${maas_rack}:${maas_port}; trying every card again in 10 seconds."
    fi
    echo "Press Esc for the boot mode menu."
    if ! sleep --interruptible 10; then
      set timeout={{.MenuSeconds}}
      set default=dhcp
      swallow_mode_entries
      return
    fi
  done
}

function swallow_dhcp_answered {
  eval "set swallow_try_mac=\$swallow_mac_${1}"
  if [ -z "$swallow_try_mac" ]; then
    return
  fi
  eval "set swallow_answer_ip=\$net_${1}_dhcp_ip"
  if [ -z "$swallow_answer_ip" ]; then
    eval "set swallow_answered_${1}=n"
    return
  fi
  echo "  ${1} (${swallow_try_mac}): ${swallow_answer_ip}"
  eval "set swallow_answered_${1}=y"
  net_del_route "${1}:dhcp:default"
  net_del_route "${1}:dhcp:local"
  net_del_addr "${1}:dhcp"
}

function swallow_dhcp_try_answered {
  eval "set swallow_try_answered=\$swallow_answered_${1}"
  if [ "$swallow_try_answered" = y ]; then
    swallow_dhcp_try ${1}
  fi
}

function swallow_dhcp_try {
  eval "set swallow_try_mac=\$swallow_mac_${1}"
  if [ -z "$swallow_try_mac" ]; then
    return
  fi
  echo ""
  echo "${1} (${swallow_try_mac}): taking a DHCP lease..."
  if ! net_bootp ${1}; then
    echo "  ${1}: no DHCP lease."
    return
  fi
  echo "  ${1}: lease taken; addresses GRUB has now:"
  net_ls_addr
  echo "  ${1}: checking MAAS rack ${maas_rack}:${maas_port}..."
  if [ -s (http,${maas_rack}:${maas_port})/ipxe.cfg ]; then
    swallow_boot_maas "${1}_dhcp" "$swallow_try_mac"
    echo "MAAS did not boot this machine; returning to the firmware."
    sleep 10
    exit 1
  fi
  echo "  ${1}: MAAS rack ${maas_rack}:${maas_port} cannot be reached through this card."
  net_del_route "${1}:dhcp:default"
  net_del_route "${1}:dhcp:local"
  net_del_addr "${1}:dhcp"
}

function swallow_nic_entries {
{{- range .Cards}}
  if [ -n "$swallow_mac_{{.}}" ]; then
    submenu "{{.}}    ${swallow_mac_{{.}}}" --id {{.}} {
      swallow_pick_card {{.}}
    }
  fi
{{- end}}
}

function swallow_pick_card {
  set swallow_card="$1"
  eval "set swallow_card_mac=\$swallow_mac_$1"
  export swallow_card
  export swallow_card_mac
  set swallow_error=""
  export swallow_error
  set default=ip
  swallow_settings_entries
}

function swallow_settings_entries {
  if [ -n "$swallow_error" ]; then
    menuentry "Error: ${swallow_error}" --id error {
      true
    }
  fi
  submenu "Network card    ${swallow_card} (${swallow_card_mac})" --id card {
    swallow_nic_entries
  }
  submenu "IPv4 address    ${swallow_ip}" --id ip {
    swallow_edit swallow_ip "IPv4 address" mask
  }
  submenu "Subnet mask     ${swallow_mask}" --id mask {
    swallow_edit swallow_mask "Subnet mask, for example 255.255.255.0" gateway
  }
  submenu "Gateway         ${swallow_gateway}" --id gateway {
    swallow_edit swallow_gateway "Gateway, empty when the rack is on this subnet" {{if .RackIsHostname}}dns{{else}}continue{{end}}
  }
{{- if .RackIsHostname}}
  submenu "DNS server      ${swallow_dns}" --id dns {
    swallow_edit swallow_dns "DNS server" continue
  }
{{- end}}
  submenu "Continue" --id continue {
    swallow_apply
  }
  submenu "Back to the boot mode menu" --id back {
    swallow_mode_entries
  }
}

function swallow_edit {
  echo "Static IPv4 address for ${swallow_card} (${swallow_card_mac})"
  echo ""
  echo "Type the value and press Enter; an empty line clears it."
  echo "Backspace does not work here: to fix a typo, select the row again."
  echo ""
  echo -n "$2: "
  read swallow_input
  echo ""
  set "$1=$swallow_input"
  export "$1"
  set swallow_error=""
  export swallow_error
  set default="$3"
  swallow_settings_entries
}

function swallow_fail {
  set swallow_error="$2"
  export swallow_error
  set default="$1"
  swallow_settings_entries
}

function swallow_mask_prefix {
  set swallow_prefix=""
{{- range .Masks}}
  if [ "$swallow_mask" = {{.Mask}} ]; then set swallow_prefix={{.Prefix}}; fi
{{- end}}
}

function swallow_apply {
  if ! regexp '^[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+$' "$swallow_ip"; then
    swallow_fail ip "enter the IPv4 address, for example 10.0.0.5"
    return
  fi
  swallow_mask_prefix
  if [ -z "$swallow_prefix" ]; then
    swallow_fail mask "enter the subnet mask, for example 255.255.255.0"
    return
  fi
  if [ -n "$swallow_gateway" ]; then
    if ! regexp '^[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+$' "$swallow_gateway"; then
      swallow_fail gateway "enter the gateway as an IPv4 address, or leave it empty"
      return
    fi
  fi
{{- if .RackIsHostname}}
  if ! regexp '^[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+$' "$swallow_dns"; then
    swallow_fail dns "enter the DNS server that resolves ${maas_rack}"
    return
  fi
{{- end}}
  echo "Configuring ${swallow_ip}/${swallow_prefix} on ${swallow_card}"
  if ! net_add_addr swallow_static "$swallow_card" "$swallow_ip"; then
    swallow_fail ip "${swallow_ip} could not be set on ${swallow_card}"
    return
  fi
  net_del_route swallow_static:local
  if ! net_add_route swallow_static_local "${swallow_ip}/${swallow_prefix}" swallow_static; then
    net_del_addr swallow_static
    swallow_fail mask "${swallow_ip}/${swallow_prefix} is not a valid network"
    return
  fi
  if [ -n "$swallow_gateway" ]; then
    if ! net_add_route swallow_static_default 0.0.0.0/0 gw "$swallow_gateway"; then
      net_del_route swallow_static_local
      net_del_addr swallow_static
      swallow_fail gateway "the gateway ${swallow_gateway} could not be added"
      return
    fi
  fi
{{- if .RackIsHostname}}
  net_add_dns "$swallow_dns"
{{- end}}
  swallow_boot_maas swallow_static "$net_swallow_static_mac"
{{- if .RackIsHostname}}
  net_del_dns "$swallow_dns"
{{- end}}
  if [ -n "$swallow_gateway" ]; then
    net_del_route swallow_static_default
  fi
  net_del_route swallow_static_local
  net_del_addr swallow_static
  swallow_fail continue "MAAS rack ${maas_rack}:${maas_port} did not boot this machine through ${swallow_card}; check the values"
}

set timeout={{.MenuSeconds}}
set default=dhcp
swallow_mode_entries
`))
