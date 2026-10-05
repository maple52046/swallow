package infra

import (
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// The stored name key is case-folded so the unique index enforces case-insensitive names per
// Integration, and the record round-trips unchanged.
func TestBootISODocRoundTrip(t *testing.T) {
	iso := &provisioningdomain.BootISO{
		ID: "iso-1", Name: "Tainan-Rack", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5",
		ChainURL: "http://10.1.0.5:5248/ipxe.cfg", Script: "#!ipxe\n", IPXEVersion: "v2.0.0",
		SizeBytes: 1024, SHA256: "abc", CreatedAt: time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC), CreatedBy: "admin",
	}
	doc := newBootISODoc(iso)
	if doc.NormalizedName != "tainan-rack" || doc.Name != "Tainan-Rack" {
		t.Errorf("doc names = %q / %q, want the folded key and the name as given", doc.NormalizedName, doc.Name)
	}
	if got := toBootISO(&doc); *got != *iso {
		t.Errorf("round trip = %+v, want %+v", got, iso)
	}
}
