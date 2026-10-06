package infra

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Projections stored before decision 048 hold the MAAS word `commissioning`; they must read,
// and be filterable, as the generic `inspecting` until reconcile rewrites them.
func TestProvisioningStateLegacyCompatibility(t *testing.T) {
	reads := []struct {
		stored, want string
	}{
		{stored: "commissioning", want: "inspecting"},
		{stored: "inspecting", want: "inspecting"},
		{stored: "releasing", want: "releasing"},
		{stored: "", want: ""},
	}
	for _, tc := range reads {
		if got := provisioningStateFromStore(tc.stored); got != tc.want {
			t.Errorf("provisioningStateFromStore(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}

	filters := []struct {
		state string
		want  any
	}{
		{state: "inspecting", want: bson.M{"$in": []string{"inspecting", "commissioning"}}},
		{state: "releasing", want: "releasing"},
	}
	for _, tc := range filters {
		if got := provisioningStateQuery(tc.state); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("provisioningStateQuery(%q) = %#v, want %#v", tc.state, got, tc.want)
		}
	}
}

// The running Boot Media apply round-trips through its document, including a phase end.
func TestBootMediaApplyDocRoundTrip(t *testing.T) {
	ends := time.Date(2026, 10, 5, 1, 36, 25, 0, time.UTC)
	apply := &serverdomain.BootMediaApply{
		ISOID: "iso-1", Phase: serverdomain.BootMediaPhaseSettling,
		StartedAt:      time.Date(2026, 10, 5, 1, 32, 33, 0, time.UTC),
		PhaseStartedAt: time.Date(2026, 10, 5, 1, 33, 25, 0, time.UTC), PhaseEndsAt: &ends,
	}
	doc := newBootMediaApplyDoc(apply)
	got := toBootMediaApply(&doc)
	if got.ISOID != apply.ISOID || got.Phase != apply.Phase || !got.StartedAt.Equal(apply.StartedAt) ||
		!got.PhaseStartedAt.Equal(apply.PhaseStartedAt) || got.PhaseEndsAt == nil || !got.PhaseEndsAt.Equal(ends) {
		t.Errorf("round trip = %+v, want %+v", got, apply)
	}
}

// GPU documents created before kind existed must become correctly classified as soon as the
// upgraded service reads them; waiting for a later inventory sweep would keep capacity wrong.
func TestGPUKindLegacyCompatibility(t *testing.T) {
	doc := &serverDoc{GPUs: []gpuDoc{
		{Vendor: "AMD", Model: "MI300X", Count: 8},
		{Vendor: "ASPEED Technology, Inc.", Model: "ASPEED Graphics Family", Count: 1},
	}}

	gpus := toServer(doc).Observed.GPUs
	if len(gpus) != 2 {
		t.Fatalf("toServer legacy GPUs length = %d, want 2", len(gpus))
	}
	if gpus[0].Kind != serverdomain.GPUKindCompute || gpus[1].Kind != serverdomain.GPUKindDisplay {
		t.Errorf("toServer legacy GPU kinds = [%q, %q], want [%q, %q]", gpus[0].Kind, gpus[1].Kind, serverdomain.GPUKindCompute, serverdomain.GPUKindDisplay)
	}

	docs := gpuDocs(gpus)
	if docs[0].Kind != "compute" || docs[1].Kind != "display" {
		t.Errorf("gpuDocs kinds = [%q, %q], want [compute, display]", docs[0].Kind, docs[1].Kind)
	}
}
