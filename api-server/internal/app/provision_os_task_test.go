package app

import (
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// A provision-os Task reads by host, like the other Tasks that act on one Server, and falls back the
// way the Server's display name does when the provisioner reports no hostname.
func TestProvisionOSTaskName(t *testing.T) {
	tests := []struct {
		name   string
		server *serverdomain.Server
		want   string
	}{
		{
			name:   "hostname",
			server: &serverdomain.Server{ID: "server-a", Observed: serverdomain.Observed{Hostname: "gpu-node-01", FQDN: "gpu-node-01.lab"}},
			want:   "Provision and verify operating system on gpu-node-01",
		},
		{
			name:   "FQDN without a hostname",
			server: &serverdomain.Server{ID: "server-b", Observed: serverdomain.Observed{FQDN: "gpu-node-02.lab"}},
			want:   "Provision and verify operating system on gpu-node-02.lab",
		},
		{
			name:   "id when the provisioner reports no name",
			server: &serverdomain.Server{ID: "server-c"},
			want:   "Provision and verify operating system on server-c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := provisionOSTaskName(tc.server); got != tc.want {
				t.Errorf("provisionOSTaskName(%+v) = %q, want %q", tc.server.Observed, got, tc.want)
			}
		})
	}
}
