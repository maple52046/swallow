package maas

import (
	"errors"
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

// The iPXE script, which only BIOS firmware runs, is the knowledge base's verified template with
// only the rack substituted.
func TestRenderBootScript(t *testing.T) {
	script := renderBootScript("10.140.168.20", 8080)
	for _, want := range []string{
		"#!ipxe\n",
		"set maas_rack 10.140.168.20\n",
		"dhcp || goto retry\n",
		"set next-server ${maas_rack}\n",
		"chain http://${next-server}:8080/ipxe.cfg || goto returned\n",
		"iseq ${platform} efi && exit 1 || goto retry\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("renderBootScript missing %q in:\n%s", want, script)
		}
	}
	if !strings.HasPrefix(script, "#!ipxe\n") {
		t.Errorf("script must start with the iPXE signature, got %q", script[:10])
	}
	if strings.Contains(script, "grub") {
		t.Errorf("the iPXE script must not hand UEFI to GRUB (decision 057):\n%s", script)
	}
	if got := chainURL("10.140.168.20", 5248); got != "http://10.140.168.20:5248/ipxe.cfg" {
		t.Errorf("chainURL = %q", got)
	}
	if got := uefiChainURL("10.140.168.20", 5248); got != "http://10.140.168.20:5248/grub/grub.cfg-<mac>" {
		t.Errorf("uefiChainURL = %q", got)
	}
}

// The UEFI GRUB configuration takes its own DHCP lease, derives the MAC and interface the way the
// lab-verified prototype does, exports what MAAS's configuration reads, loads the partition maps
// MAAS's local-boot search needs, and loads the rack's per-MAC configuration over HTTP.
func TestRenderGrubConfig(t *testing.T) {
	config := renderGrubConfig("10.140.168.20", 8080)
	for _, want := range []string{
		"insmod efinet\n", "insmod http\n", "insmod eval\n",
		"insmod part_gpt\n", "insmod part_msdos\n",
		"set maas_rack=10.140.168.20\n", "set maas_port=8080\n",
		"while [ -z \"$mac\" ]; do\n  net_bootp\n",
		"for card in efinet0 efinet1 ",
		" efinet15; do\n",
		"eval \"set candidate=\\$net_${card}_dhcp_mac\"\n",
		"set iface=\"${card}_dhcp\"\n",
		"set net_default_interface=\"$iface\"\n",
		"set net_default_server=\"$maas_rack\"\n",
		"export net_default_interface\n", "export net_default_server\n",
		"set root=(http,${maas_rack}:${maas_port})\n",
		"configfile /grub/grub.cfg-$mac\n",
		"exit 1\n",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("renderGrubConfig missing %q in:\n%s", want, config)
		}
	}
	// MAAS's configuration reads these variables, so they must be exported before it is loaded.
	if strings.Index(config, "export net_default_interface") > strings.Index(config, "configfile") {
		t.Errorf("variables are exported after configfile in:\n%s", config)
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
