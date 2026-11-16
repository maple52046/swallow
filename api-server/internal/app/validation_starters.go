package app

import (
	"context"
	"errors"
	"fmt"

	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// validationStarters implements validationapp.StarterAccess over the access, Server, Platform, and
// software stores: what a User may do on target Servers through the User's own account, for accepting
// a user suite run as that User (validation.md §3, identity-and-access.md §8.3). Host Access and sudo
// come from accessapp.HostAccessFor with the same inputs the Directory projection reads, so the
// acceptance check and the Directory cannot disagree on the rule; they may disagree for about a
// minute on the data while the hosts' caches refresh, which the playbook's own checks cover.
type validationStarters struct {
	// directoryConfigured is whether the installation has a Directory, without which no host has a
	// User's account.
	directoryConfigured bool
	users               authdomain.UserRepository
	memberships         accessdomain.MembershipRepository
	policies            accessdomain.PolicyRepository
	settings            accessdomain.SettingsRepository
	servers             serverdomain.ServerRepository
	// limits reads the Platform modules' Host Access limits, as the Directory controller does.
	limits interface {
		HostAccessLimits(ctx context.Context) (map[string]accessdomain.HostAccessLimit, error)
	}
	assignments softwaredomain.AssignmentRepository
	// directoryClient is the Managed Software kind the catalog marks as the directory client; empty
	// when the build has none, in which case no Server has one.
	directoryClient softwaredomain.Kind
}

var _ validationapp.StarterAccess = validationStarters{}

// directoryClientKind returns the catalog's directory client kind, or "" when the build has none.
func directoryClientKind(catalog *softwaredomain.Catalog) softwaredomain.Kind {
	for _, entry := range catalog.Entries() {
		if entry.DirectoryClient {
			return entry.Kind
		}
	}
	return ""
}

// StarterAccount implements validationapp.StarterAccess. A deleted User is ErrRunForbidden; a Server
// that no longer exists has no access (its entry is the zero StarterHost).
func (s validationStarters) StarterAccount(ctx context.Context, userID string, serverIDs []string) (validationapp.StarterAccount, error) {
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return validationapp.StarterAccount{}, fmt.Errorf("%w: the caller's User no longer exists", validationdomain.ErrRunForbidden)
	}
	if err != nil {
		return validationapp.StarterAccount{}, err
	}
	account := validationapp.StarterAccount{
		DirectoryConfigured: s.directoryConfigured, Username: user.Username, UIDNumber: user.UIDNumber,
		Hosts: make(map[string]validationapp.StarterHost, len(serverIDs)),
	}
	if !s.directoryConfigured || user.UIDNumber <= 0 {
		return account, nil
	}
	memberships, err := s.memberships.ListByUser(ctx, userID)
	if err != nil {
		return validationapp.StarterAccount{}, err
	}
	byPool := make(map[string]*accessdomain.Membership, len(memberships))
	for _, membership := range memberships {
		byPool[membership.PoolID] = membership
	}
	settings, err := s.settings.Find(ctx)
	if err != nil {
		return validationapp.StarterAccount{}, err
	}
	limits, err := s.limits.HostAccessLimits(ctx)
	if err != nil {
		return validationapp.StarterAccount{}, err
	}
	for _, id := range serverIDs {
		server, err := s.servers.FindByID(ctx, id)
		if errors.Is(err, serverdomain.ErrServerNotFound) {
			account.Hosts[id] = validationapp.StarterHost{}
			continue
		}
		if err != nil {
			return validationapp.StarterAccount{}, err
		}
		in := accessapp.HostAccessInput{
			Admin: user.Role == authdomain.RoleAdmin, Settings: settings, Server: directoryServerOf(server), Limit: accessdomain.NoLimit,
		}
		if limit, ok := limits[id]; ok {
			in.Limit = limit
		}
		if membership, ok := byPool[server.Spec.PoolID]; ok && server.Spec.PoolID != "" {
			policy, err := s.policies.Find(ctx, server.Spec.PoolID)
			if err != nil {
				return validationapp.StarterAccount{}, err
			}
			in.Membership, in.Policy = membership, policy
		}
		access := accessapp.HostAccessFor(in)
		client, err := s.hasDirectoryClient(ctx, id)
		if err != nil {
			return validationapp.StarterAccount{}, err
		}
		account.Hosts[id] = validationapp.StarterHost{
			Login: access.Login, Sudo: access.Sudo, Passwordless: access.Passwordless, DirectoryClient: client,
		}
	}
	return account, nil
}

// hasDirectoryClient reports whether the Server's directory client assignment is installed.
func (s validationStarters) hasDirectoryClient(ctx context.Context, serverID string) (bool, error) {
	if s.directoryClient == "" {
		return false, nil
	}
	assignment, err := s.assignments.FindByServerAndKind(ctx, serverID, s.directoryClient)
	if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return assignment.State == softwaredomain.StateInstalled, nil
}
