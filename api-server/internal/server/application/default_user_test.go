package application

import (
	"context"
	"errors"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// defaultUserServerRepo is an in-memory ServerRepository; only FindByID and SetDefaultUser are
// used, so the embedded interface makes any other call panic.
type defaultUserServerRepo struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
	saved   []string
}

func (r *defaultUserServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	copied := *server
	return &copied, nil
}

func (r *defaultUserServerRepo) SetDefaultUser(_ context.Context, id string, user string) error {
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	server.DefaultUser = user
	r.saved = append(r.saved, user)
	return nil
}

type defaultUserGuard struct{ err error }

func (g defaultUserGuard) RequireUnlocked(context.Context, []string) error { return g.err }

// fakeHostAccess records the logins; authorizeErr and checkErr are what each returns.
type fakeHostAccess struct {
	authorizeErr error
	checkErr     error
	sudo         serverdomain.SudoAccess
	calls        []string
	passwords    []string
	targets      []serverdomain.HostLoginTarget
}

func (h *fakeHostAccess) AuthorizeDeploymentKey(_ context.Context, target serverdomain.HostLoginTarget, user, password string) error {
	h.calls = append(h.calls, "authorize:"+user)
	h.passwords = append(h.passwords, password)
	h.targets = append(h.targets, target)
	return h.authorizeErr
}

func (h *fakeHostAccess) CheckDeploymentKeyLogin(_ context.Context, target serverdomain.HostLoginTarget, user string) (serverdomain.SudoAccess, error) {
	h.calls = append(h.calls, "check:"+user)
	h.targets = append(h.targets, target)
	return h.sudo, h.checkErr
}

func deployedServer(id string) *serverdomain.Server {
	return &serverdomain.Server{
		ID:           id,
		Source:       serverdomain.Source{SiteID: "site-a"},
		Observed:     serverdomain.Observed{Hostname: "tainan-ci", Addresses: []string{"10.0.0.9", "10.0.1.9"}},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", DeployedImageDefaultUser: "ubuntu"},
	}
}

func newDefaultUserFixture(server *serverdomain.Server) (*DefaultUserUseCase, *defaultUserServerRepo, *fakeHostAccess) {
	repo := &defaultUserServerRepo{servers: map[string]*serverdomain.Server{server.ID: server}}
	hosts := &fakeHostAccess{sudo: serverdomain.SudoPasswordless}
	return NewDefaultUserUseCase(repo, defaultUserGuard{}, hosts), repo, hosts
}

// With a password the key is installed first, then a key login proves it before anything is saved.
func TestSetDefaultUserInstallsKeyThenVerifies(t *testing.T) {
	uc, repo, hosts := newDefaultUserFixture(deployedServer("srv-1"))
	result, err := uc.Set(context.Background(), SetDefaultUserInput{ServerID: "srv-1", User: " amd ", Password: " p@ss "})
	if err != nil {
		t.Fatalf("Set error = %v", err)
	}
	if want := []string{"authorize:amd", "check:amd"}; !equalStrings(hosts.calls, want) {
		t.Errorf("host logins = %v, want %v", hosts.calls, want)
	}
	if hosts.passwords[0] != " p@ss " {
		t.Errorf("password passed = %q, want it verbatim", hosts.passwords[0])
	}
	if target := hosts.targets[0]; target.Address != "10.0.0.9" || target.SiteID != "site-a" || target.Name != "tainan-ci" {
		t.Errorf("login target = %+v, want the primary address on the Server's Site", target)
	}
	if !result.KeyInstalled || result.Sudo != serverdomain.SudoPasswordless {
		t.Errorf("result = keyInstalled %v sudo %q, want true, passwordless", result.KeyInstalled, result.Sudo)
	}
	if user, source := result.Server.EffectiveDefaultUser(); user != "amd" || source != serverdomain.DefaultUserSourceServer {
		t.Errorf("effective default user = %q (%s), want amd (server)", user, source)
	}
	if !equalStrings(repo.saved, []string{"amd"}) {
		t.Errorf("saved = %v, want [amd]", repo.saved)
	}
}

// Without a password the key must already work; a rejected key saves nothing.
func TestSetDefaultUserWithoutPasswordRequiresAWorkingKey(t *testing.T) {
	uc, repo, hosts := newDefaultUserFixture(deployedServer("srv-1"))
	hosts.checkErr = serverdomain.ErrDeploymentKeyRejected
	_, err := uc.Set(context.Background(), SetDefaultUserInput{ServerID: "srv-1", User: "amd"})
	var userErr *serverdomain.DefaultUserError
	if !errors.As(err, &userErr) || !errors.Is(err, serverdomain.ErrDeploymentKeyRejected) {
		t.Fatalf("Set error = %v, want a DefaultUserError wrapping ErrDeploymentKeyRejected", err)
	}
	if !equalStrings(hosts.calls, []string{"check:amd"}) {
		t.Errorf("host logins = %v, want only the key check", hosts.calls)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %v after a rejected key, want nothing", repo.saved)
	}
}

func TestSetDefaultUserRejections(t *testing.T) {
	notDeployed := deployedServer("srv-1")
	notDeployed.Provisioning.State = "ready"
	noAddress := deployedServer("srv-1")
	noAddress.Observed.Addresses = nil
	tests := []struct {
		name     string
		server   *serverdomain.Server
		input    SetDefaultUserInput
		guardErr error
		hosts    fakeHostAccess
		want     error
	}{
		{name: "invalid name", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-1", User: "Bad User"}, want: serverdomain.ErrInvalidDefaultUser},
		{name: "unknown server", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-x", User: "amd"}, want: serverdomain.ErrServerNotFound},
		{name: "not deployed", server: notDeployed, input: SetDefaultUserInput{ServerID: "srv-1", User: "amd"}, want: serverdomain.ErrDefaultUserNotDeployed},
		{name: "no address", server: noAddress, input: SetDefaultUserInput{ServerID: "srv-1", User: "amd"}, want: serverdomain.ErrDefaultUserNotDeployed},
		{name: "locked", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-1", User: "amd"}, guardErr: &serverdomain.ServerLockedError{Name: "tainan-ci"}, want: serverdomain.ErrServerLocked},
		{name: "password rejected", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-1", User: "amd", Password: "x"}, hosts: fakeHostAccess{authorizeErr: serverdomain.ErrHostPasswordRejected}, want: serverdomain.ErrHostPasswordRejected},
		{name: "unreachable", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-1", User: "amd"}, hosts: fakeHostAccess{checkErr: serverdomain.ErrHostUnreachable}, want: serverdomain.ErrHostUnreachable},
		{name: "no deployment key", server: deployedServer("srv-1"), input: SetDefaultUserInput{ServerID: "srv-1", User: "amd"}, hosts: fakeHostAccess{checkErr: serverdomain.ErrDeploymentKeyMissing}, want: serverdomain.ErrDeploymentKeyMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &defaultUserServerRepo{servers: map[string]*serverdomain.Server{tc.server.ID: tc.server}}
			hosts := tc.hosts
			uc := NewDefaultUserUseCase(repo, defaultUserGuard{err: tc.guardErr}, &hosts)
			if _, err := uc.Set(context.Background(), tc.input); !errors.Is(err, tc.want) {
				t.Errorf("Set(%+v) error = %v, want %v", tc.input, err, tc.want)
			}
			if len(repo.saved) != 0 {
				t.Errorf("saved = %v, want nothing on failure", repo.saved)
			}
		})
	}
}

func TestClearDefaultUser(t *testing.T) {
	server := deployedServer("srv-1")
	server.DefaultUser = "amd"
	uc, repo, hosts := newDefaultUserFixture(server)
	if err := uc.Clear(context.Background(), "srv-1"); err != nil {
		t.Fatalf("Clear error = %v", err)
	}
	if !equalStrings(repo.saved, []string{""}) || len(hosts.calls) != 0 {
		t.Errorf("Clear saved %v and logged in %v, want [\"\"] and no host contact", repo.saved, hosts.calls)
	}

	locked := NewDefaultUserUseCase(repo, defaultUserGuard{err: &serverdomain.ServerLockedError{Name: "tainan-ci"}}, hosts)
	if err := locked.Clear(context.Background(), "srv-1"); !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Errorf("Clear on a locked Server error = %v, want ErrServerLocked", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
