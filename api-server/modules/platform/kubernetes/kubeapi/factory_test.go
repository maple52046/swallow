package kubeapi

import (
	"context"
	"errors"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// twoCredentials resolves every Platform to the read or the operator Connection of one
// Integration, and records which one was asked for.
type twoCredentials struct{ asked []string }

func (s *twoCredentials) Connection(context.Context, *platformdomain.Platform) (platformdomain.Connection, error) {
	s.asked = append(s.asked, "read")
	return platformdomain.Connection{ProviderKind: ProviderKind, Endpoint: "https://10.0.0.1:6443", Token: "reader", Timeout: time.Second}, nil
}

func (s *twoCredentials) OperatorConnection(context.Context, *platformdomain.Platform) (platformdomain.Connection, error) {
	s.asked = append(s.asked, "operate")
	return platformdomain.Connection{ProviderKind: ProviderKind, Endpoint: "https://10.0.0.1:6443", Token: "operator", Timeout: time.Second}, nil
}

// For builds the explorer client on the read credential and ForOperator on the operator
// credential, each resolving only its own Connection; a Platform that cannot have an explorer
// client gets the eligibility error before any Connection, and so any credential, is resolved.
func TestClientFactoryBuildsEachClientFromItsOwnCredential(t *testing.T) {
	platform := &platformdomain.Platform{Name: "lab", Type: platformdomain.PlatformTypeKubernetes, IntegrationID: "i1"}
	cases := []struct {
		name      string
		build     func(f *ClientFactory) (platformdomain.KubernetesClusterClient, error)
		wantToken string
	}{
		{name: "For", build: func(f *ClientFactory) (platformdomain.KubernetesClusterClient, error) {
			return f.For(context.Background(), platform)
		}, wantToken: "reader"},
		{name: "ForOperator", build: func(f *ClientFactory) (platformdomain.KubernetesClusterClient, error) {
			return f.ForOperator(context.Background(), platform)
		}, wantToken: "operator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &twoCredentials{}
			client, err := tc.build(NewClientFactory(source))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := client.(*Client).token; got != tc.wantToken || len(source.asked) != 1 {
				t.Errorf("%s: client token %q after resolving %v, want %q from one Connection", tc.name, got, source.asked, tc.wantToken)
			}
		})
	}

	source := &twoCredentials{}
	factory := NewClientFactory(source)
	for _, ineligible := range []struct {
		platform *platformdomain.Platform
		wantErr  error
	}{
		{&platformdomain.Platform{Type: platformdomain.PlatformTypeSlurm, IntegrationID: "i1"}, platformdomain.ErrPlatformNotKubernetes},
		{&platformdomain.Platform{Type: platformdomain.PlatformTypeKubernetes}, platformdomain.ErrClusterExplorerUnavailable},
	} {
		if _, err := factory.ForOperator(context.Background(), ineligible.platform); !errors.Is(err, ineligible.wantErr) {
			t.Errorf("ForOperator(%+v) error = %v, want %v", ineligible.platform, err, ineligible.wantErr)
		}
	}
	if len(source.asked) != 0 {
		t.Errorf("an ineligible Platform resolved %v, want no Connection", source.asked)
	}
}
