package domain

import (
	"errors"
	"strings"
	"testing"
)

// Subnet DNS is validated before any provisioner call: addresses are canonicalized in order, an
// empty list clears the setting, and malformed, zoned, repeated, or too many entries are refused.
func TestNormalizeSubnetDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		want    string
		wantErr bool
	}{
		{name: "empty clears", raw: nil, want: ""},
		{name: "addresses keep order and are trimmed", raw: []string{" 165.204.124.158", "165.204.219.249 "}, want: "165.204.124.158,165.204.219.249"},
		{name: "IPv6 is canonicalized", raw: []string{"2001:DB8:0:0::53"}, want: "2001:db8::53"},
		{name: "blank entry is refused", raw: []string{"10.0.0.1", " "}, wantErr: true},
		{name: "hostname is refused", raw: []string{"dns.example.com"}, wantErr: true},
		{name: "zone is refused", raw: []string{"fe80::1%eth0"}, wantErr: true},
		{name: "repeat after canonicalization is refused", raw: []string{"2001:db8::53", "2001:DB8::53"}, wantErr: true},
		{name: "more than the maximum is refused", raw: strings.Split("10.0.0.1,10.0.0.2,10.0.0.3,10.0.0.4,10.0.0.5,10.0.0.6,10.0.0.7,10.0.0.8,10.0.0.9", ","), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeSubnetDNSServers(test.raw)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidSubnetDNS) {
					t.Fatalf("NormalizeSubnetDNSServers(%q) error = %v, want ErrInvalidSubnetDNS", test.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSubnetDNSServers(%q) error = %v", test.raw, err)
			}
			if joined := strings.Join(got, ","); joined != test.want {
				t.Errorf("NormalizeSubnetDNSServers(%q) = %q, want %q", test.raw, joined, test.want)
			}
		})
	}
}
