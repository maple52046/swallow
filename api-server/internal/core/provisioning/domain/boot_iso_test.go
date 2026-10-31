package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMAASRackAddress(t *testing.T) {
	tests := []struct {
		raw      string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{raw: "10.170.168.20", wantHost: "10.170.168.20", wantPort: 5248},
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
		host, port, err := ParseMAASRackAddress(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidBootISO) {
				t.Errorf("ParseMAASRackAddress(%q) error = %v, want ErrInvalidBootISO", tc.raw, err)
			}
			continue
		}
		if err != nil || host != tc.wantHost || port != tc.wantPort {
			t.Errorf("ParseMAASRackAddress(%q) = %q, %d, %v; want %q, %d", tc.raw, host, port, err, tc.wantHost, tc.wantPort)
		}
	}
}

func TestValidateBootISOName(t *testing.T) {
	if got, err := ValidateBootISOName("  tainan-rack "); err != nil || got != "tainan-rack" {
		t.Errorf("ValidateBootISOName trims: got %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 64), "line\nbreak"} {
		if _, err := ValidateBootISOName(bad); !errors.Is(err, ErrInvalidBootISO) {
			t.Errorf("ValidateBootISOName(%q) error = %v, want ErrInvalidBootISO", bad, err)
		}
	}
}

// The iPXE script, which only BIOS firmware runs, is the knowledge base's verified template with
// only the rack substituted.
func TestRenderBootISOScript(t *testing.T) {
	script := RenderBootISOScript("10.170.168.20", 8080)
	for _, want := range []string{
		"#!ipxe\n",
		"set maas_rack 10.170.168.20\n",
		"dhcp || goto retry\n",
		"set next-server ${maas_rack}\n",
		"chain http://${next-server}:8080/ipxe.cfg || goto returned\n",
		"iseq ${platform} efi && exit 1 || goto retry\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("RenderBootISOScript missing %q in:\n%s", want, script)
		}
	}
	if !strings.HasPrefix(script, "#!ipxe\n") {
		t.Errorf("script must start with the iPXE signature, got %q", script[:10])
	}
	if strings.Contains(script, "grub") {
		t.Errorf("the iPXE script must not hand UEFI to GRUB (decision 057):\n%s", script)
	}
	if got := MAASChainURL("10.170.168.20", 5248); got != "http://10.170.168.20:5248/ipxe.cfg" {
		t.Errorf("MAASChainURL = %q", got)
	}
	if got := MAASUEFIChainURL("10.170.168.20", 5248); got != "http://10.170.168.20:5248/grub/grub.cfg-<mac>" {
		t.Errorf("MAASUEFIChainURL = %q", got)
	}
}

// The UEFI GRUB configuration takes its own DHCP lease, derives the MAC and interface the way the
// lab-verified prototype does, exports what MAAS's configuration reads, loads the partition maps
// MAAS's local-boot search needs, and loads the rack's per-MAC configuration over HTTP.
func TestRenderBootISOGrubConfig(t *testing.T) {
	config := RenderBootISOGrubConfig("10.170.168.20", 8080)
	for _, want := range []string{
		"insmod efinet\n", "insmod http\n", "insmod eval\n",
		"insmod part_gpt\n", "insmod part_msdos\n",
		"set maas_rack=10.170.168.20\n", "set maas_port=8080\n",
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
			t.Errorf("RenderBootISOGrubConfig missing %q in:\n%s", want, config)
		}
	}
	// MAAS's configuration reads these variables, so they must be exported before it is loaded.
	if strings.Index(config, "export net_default_interface") > strings.Index(config, "configfile") {
		t.Errorf("variables are exported after configfile in:\n%s", config)
	}
	content := RenderBootISOContent("10.170.168.20", 8080)
	if content.IPXEScript != RenderBootISOScript("10.170.168.20", 8080) || content.GRUBConfig != config {
		t.Errorf("RenderBootISOContent = %+v, want both renderings", content)
	}
}
