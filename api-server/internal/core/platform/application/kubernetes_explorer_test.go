package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// explorerPlatforms is a FindByID-only Platform repository.
type explorerPlatforms struct {
	platformdomain.PlatformRepository
	platforms map[string]*platformdomain.Platform
}

func (r explorerPlatforms) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	if platform, ok := r.platforms[id]; ok {
		return platform, nil
	}
	return nil, platformdomain.ErrPlatformNotFound
}

// explorerLifecycle reports every listed Platform as swallow-deployed and the rest as registered.
type explorerLifecycle struct{ deployed map[string]bool }

func (l explorerLifecycle) Read(_ context.Context, ids []string) (map[string]platformdomain.LifecycleSnapshot, error) {
	snapshots := make(map[string]platformdomain.LifecycleSnapshot, len(ids))
	for _, id := range ids {
		origin := platformdomain.PlatformOriginRegistered
		if l.deployed[id] {
			origin = platformdomain.PlatformOriginDeployed
		}
		snapshots[id] = platformdomain.LifecycleSnapshot{Origin: origin}
	}
	return snapshots, nil
}

// credentialClient is a cluster client that names the credential it was built from.
type credentialClient struct {
	platformdomain.KubernetesClusterClient
	credential string
}

func (credentialClient) ListNodes(context.Context) ([]platformdomain.KubernetesNode, error) {
	return []platformdomain.KubernetesNode{{Name: "node-1"}}, nil
}

// readClientFactory builds only the read client and records each build.
type readClientFactory struct{ builds *[]string }

func (f readClientFactory) For(context.Context, *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	*f.builds = append(*f.builds, "read")
	return credentialClient{credential: "read"}, nil
}

// operatorClientFactory also builds the operator client.
type operatorClientFactory struct{ readClientFactory }

func (f operatorClientFactory) ForOperator(context.Context, *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	*f.builds = append(*f.builds, "operator")
	return credentialClient{credential: "operator"}, nil
}

// noServers answers every Server list with none.
type noServers struct{ serverdomain.ServerRepository }

func (noServers) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return serverdomain.ListResult{}, nil
}

func newExplorerForTest(factory platformdomain.KubernetesClientFactory) *KubernetesExplorerUseCase {
	platforms := explorerPlatforms{platforms: map[string]*platformdomain.Platform{
		"k8s":        {ID: "k8s", Type: platformdomain.PlatformTypeKubernetes, IntegrationID: "i1"},
		"registered": {ID: "registered", Type: platformdomain.PlatformTypeKubernetes, IntegrationID: "i2"},
		"slurm":      {ID: "slurm", Type: platformdomain.PlatformTypeSlurm, IntegrationID: "i3"},
	}}
	lifecycle := explorerLifecycle{deployed: map[string]bool{"k8s": true, "slurm": true}}
	return NewKubernetesExplorerUseCase(platforms, lifecycle, factory, noServers{})
}

// A read request gets the client of the read credential and an operate request the client of the
// operator credential; the node list, a read, never builds the operator client.
func TestKubernetesExplorerBuildsTheClientOfTheRequestedAccess(t *testing.T) {
	cases := []struct {
		name   string
		access platformdomain.ConnectionAccess
		want   string
	}{
		{name: "read", access: platformdomain.ConnectionRead, want: "read"},
		{name: "operate", access: platformdomain.ConnectionOperate, want: "operator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var builds []string
			explorer := newExplorerForTest(operatorClientFactory{readClientFactory{&builds}})
			client, err := explorer.Client(context.Background(), "k8s", tc.access)
			if err != nil {
				t.Fatalf("Client(%v): %v", tc.access, err)
			}
			if got := client.(credentialClient).credential; got != tc.want || len(builds) != 1 {
				t.Errorf("Client(%v) built from %q after %v, want %q from one build", tc.access, got, builds, tc.want)
			}
		})
	}

	var builds []string
	explorer := newExplorerForTest(operatorClientFactory{readClientFactory{&builds}})
	if _, err := explorer.Nodes(context.Background(), "k8s"); err != nil || len(builds) != 1 || builds[0] != "read" {
		t.Errorf("Nodes built %v, err %v; want only the read client", builds, err)
	}
}

// A factory without the operator capability gives its read client to an operate request, which
// the cluster may then deny but which never runs with more than the read credential.
func TestKubernetesExplorerOperateFallsBackToTheReadClient(t *testing.T) {
	var builds []string
	explorer := newExplorerForTest(readClientFactory{&builds})
	client, err := explorer.Client(context.Background(), "k8s", platformdomain.ConnectionOperate)
	if err != nil {
		t.Fatalf("Client(operate): %v", err)
	}
	if got := client.(credentialClient).credential; got != "read" {
		t.Errorf("Client(operate) without the capability built from %q, want read", got)
	}
}

// Eligibility is the same for either access and is checked before any client, and so any
// credential, is built.
func TestKubernetesExplorerChecksEligibilityBeforeAnyCredential(t *testing.T) {
	cases := []struct {
		platformID string
		wantErr    error
	}{
		{platformID: "registered", wantErr: platformdomain.ErrClusterExplorerUnavailable},
		{platformID: "slurm", wantErr: platformdomain.ErrPlatformNotKubernetes},
		{platformID: "missing", wantErr: platformdomain.ErrPlatformNotFound},
	}
	for _, tc := range cases {
		for _, access := range []platformdomain.ConnectionAccess{platformdomain.ConnectionRead, platformdomain.ConnectionOperate} {
			var builds []string
			explorer := newExplorerForTest(operatorClientFactory{readClientFactory{&builds}})
			if _, err := explorer.Client(context.Background(), tc.platformID, access); !errors.Is(err, tc.wantErr) || len(builds) != 0 {
				t.Errorf("Client(%q, %v) = %v after building %v; want %v and no client", tc.platformID, access, err, builds, tc.wantErr)
			}
		}
	}
}
