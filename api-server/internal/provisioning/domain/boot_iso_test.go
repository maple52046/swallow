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

// The rendered script is the knowledge base's verified template with only the rack substituted.
func TestRenderBootISOScript(t *testing.T) {
	script := RenderBootISOScript("10.170.168.20", 5248)
	for _, want := range []string{
		"#!ipxe\n",
		"set maas_rack 10.170.168.20\n",
		"dhcp || goto retry\n",
		"set next-server ${maas_rack}\n",
		"chain http://${next-server}:5248/ipxe.cfg || goto returned\n",
		"iseq ${platform} efi && exit 1 || goto retry\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("RenderBootISOScript missing %q in:\n%s", want, script)
		}
	}
	if !strings.HasPrefix(script, "#!ipxe\n") {
		t.Errorf("script must start with the iPXE signature, got %q", script[:10])
	}
	if got := MAASChainURL("10.170.168.20", 5248); got != "http://10.170.168.20:5248/ipxe.cfg" {
		t.Errorf("MAASChainURL = %q", got)
	}
}
