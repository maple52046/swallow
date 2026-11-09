package hostenroll

import "testing"

// The source address is the one the routing table picks for the endpoint's host; for a loopback
// endpoint that is the loopback address, which the address check then skips.
func TestSourceAddress(t *testing.T) {
	got, err := sourceAddress("http://127.0.0.1:5240/MAAS")
	if err != nil || got != "127.0.0.1" {
		t.Errorf("sourceAddress(loopback) = %q, %v; want 127.0.0.1", got, err)
	}
	if _, err := sourceAddress("not a url"); err == nil {
		t.Error("sourceAddress(not a url) error = nil, want one")
	}
}
