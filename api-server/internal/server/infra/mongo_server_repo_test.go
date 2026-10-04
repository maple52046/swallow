package infra

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
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
