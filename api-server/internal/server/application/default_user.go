package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// deployedState is the normalized provisioning state of a Server running an OS. Only such a
// Server has an account to log in to.
const deployedState = "deployed"

// DefaultUserUseCase sets and clears the operator-set Server Default User (decision 045).
//
// It owns the rule that a value is saved only after a Deployment Key login as that account
// succeeds, so automation can never be pointed at an account it cannot use. With a one-time
// password it first has HostAccess install the Deployment Key for the account; the password is
// passed through once and never kept. Both writes are host-facing changes, so they pass the Server
// Lock guard first. Authorization (admin) is the delivery layer's job.
type DefaultUserUseCase struct {
	servers serverdomain.ServerRepository
	guard   serverdomain.MutationGuard
	hosts   serverdomain.HostAccess
}

// NewDefaultUserUseCase wires the use case to the Server repository, the live Server Lock guard,
// and the SSH host-access port.
func NewDefaultUserUseCase(servers serverdomain.ServerRepository, guard serverdomain.MutationGuard, hosts serverdomain.HostAccess) *DefaultUserUseCase {
	return &DefaultUserUseCase{servers: servers, guard: guard, hosts: hosts}
}

// SetDefaultUserInput is one request to set a Server Default User. Password is optional and used
// verbatim (no trimming) for a single login.
type SetDefaultUserInput struct {
	ServerID string
	User     string
	Password string
}

// SetDefaultUserResult reports the saved Server, whether the Deployment Key was installed with the
// password in this request, and the account's sudo access as the verifying login observed it.
type SetDefaultUserResult struct {
	Server       *serverdomain.Server
	KeyInstalled bool
	Sudo         serverdomain.SudoAccess
}

// Set validates the account name, requires a deployed, unlocked Server with an address, optionally
// installs the Deployment Key with the password, proves a Deployment Key login, and only then saves
// the value. Errors: ErrInvalidDefaultUser; ErrServerNotFound; the Server Lock errors; and a
// *DefaultUserError wrapping ErrDefaultUserNotDeployed, ErrHostUnreachable, ErrHostPasswordRejected,
// ErrDeploymentKeyRejected, or ErrDeploymentKeyMissing. A failure after the key was installed leaves
// the key on the host (installing it is idempotent) and saves nothing.
func (uc *DefaultUserUseCase) Set(ctx context.Context, input SetDefaultUserInput) (*SetDefaultUserResult, error) {
	user := strings.TrimSpace(input.User)
	if !serverdomain.ValidDefaultUser(user) {
		return nil, serverdomain.ErrInvalidDefaultUser
	}
	server, err := uc.servers.FindByID(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	name := server.DisplayName()
	address := server.PrimaryAddress()
	if server.Absent || server.Provisioning == nil || server.Provisioning.State != deployedState || address == "" {
		return nil, &serverdomain.DefaultUserError{Err: serverdomain.ErrDefaultUserNotDeployed, Server: name, User: user}
	}
	if err := uc.guard.RequireUnlocked(ctx, []string{server.ID}); err != nil {
		return nil, err
	}

	target := serverdomain.HostLoginTarget{SiteID: server.Source.SiteID, Address: address, Name: name}
	keyInstalled := false
	if input.Password != "" {
		if err := uc.hosts.AuthorizeDeploymentKey(ctx, target, user, input.Password); err != nil {
			return nil, hostAccessError(err, name, user)
		}
		keyInstalled = true
	}
	sudo, err := uc.hosts.CheckDeploymentKeyLogin(ctx, target, user)
	if err != nil {
		return nil, hostAccessError(err, name, user)
	}
	if err := uc.servers.SetDefaultUser(ctx, server.ID, user); err != nil {
		return nil, fmt.Errorf("save default user of %s: %w", name, err)
	}
	server.DefaultUser = user
	return &SetDefaultUserResult{Server: server, KeyInstalled: keyInstalled, Sudo: sudo}, nil
}

// Clear removes the value set on the Server so the OS Image's default user applies again. It does
// not contact the host: the Deployment Key stays authorized for the previous account. Clearing an
// unset value succeeds. Errors: ErrServerNotFound and the Server Lock errors.
func (uc *DefaultUserUseCase) Clear(ctx context.Context, serverID string) error {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return err
	}
	if err := uc.guard.RequireUnlocked(ctx, []string{server.ID}); err != nil {
		return err
	}
	return uc.servers.SetDefaultUser(ctx, server.ID, "")
}

// hostAccessError turns a HostAccess sentinel into the operator-facing DefaultUserError for this
// Server and account; any other error is wrapped as an unexpected failure.
func hostAccessError(err error, server, user string) error {
	for _, sentinel := range []error{
		serverdomain.ErrHostUnreachable,
		serverdomain.ErrHostPasswordRejected,
		serverdomain.ErrDeploymentKeyRejected,
		serverdomain.ErrDeploymentKeyMissing,
	} {
		if errors.Is(err, sentinel) {
			return &serverdomain.DefaultUserError{Err: sentinel, Server: server, User: user}
		}
	}
	return fmt.Errorf("log in to %s as %s: %w", server, user, err)
}
