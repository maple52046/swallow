package maas

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

func TestParseRackAddress(t *testing.T) {
	tests := []struct {
		raw      string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{raw: "10.140.168.20", wantHost: "10.140.168.20", wantPort: 5248},
		{raw: " 10.0.0.5:8080 ", wantHost: "10.0.0.5", wantPort: 8080},
		{raw: "MAAS-rack.lab.example", wantHost: "maas-rack.lab.example", wantPort: 5248},
		{raw: "rack1:5248", wantHost: "rack1", wantPort: 5248},
		{raw: "", wantErr: true},
		{raw: "10.0.0.5:0", wantErr: true},
		{raw: "10.0.0.5:65536", wantErr: true},
		{raw: "10.0.0.5:http", wantErr: true},
		{raw: "fd00::1", wantErr: true},
		{raw: "http://10.0.0.5", wantErr: true},
		{raw: "rack;chain evil", wantErr: true},
		{raw: "-rack", wantErr: true},
		{raw: "rack..lab", wantErr: true},
		{raw: "10.0.0.5/ipxe.cfg", wantErr: true},
	}
	for _, tc := range tests {
		host, port, err := parseRackAddress(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, provisioningdomain.ErrInvalidBootISO) {
				t.Errorf("parseRackAddress(%q) error = %v, want provisioningdomain.ErrInvalidBootISO", tc.raw, err)
			}
			continue
		}
		if err != nil || host != tc.wantHost || port != tc.wantPort {
			t.Errorf("parseRackAddress(%q) = %q, %d, %v; want %q, %d", tc.raw, host, port, err, tc.wantHost, tc.wantPort)
		}
	}
}

// The iPXE script, which only BIOS firmware runs, opens with a boot mode menu that takes DHCP
// after ten seconds. Its DHCP branch is the knowledge base's verified template with only the rack
// substituted; failures go back to the menu, whose countdown retries.
func TestRenderBootScript(t *testing.T) {
	script := renderBootScript("10.140.168.20", 8080)
	for _, want := range []string{
		"#!ipxe\n",
		"set maas_rack 10.140.168.20\n",
		"menu swallow Boot ISO for MAAS rack ${maas_rack}:8080\n",
		"item dhcp DHCP (default)\n",
		"item static Static IPv4 address\n",
		"item shell iPXE shell\n",
		"choose --default dhcp --timeout 10000 mode || goto dhcp\n",
		"dhcp || goto dhcp_failed\n",
		"prompt --timeout 5000 No DHCP lease; back to the menu in 5 seconds. ||\ngoto menu\n",
		"set next-server ${maas_rack}\n",
		"chain http://${next-server}:8080/ipxe.cfg || goto returned\n",
		"iseq ${platform} efi && exit 1 ||\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("renderBootScript missing %q in:\n%s", want, script)
		}
	}
	if !strings.HasPrefix(script, "#!ipxe\n") {
		t.Errorf("script must start with the iPXE signature, got %q", script[:10])
	}
	if strings.Contains(script, "grub") {
		t.Errorf("the iPXE script must not hand UEFI to GRUB (decision 047):\n%s", script)
	}
	if got := chainURL("10.140.168.20", 5248); got != "http://10.140.168.20:5248/ipxe.cfg" {
		t.Errorf("chainURL = %q", got)
	}
	if got := uefiChainURL("10.140.168.20", 5248); got != "http://10.140.168.20:5248/grub/grub.cfg-<mac>" {
		t.Errorf("uefiChainURL = %q", got)
	}
}

// The static branch lists the interfaces, presents a form for the address, subnet mask, and
// gateway, and opens only the chosen interface, so the unqualified ${mac} MAAS reads is that
// interface's. Choosing DHCP afterwards clears what the static branch stored on the interface.
func TestRenderBootScriptStaticBranch(t *testing.T) {
	script := renderBootScript("10.140.168.20", 5248)
	for _, want := range []string{
		":static\nifclose\n",
		"isset ${net${idx}/mac} || goto static_choose\n",
		"item net${idx} net${idx}    ${net${idx}/mac}    ${net${idx}/chip}\n",
		"choose iface || goto menu\n",
		"form Static IPv4 address for ${iface} (${${iface}/mac})\n",
		"item ${iface}/ip IPv4 address\n",
		"item ${iface}/netmask Subnet mask\n",
		"item ${iface}/gateway Gateway (empty when the rack is on this subnet)\n",
		"present || goto static\n",
		"isset ${${iface}/ip} || goto static_incomplete\n",
		"isset ${${iface}/netmask} || goto static_incomplete\n",
		"ifopen ${iface} || goto static_failed\ngoto chain\n",
		"clear ${iface}/ip\nclear ${iface}/netmask\nclear ${iface}/gateway\nclear iface\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("renderBootScript missing %q in:\n%s", want, script)
		}
	}
	if strings.Index(script, "clear iface") > strings.Index(script, "dhcp || goto dhcp_failed") {
		t.Errorf("the DHCP branch must clear the static values before it takes a lease:\n%s", script)
	}
	// A hostname rack cannot be stored in next-server, so the BIOS path has no DNS server field.
	if hostname := renderBootScript("maas-rack.lab.example", 5248); strings.Contains(hostname, "dns") {
		t.Errorf("the iPXE script must not ask for a DNS server:\n%s", hostname)
	}
}

// The UEFI GRUB configuration opens with a boot mode menu that takes DHCP after ten seconds. Both
// branches export what MAAS's configuration reads, load the partition maps MAAS's local-boot
// search needs, and load the rack's per-MAC configuration over HTTP.
func TestRenderGrubConfig(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 8080)
	for _, want := range []string{
		"insmod efinet\n", "insmod http\n", "insmod eval\n",
		"insmod part_gpt\n", "insmod part_msdos\n",
		"insmod read\n", "insmod regexp\n", "insmod sleep\n",
		"set maas_rack=10.140.168.20\n", "set maas_port=8080\n",
		"export maas_rack\n", "export maas_port\n",
		"submenu \"DHCP (default) - MAAS rack ${maas_rack}:${maas_port}\" --id dhcp {\n",
		"submenu \"Static IPv4 address - MAAS rack ${maas_rack}:${maas_port}\" --id static {\n",
		"\nset timeout=10\nset default=dhcp\nswallow_mode_entries\n",
		"if ! sleep --interruptible 10; then\n      set timeout=10\n      set default=dhcp\n      swallow_mode_entries\n",
		"set net_default_interface=\"$1\"\n",
		"set net_default_server=\"$maas_rack\"\n",
		"export net_default_interface\n", "export net_default_server\n",
		"set root=(http,${maas_rack}:${maas_port})\n",
		"configfile /grub/grub.cfg-$2\n",
		"exit 1\n",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q in:\n%s", want, config)
		}
	}
	// MAAS's configuration reads these variables, so they must be exported before it is loaded.
	if strings.Index(config, "export net_default_interface") > strings.Index(config, "configfile /grub/") {
		t.Errorf("variables are exported after configfile in:\n%s", config)
	}
	// The rack answers a MAC it does not know with 404, so the default configuration, which
	// enlists a new machine, follows the per-MAC one, as in MAAS's own grub/grub.cfg pre-loader.
	perMAC := strings.Index(config, "configfile /grub/grub.cfg-$2\n")
	fallback := strings.Index(config, "configfile /grub/grub.cfg-default-${grub_cpu}\n")
	if perMAC < 0 || fallback < perMAC {
		t.Errorf("renderGrubConfig must load grub.cfg-default-${grub_cpu} after the per-MAC configuration:\n%s", config)
	}
	chain, err := BootChainRenderer{}.RenderBootChain("10.140.168.20:8080")
	if err != nil {
		t.Fatalf("RenderBootChain error = %v", err)
	}
	if chain.Content.IPXEScript != renderBootScript("10.140.168.20", 8080) || chain.Content.GRUBConfig != config ||
		chain.ChainURL != "http://10.140.168.20:8080/ipxe.cfg" || chain.UEFIChainURL != "http://10.140.168.20:8080/grub/grub.cfg-<mac>" {
		t.Errorf("RenderBootChain = %+v, want both renderings and their URLs", chain)
	}
	if _, err := (BootChainRenderer{}).RenderBootChain("http://10.140.168.20:5240/MAAS"); !errors.Is(err, provisioningdomain.ErrInvalidBootISO) {
		t.Errorf("RenderBootChain(a MAAS URL) error = %v, want ErrInvalidBootISO", err)
	}
}

// The UEFI DHCP branch lists the cards GRUB found. Its first pass asks every card at once and tries
// only the cards that answered; later passes try every card. A card is tried by taking a lease on
// it alone, judged by net_bootp's status, and the branch boots through the first card that can
// fetch the rack's static ipxe.cfg; a card that cannot has its routes and interface removed
// before the next card is tried.
func TestRenderGrubConfigDHCPTriesEachCard(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 5248)
	for _, want := range []string{
		"echo \"Network cards GRUB can use:\"\n",
		"  if [ -n \"$swallow_mac_efinet0\" ]; then\n    echo \"  efinet0  ${swallow_mac_efinet0}\"\n",
		"    if [ \"$swallow_dhcp_asked\" != y ]; then\n      set swallow_dhcp_asked=y\n      export swallow_dhcp_asked\n",
		"      net_bootp\n      echo \"(GRUB prints an error for each card that did not answer;",
		"an error stack overflow, which is harmless.)\"\n      echo \"Cards that answered:\"\n      swallow_dhcp_answered efinet0\n",
		"      swallow_dhcp_try_answered efinet63\n",
		"    else\n      swallow_dhcp_try efinet0\n      swallow_dhcp_try efinet1\n",
		"      swallow_dhcp_try efinet16\n",
		"      swallow_dhcp_try efinet63\n      echo \"\"\n",
		"  eval \"set swallow_answer_ip=\\$net_${1}_dhcp_ip\"\n",
		"  eval \"set swallow_answered_${1}=y\"\n  net_del_route \"${1}:dhcp:default\"\n  net_del_route \"${1}:dhcp:local\"\n  net_del_addr \"${1}:dhcp\"\n",
		"  if ! net_bootp ${1}; then\n    echo \"  ${1}: no DHCP lease.\"\n    return\n  fi\n",
		"  net_ls_addr\n",
		"  if [ -s (http,${maas_rack}:${maas_port})/ipxe.cfg ]; then\n    swallow_boot_maas \"${1}_dhcp\" \"$swallow_try_mac\"\n",
		"  net_del_route \"${1}:dhcp:default\"\n  net_del_route \"${1}:dhcp:local\"\n  net_del_addr \"${1}:dhcp\"\n",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q in:\n%s", want, config)
		}
	}
	// The sticky variables only tell the first pass's answers apart; a tried card's lease is
	// judged by its own net_bootp status.
	if strings.Count(config, "_dhcp_ip") != 1 || strings.Contains(config, "_dhcp_mac") {
		t.Errorf("renderGrubConfig reads GRUB's DHCP variables outside the first pass:\n%s", config)
	}
}

// A GPU server's firmware can hand GRUB more cards than a small bound covers — pvt250 has
// fourteen Broadcom ports, an Intel pair, and the BMC's USB NIC, and its only cabled port came
// after the sixteenth card — so cards well past sixteen are probed and tried, and a card past the
// bound is reported instead of being dropped without a word.
func TestRenderGrubConfigCoversManyCards(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 5248)
	if bootISOGrubCards < 32 {
		t.Errorf("bootISOGrubCards = %d, want at least 32", bootISOGrubCards)
	}
	for _, want := range []string{
		"  if net_add_addr swallow_probe efinet16 0.0.0.0; then\n",
		"      swallow_dhcp_answered efinet16\n",
		"      swallow_dhcp_try efinet16\n",
		"submenu \"efinet16    ${swallow_mac_efinet16}\" --id efinet16 {\n",
		"  if net_add_addr swallow_probe efinet64 0.0.0.0; then\n    set swallow_cards_ignored=y\n",
		"    echo \"  GRUB has more cards from efinet64 on; this Boot ISO ignores them.\"\n",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q", want)
		}
	}
	if strings.Contains(config, "swallow_dhcp_try efinet64") {
		t.Errorf("renderGrubConfig tries efinet64, past bootISOGrubCards")
	}
}

// The UEFI static branch probes the cards once at start, removing the classful route each probe
// adds, and offers one submenu per card found; its settings screen edits one row at a time.
func TestRenderGrubConfigStaticScreens(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 5248)
	for _, want := range []string{
		"  if net_add_addr swallow_probe efinet0 0.0.0.0; then\n" +
			"    set swallow_mac_efinet0=\"$net_swallow_probe_mac\"\n" +
			"    export swallow_mac_efinet0\n" +
			"    net_del_route swallow_probe:local\n" +
			"    net_del_addr swallow_probe\n" +
			"  else\n" +
			"    set swallow_probing=n\n",
		"if net_add_addr swallow_probe efinet63 0.0.0.0; then\n",
		"submenu \"efinet3    ${swallow_mac_efinet3}\" --id efinet3 {\n      swallow_pick_card efinet3\n",
		"menuentry \"Error: ${swallow_error}\" --id error {\n",
		"submenu \"Network card    ${swallow_card} (${swallow_card_mac})\" --id card {\n",
		"swallow_edit swallow_ip \"IPv4 address\" mask\n",
		"swallow_edit swallow_mask \"Subnet mask, for example 255.255.255.0\" gateway\n",
		"swallow_edit swallow_gateway \"Gateway, empty when the rack is on this subnet\" continue\n",
		"submenu \"Continue\" --id continue {\n    swallow_apply\n",
		"submenu \"Back to the boot mode menu\" --id back {\n    swallow_mode_entries\n",
		"read swallow_input\n",
		"set \"$1=$swallow_input\"\n  export \"$1\"\n",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q in:\n%s", want, config)
		}
	}
	if strings.Contains(config, "configfile (memdisk)") || strings.Contains(config, "configfile $prefix") {
		t.Errorf("screens must be submenus: reloading the configuration deepens GRUB's script recursion:\n%s", config)
	}
}

// Continue validates the values, turns the subnet mask into the prefix the local route needs, and
// adds the static interface and its routes; when MAAS does not boot the machine it removes them
// again and shows the settings with the reason.
func TestRenderGrubConfigStaticApply(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 5248)
	for _, want := range []string{
		"if [ \"$swallow_mask\" = 255.255.255.255 ]; then set swallow_prefix=32; fi\n",
		"if [ \"$swallow_mask\" = 255.255.255.0 ]; then set swallow_prefix=24; fi\n",
		"if [ \"$swallow_mask\" = 128.0.0.0 ]; then set swallow_prefix=1; fi\n",
		"if ! regexp '^[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+$' \"$swallow_ip\"; then\n    swallow_fail ip ",
		"if [ -z \"$swallow_prefix\" ]; then\n    swallow_fail mask ",
		"if ! net_add_addr swallow_static \"$swallow_card\" \"$swallow_ip\"; then\n",
		"  net_del_route swallow_static:local\n  if ! net_add_route swallow_static_local \"${swallow_ip}/${swallow_prefix}\" swallow_static; then\n",
		"if ! net_add_route swallow_static_default 0.0.0.0/0 gw \"$swallow_gateway\"; then\n",
		"swallow_boot_maas swallow_static \"$net_swallow_static_mac\"\n",
		"  net_del_route swallow_static_local\n  net_del_addr swallow_static\n  swallow_fail continue ",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q in:\n%s", want, config)
		}
	}
	if strings.Contains(config, "= 0.0.0.0 ]") {
		t.Errorf("a zero subnet mask must not be accepted:\n%s", config)
	}
}

// A static address has no DHCP to supply a DNS server, so the UEFI settings ask for one only when
// the rack is a hostname, and Continue adds it before loading MAAS's configuration.
func TestRenderGrubConfigDNSServerForHostnameRack(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantDNS bool
	}{
		{name: "IPv4 rack needs no DNS server", host: "10.140.168.20", wantDNS: false},
		{name: "hostname rack asks for a DNS server", host: "maas-rack.lab.example", wantDNS: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := renderGrubConfig(tc.host, 5248)
			for _, part := range []string{
				"submenu \"DNS server      ${swallow_dns}\" --id dns {\n",
				"swallow_edit swallow_gateway \"Gateway, empty when the rack is on this subnet\" dns\n",
				"net_add_dns \"$swallow_dns\"\n",
				"net_del_dns \"$swallow_dns\"\n",
			} {
				if got := strings.Contains(config, part); got != tc.wantDNS {
					t.Errorf("renderGrubConfig(%q) contains %q = %v, want %v", tc.host, part, got, tc.wantDNS)
				}
			}
		})
	}
}

// grub-script-check parses the rendered configurations the way GRUB does; it is part of
// grub-common, so the test runs wherever the Boot ISO builder's tools are installed.
func TestRenderGrubConfigParses(t *testing.T) {
	checker, err := exec.LookPath("grub-script-check")
	if err != nil {
		t.Skip("grub-script-check (grub-common) is not installed")
	}
	for _, host := range []string{"10.140.168.20", "maas-rack.lab.example"} {
		path := filepath.Join(t.TempDir(), "grub.cfg")
		if err := os.WriteFile(path, []byte(renderGrubConfig(host, 5248)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(checker, path).CombinedOutput(); err != nil {
			t.Errorf("grub-script-check on the configuration for %q: %v\n%s", host, err, out)
		}
	}
}
