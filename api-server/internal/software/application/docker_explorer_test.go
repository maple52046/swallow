package application

import (
	"context"
	"errors"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// fakeDockerClient records the calls that reach the Engine; embedding the interface makes any
// method a test does not expect panic, so an unexpected Engine call cannot pass silently.
type fakeDockerClient struct {
	softwaredomain.DockerEngineClient
	calls      []string
	pulledName string
	pulledTag  string
	pulledAuth *softwaredomain.DockerRegistryAuth
	pullErr    error
}

func (c *fakeDockerClient) ListImages(context.Context) ([]softwaredomain.DockerImage, error) {
	c.calls = append(c.calls, "ListImages")
	return []softwaredomain.DockerImage{{ID: "sha256:1"}}, nil
}

func (c *fakeDockerClient) PullImage(_ context.Context, name, tag string, auth *softwaredomain.DockerRegistryAuth) (string, error) {
	c.calls = append(c.calls, "PullImage")
	c.pulledName, c.pulledTag, c.pulledAuth = name, tag, auth
	if c.pullErr != nil {
		return "", c.pullErr
	}
	return "Status: Downloaded newer image", nil
}

func (c *fakeDockerClient) StopContainer(context.Context, string) error {
	c.calls = append(c.calls, "StopContainer")
	return nil
}

func (c *fakeDockerClient) RemoveNetwork(context.Context, string) error {
	c.calls = append(c.calls, "RemoveNetwork")
	return nil
}

// fakeDockerFactory hands out one shared fake client and records the dialled endpoint.
type fakeDockerFactory struct {
	client  *fakeDockerClient
	address string
	port    int
	built   int
}

func (f *fakeDockerFactory) For(address string, port int) (softwaredomain.DockerEngineClient, error) {
	f.address, f.port = address, port
	f.built++
	return f.client, nil
}

// fakeGuard is the Server Lock guard; err is what RequireUnlocked returns.
type fakeGuard struct {
	err     error
	checked int
}

func (g *fakeGuard) RequireUnlocked(context.Context, []string) error {
	g.checked++
	return g.err
}

type dockerExplorerFixture struct {
	explorer *DockerExplorerUseCase
	repo     *fakeAssignmentRepo
	server   *serverdomain.Server
	factory  *fakeDockerFactory
	guard    *fakeGuard
}

// newDockerExplorerFixture builds an eligible host: deployed with an address and an installed
// Docker CE assignment with enableApi=true. Tests degrade one fact at a time.
func newDockerExplorerFixture(t *testing.T) *dockerExplorerFixture {
	t.Helper()
	server := deployedServer("srv-a")
	server.Observed.Hostname = "lab-control-2"
	server.Observed.Addresses = []string{"192.168.100.7", "10.0.0.7"}
	repo := newFakeAssignmentRepo()
	_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{
		ServerID: "srv-a", Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled,
		Spec: map[string]any{"enableApi": true},
	})
	factory := &fakeDockerFactory{client: &fakeDockerClient{}}
	guard := &fakeGuard{}
	explorer := NewDockerExplorerUseCase(repo, &fakeServerRepo{servers: map[string]*serverdomain.Server{"srv-a": server}}, factory, guard)
	return &dockerExplorerFixture{explorer: explorer, repo: repo, server: server, factory: factory, guard: guard}
}

func TestDockerExplorerDialsPrimaryAddressOnDomainPort(t *testing.T) {
	f := newDockerExplorerFixture(t)
	images, err := f.explorer.ListImages(context.Background(), "srv-a")
	if err != nil {
		t.Fatalf("ListImages error = %v", err)
	}
	if len(images) != 1 {
		t.Errorf("ListImages returned %d images, want 1", len(images))
	}
	if f.factory.address != "192.168.100.7" || f.factory.port != softwaredomain.DockerEngineAPIPort {
		t.Errorf("dialled %s:%d, want 192.168.100.7:%d", f.factory.address, f.factory.port, softwaredomain.DockerEngineAPIPort)
	}
	if f.guard.checked != 0 {
		t.Errorf("a read checked the Server Lock %d times, want 0 (reads stay available on locked Servers)", f.guard.checked)
	}
}

func TestDockerExplorerEligibility(t *testing.T) {
	tests := []struct {
		name    string
		degrade func(f *dockerExplorerFixture)
		want    error
	}{
		{
			name: "server not found",
			degrade: func(f *dockerExplorerFixture) {
				f.explorer.servers = &fakeServerRepo{servers: map[string]*serverdomain.Server{}}
			},
			want: serverdomain.ErrServerNotFound,
		},
		{
			name:    "server not deployed",
			degrade: func(f *dockerExplorerFixture) { f.server.Provisioning.State = "ready" },
			want:    softwaredomain.ErrDockerHostUnavailable,
		},
		{
			name:    "server absent",
			degrade: func(f *dockerExplorerFixture) { f.server.Absent = true },
			want:    softwaredomain.ErrDockerHostUnavailable,
		},
		{
			name:    "no known address",
			degrade: func(f *dockerExplorerFixture) { f.server.Observed.Addresses = nil },
			want:    softwaredomain.ErrDockerHostUnavailable,
		},
		{
			name: "no docker-ce assignment",
			degrade: func(f *dockerExplorerFixture) {
				delete(f.repo.items, assignmentKey("srv-a", softwaredomain.KindDockerCE))
			},
			want: softwaredomain.ErrDockerNotInstalled,
		},
		{
			name: "assignment still pending",
			degrade: func(f *dockerExplorerFixture) {
				f.repo.items[assignmentKey("srv-a", softwaredomain.KindDockerCE)].State = softwaredomain.StatePending
			},
			want: softwaredomain.ErrDockerNotInstalled,
		},
		{
			name: "api disabled",
			degrade: func(f *dockerExplorerFixture) {
				f.repo.items[assignmentKey("srv-a", softwaredomain.KindDockerCE)].Spec = map[string]any{"enableApi": false}
			},
			want: softwaredomain.ErrDockerAPIDisabled,
		},
		{
			name: "legacy assignment without enableApi",
			degrade: func(f *dockerExplorerFixture) {
				f.repo.items[assignmentKey("srv-a", softwaredomain.KindDockerCE)].Spec = nil
			},
			want: softwaredomain.ErrDockerAPIDisabled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDockerExplorerFixture(t)
			tc.degrade(f)
			_, err := f.explorer.ListImages(context.Background(), "srv-a")
			if !errors.Is(err, tc.want) {
				t.Fatalf("ListImages error = %v, want %v", err, tc.want)
			}
			if f.factory.built != 0 {
				t.Errorf("an ineligible Server built %d Engine clients, want 0", f.factory.built)
			}
		})
	}
}

func TestDockerExplorerWritesRequireUnlockedServer(t *testing.T) {
	f := newDockerExplorerFixture(t)
	f.guard.err = &serverdomain.ServerLockedError{Name: "lab-control-2"}
	err := f.explorer.ActOnContainer(context.Background(), "srv-a", "c1", ContainerStop)
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("ActOnContainer on a locked Server error = %v, want ErrServerLocked", err)
	}
	if len(f.factory.client.calls) != 0 {
		t.Errorf("a locked Server reached the Engine with %v", f.factory.client.calls)
	}

	f.guard.err = nil
	if err := f.explorer.ActOnContainer(context.Background(), "srv-a", "c1", ContainerStop); err != nil {
		t.Fatalf("ActOnContainer on an unlocked Server error = %v", err)
	}
	if got := f.factory.client.calls; len(got) != 1 || got[0] != "StopContainer" {
		t.Errorf("Engine calls = %v, want [StopContainer]", got)
	}
}

// Validation runs before eligibility and lock checks, so a malformed request costs no provisioner
// or Engine I/O.
func TestDockerExplorerValidatesBeforeIO(t *testing.T) {
	tests := []struct {
		name string
		call func(uc *DockerExplorerUseCase) error
	}{
		{
			name: "container without image",
			call: func(uc *DockerExplorerUseCase) error {
				_, err := uc.CreateContainer(context.Background(), "srv-a", softwaredomain.DockerContainerSpec{Name: "web"})
				return err
			},
		},
		{
			name: "network gateway without subnet",
			call: func(uc *DockerExplorerUseCase) error {
				_, err := uc.CreateNetwork(context.Background(), "srv-a", softwaredomain.DockerNetworkSpec{Name: "app", Gateway: "10.0.0.1"})
				return err
			},
		},
		{
			name: "pull reference with whitespace",
			call: func(uc *DockerExplorerUseCase) error {
				_, err := uc.PullImage(context.Background(), "srv-a", "nginx latest")
				return err
			},
		},
		{
			name: "unknown container action",
			call: func(uc *DockerExplorerUseCase) error {
				return uc.ActOnContainer(context.Background(), "srv-a", "c1", ContainerAction("pause"))
			},
		},
		{
			name: "non-positive log tail",
			call: func(uc *DockerExplorerUseCase) error {
				_, err := uc.ContainerLogs(context.Background(), "srv-a", "c1", 0)
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDockerExplorerFixture(t)
			if err := tc.call(f.explorer); !errors.Is(err, softwaredomain.ErrInvalidDockerRequest) {
				t.Fatalf("error = %v, want ErrInvalidDockerRequest", err)
			}
			if f.factory.built != 0 || f.guard.checked != 0 {
				t.Errorf("invalid request did I/O: clients built=%d, lock checks=%d", f.factory.built, f.guard.checked)
			}
		})
	}
}

func TestDockerExplorerRefusesPredefinedNetworkBeforeIO(t *testing.T) {
	f := newDockerExplorerFixture(t)
	err := f.explorer.RemoveNetwork(context.Background(), "srv-a", "bridge")
	if !errors.Is(err, softwaredomain.ErrPredefinedDockerNetwork) {
		t.Fatalf("RemoveNetwork(bridge) error = %v, want ErrPredefinedDockerNetwork", err)
	}
	if f.factory.built != 0 {
		t.Errorf("RemoveNetwork(bridge) built %d Engine clients, want 0", f.factory.built)
	}
}

// fakeCredentialRepo is an in-memory Registry Credential store keyed by registry; err makes every
// FindAuth fail like an unreadable sealed password.
type fakeCredentialRepo struct {
	softwaredomain.RegistryCredentialRepository
	byRegistry map[string]string
	err        error
}

func (r *fakeCredentialRepo) FindAuth(_ context.Context, registry string) (*softwaredomain.RegistryCredential, string, error) {
	if r.err != nil {
		return nil, "", r.err
	}
	password, ok := r.byRegistry[registry]
	if !ok {
		return nil, "", softwaredomain.ErrRegistryCredentialNotFound
	}
	return &softwaredomain.RegistryCredential{Registry: registry, Username: "robot"}, password, nil
}

// A pull sends exactly the credential stored for the reference's registry, Docker Hub included, and
// stays anonymous when none matches.
func TestDockerExplorerPullResolvesRegistryCredential(t *testing.T) {
	tests := []struct {
		name          string
		reference     string
		wantRegistry  string
		wantAuth      *softwaredomain.DockerRegistryAuth
		wantAnonymous bool
	}{
		{
			name: "private registry with a stored credential", reference: "harbor.lab.local/team/app:1.4",
			wantRegistry: "harbor.lab.local",
			wantAuth:     &softwaredomain.DockerRegistryAuth{Username: "robot", Password: "harbor-secret", ServerAddress: "harbor.lab.local"},
		},
		{
			name: "docker hub name uses the docker.io credential", reference: "team/private",
			wantRegistry: "docker.io",
			wantAuth:     &softwaredomain.DockerRegistryAuth{Username: "robot", Password: "hub-secret", ServerAddress: "https://index.docker.io/v1/"},
		},
		{name: "registry without a credential is anonymous", reference: "ghcr.io/org/tool:2", wantRegistry: "ghcr.io", wantAnonymous: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDockerExplorerFixture(t)
			f.explorer.AttachRegistryCredentials(&fakeCredentialRepo{byRegistry: map[string]string{
				"harbor.lab.local": "harbor-secret", "docker.io": "hub-secret",
			}})
			result, err := f.explorer.PullImage(context.Background(), "srv-a", tc.reference)
			if err != nil {
				t.Fatalf("PullImage(%q) error = %v", tc.reference, err)
			}
			if result.Registry != tc.wantRegistry || result.Authenticated == tc.wantAnonymous {
				t.Errorf("PullImage(%q) registry=%q authenticated=%v, want %q authenticated=%v",
					tc.reference, result.Registry, result.Authenticated, tc.wantRegistry, !tc.wantAnonymous)
			}
			got := f.factory.client.pulledAuth
			if tc.wantAnonymous {
				if got != nil {
					t.Errorf("PullImage(%q) sent a credential for %q, want anonymous", tc.reference, got.ServerAddress)
				}
				return
			}
			if got == nil || *got != *tc.wantAuth {
				t.Errorf("PullImage(%q) auth = %+v, want %+v", tc.reference, got, tc.wantAuth)
			}
		})
	}
}

// An unreadable credential store fails the pull instead of silently pulling anonymously.
func TestDockerExplorerPullFailsWhenCredentialStoreFails(t *testing.T) {
	f := newDockerExplorerFixture(t)
	f.explorer.AttachRegistryCredentials(&fakeCredentialRepo{err: errors.New("cipher: message authentication failed")})
	if _, err := f.explorer.PullImage(context.Background(), "srv-a", "harbor.lab.local/team/app"); err == nil {
		t.Fatal("PullImage with a failing credential store succeeded, want an error")
	}
	if len(f.factory.client.calls) != 0 {
		t.Errorf("PullImage reached the Engine with %v despite the credential failure", f.factory.client.calls)
	}
}

// A registry denies an anonymous and a wrongly signed-in pull with the same message, so the failure
// must say which one happened and for which registry.
func TestDockerExplorerPullFailureNamesTheCredentialUsed(t *testing.T) {
	denied := &softwaredomain.DockerEngineError{
		Kind: softwaredomain.DockerEngineNotFound, Detail: "pull access denied for team/private",
	}
	tests := []struct {
		name        string
		credentials map[string]string
		wantDetail  string
	}{
		{
			name: "anonymous", credentials: map[string]string{"harbor.lab.local": "harbor-secret"},
			wantDetail: "pull access denied for team/private (pulled anonymously: no registry credential is saved for docker.io)",
		},
		{
			name: "signed in", credentials: map[string]string{"docker.io": "hub-secret"},
			wantDetail: "pull access denied for team/private (signed in to docker.io as robot)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDockerExplorerFixture(t)
			f.factory.client.pullErr = denied
			f.explorer.AttachRegistryCredentials(&fakeCredentialRepo{byRegistry: tc.credentials})
			_, err := f.explorer.PullImage(context.Background(), "srv-a", "team/private:1.0")
			var engineErr *softwaredomain.DockerEngineError
			if !errors.As(err, &engineErr) {
				t.Fatalf("PullImage error = %v, want a DockerEngineError", err)
			}
			if engineErr.Kind != softwaredomain.DockerEngineNotFound || engineErr.Detail != tc.wantDetail {
				t.Errorf("PullImage error = %s %q, want not_found %q", engineErr.Kind, engineErr.Detail, tc.wantDetail)
			}
		})
	}
}

// An untagged reference must resolve to latest: the Engine pulls every tag when the tag is empty.
func TestDockerExplorerPullDefaultsToLatest(t *testing.T) {
	f := newDockerExplorerFixture(t)
	result, err := f.explorer.PullImage(context.Background(), "srv-a", "registry.local:5000/team/app")
	if err != nil {
		t.Fatalf("PullImage error = %v", err)
	}
	if f.factory.client.pulledName != "registry.local:5000/team/app" || f.factory.client.pulledTag != "latest" {
		t.Errorf("pulled %q tag %q, want registry.local:5000/team/app tag latest", f.factory.client.pulledName, f.factory.client.pulledTag)
	}
	if result.Reference != "registry.local:5000/team/app:latest" {
		t.Errorf("Reference = %q, want registry.local:5000/team/app:latest", result.Reference)
	}
	if f.guard.checked != 1 {
		t.Errorf("a pull checked the Server Lock %d times, want 1", f.guard.checked)
	}
}
