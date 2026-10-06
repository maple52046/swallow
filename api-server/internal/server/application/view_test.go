package application

import (
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// The wire contract requires kind even when a legacy in-memory projection has no stored kind.
func TestToServerItemAlwaysEmitsGPUKind(t *testing.T) {
	server := &serverdomain.Server{Observed: serverdomain.Observed{GPUs: []serverdomain.GPU{
		{Vendor: "AMD", Model: "MI300X", Count: 8},
		{Vendor: "ASPEED Technology, Inc.", Model: "ASPEED Graphics Family", Count: 1},
	}}}

	items := ToServerItem(server).GPUs
	if len(items) != 2 {
		t.Fatalf("ToServerItem GPUs length = %d, want 2", len(items))
	}
	if items[0].Kind != "compute" || items[1].Kind != "display" {
		t.Errorf("ToServerItem GPU kinds = [%q, %q], want [compute, display]", items[0].Kind, items[1].Kind)
	}
}
