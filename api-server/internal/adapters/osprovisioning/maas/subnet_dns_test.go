package maas

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// subnetUpdateRecorder answers PUT /subnets/{id}/ with status and records each multipart form it
// received, every value of every field, so a test can see exactly what the adapter sent.
type subnetUpdateRecorder struct {
	mu     sync.Mutex
	status int
	paths  []string
	forms  []map[string][]string
}

func newSubnetUpdateRecorder(t *testing.T, fake *fakeMAAS, status int) *subnetUpdateRecorder {
	t.Helper()
	recorder := &subnetUpdateRecorder{status: status}
	fake.mux.HandleFunc("PUT "+apiPrefix+"/subnets/{id}/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		recorder.mu.Lock()
		recorder.paths = append(recorder.paths, r.PathValue("id"))
		recorder.forms = append(recorder.forms, r.MultipartForm.Value)
		recorder.mu.Unlock()
		w.WriteHeader(recorder.status)
		_, _ = w.Write([]byte(`{}`))
	})
	return recorder
}

// A Subnet DNS write is one subnet update that carries only dns_servers, space-separated, so MAAS
// keeps every other subnet setting; clearing sends an explicit empty value rather than omitting it.
func TestSetSubnetDNSServersSendsOnlyDNSServers(t *testing.T) {
	fake := newFakeMAAS(t)
	recorder := newSubnetUpdateRecorder(t, fake, http.StatusOK)
	provider := newTestProvider(t, fake)

	if err := provider.SetSubnetDNSServers(context.Background(), "1", []string{"165.204.124.158", "165.204.219.249"}); err != nil {
		t.Fatalf("SetSubnetDNSServers(set): %v", err)
	}
	if err := provider.SetSubnetDNSServers(context.Background(), "1", nil); err != nil {
		t.Fatalf("SetSubnetDNSServers(clear): %v", err)
	}
	if len(recorder.forms) != 2 || recorder.paths[0] != "1" {
		t.Fatalf("subnet updates = %v on %v, want two on subnet 1", recorder.forms, recorder.paths)
	}
	for index, want := range []string{"165.204.124.158 165.204.219.249", ""} {
		form := recorder.forms[index]
		if len(form) != 1 || len(form["dns_servers"]) != 1 || form["dns_servers"][0] != want {
			t.Errorf("update %d form = %v, want only dns_servers=%q", index, form, want)
		}
	}
}

// Attachment follows MAAS's own markers, shaped after the lab: a controller interface (even a
// parentless libvirt bridge), MAAS DHCP, a boot interface (a Hyper-V VM NIC has no vendor), or a
// NIC with a vendor — directly or under a bond. Docker and Kubernetes bridges never count, even
// though their veth parents are typed physical.
func TestAttachedSubnetIDsUsesMAASNetworkMarkers(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/subnets/{$}", http.StatusOK, `[
	  {"id": 1, "cidr": "10.170.168.0/23", "vlan": {"id": 1, "dhcp_on": false}},
	  {"id": 2, "cidr": "192.168.100.0/24", "vlan": {"id": 2, "dhcp_on": true}},
	  {"id": 6, "cidr": "192.168.110.0/24", "vlan": {"id": 6, "dhcp_on": false}},
	  {"id": 9, "cidr": "172.19.0.0/16", "vlan": {"id": 9, "dhcp_on": false}},
	  {"id": 13, "cidr": "172.18.0.0/16", "vlan": {"id": 13, "dhcp_on": false}},
	  {"id": 14, "cidr": "192.168.68.0/24", "vlan": {"id": 14, "dhcp_on": false}},
	  {"id": 20, "cidr": "10.20.0.0/24", "vlan": {"id": 20, "dhcp_on": false}},
	  {"id": 21, "cidr": "10.30.0.0/24", "vlan": {"id": 21, "dhcp_on": false}},
	  {"id": 22, "cidr": "192.168.49.0/24", "vlan": {"id": 22, "dhcp_on": false}}
	]`)
	fake.respond("GET "+apiPrefix+"/rackcontrollers/{$}", http.StatusOK, `[{"interface_set": [
	  {"id": 1, "name": "eno1", "type": "physical", "links": [{"id": 1, "subnet": {"id": 1}}]},
	  {"id": 2, "name": "virbr-ipxe", "type": "bridge", "parents": [], "links": [{"id": 2, "subnet": {"id": 6}}]}
	]}]`)
	fake.respond("GET "+apiPrefix+"/regioncontrollers/{$}", http.StatusOK, `[]`)
	fake.respond("GET "+apiPrefix+"/machines/{$}", http.StatusOK, `[
	  {"boot_interface": {"id": 10}, "interface_set": [
	    {"id": 10, "name": "ens5f0np0", "type": "physical", "vendor": "Intel Corporation", "links": [{"id": 10, "subnet": {"id": 1}}]},
	    {"id": 11, "name": "br-799c0543f9b6", "type": "bridge", "parents": [], "links": [{"id": 11, "subnet": {"id": 9}}]},
	    {"id": 12, "name": "veth0de6cbd", "type": "physical", "vendor": "", "links": []},
	    {"id": 13, "name": "docker_gwbridge", "type": "bridge", "parents": ["veth0de6cbd"], "links": [{"id": 13, "subnet": {"id": 13}}]},
	    {"id": 14, "name": "kube-bridge", "type": "bridge", "parents": ["veth0de6cbd", "kube-bridge"], "links": [{"id": 14, "subnet": {"id": 14}}]},
	    {"id": 15, "name": "enp193s0f0np0", "type": "physical", "vendor": "Mellanox Technologies", "links": []},
	    {"id": 16, "name": "bond0", "type": "bond", "parents": ["enp193s0f0np0"], "links": [{"id": 16, "subnet": {"id": 20}}]}
	  ]},
	  {"boot_interface": {"id": 30}, "interface_set": [
	    {"id": 30, "name": "eth0", "type": "physical", "vendor": "", "links": [{"id": 30, "subnet": {"id": 21}}]}
	  ]}
	]`)

	attached, err := newTestProvider(t, fake).AttachedSubnetIDs(context.Background())
	if err != nil {
		t.Fatalf("AttachedSubnetIDs: %v", err)
	}
	want := map[string]bool{"1": true, "2": true, "6": true, "20": true, "21": true}
	for _, id := range []string{"1", "2", "6", "9", "13", "14", "20", "21", "22"} {
		if attached[id] != want[id] {
			t.Errorf("AttachedSubnetIDs()[%s] = %v, want %v", id, attached[id], want[id])
		}
	}
}

// MAAS's refusals of a subnet update keep their meaning: an unknown subnet is a domain miss, and a
// non-admin account is an Integration credential problem, not a rejected value.
func TestSetSubnetDNSServersTranslatesRefusals(t *testing.T) {
	tests := []struct {
		name   string
		status int
		check  func(error) bool
	}{
		{name: "unknown subnet", status: http.StatusNotFound, check: func(err error) bool {
			return errors.Is(err, provisioningdomain.ErrSubnetNotFound)
		}},
		{name: "not an administrator", status: http.StatusForbidden, check: func(err error) bool {
			var providerErr *provisioningdomain.ProviderError
			return errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorAuth
		}},
		{name: "invalid value", status: http.StatusBadRequest, check: func(err error) bool {
			var providerErr *provisioningdomain.ProviderError
			return errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorRejected
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			newSubnetUpdateRecorder(t, fake, test.status)
			err := newTestProvider(t, fake).SetSubnetDNSServers(context.Background(), "9", []string{"10.0.0.53"})
			if !test.check(err) {
				t.Errorf("SetSubnetDNSServers with MAAS HTTP %d error = %v", test.status, err)
			}
		})
	}
}
