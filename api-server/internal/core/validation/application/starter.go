package application

import (
	"context"
	"fmt"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// Actor is the User a use case acts for, as the delivery layer resolved it for this request from
// the caller's current User Role and Pool Memberships. The route guards have already checked what
// every route of the use case needs (Pool read on a run's targets); the use cases decide the finer
// rules that depend on the suite or the run (validation.md §3, §4.4; identity-and-access.md §4.2).
type Actor struct {
	UserID   string
	Username string
	Admin    bool
	// Targets answers whether the actor may operate targets. It is required for starting and
	// canceling runs, and unused by the suite writes.
	Targets TargetAuthorizer
}

// TargetAuthorizer reports whether one caller may operate targets: Pool operate (an owner of their
// Pool, or an admin) on every Server in serverIDs and, when platformID is set, on that Platform. A
// target the caller may not reach is false, not an error; an error is a store failure.
type TargetAuthorizer interface {
	OperatesTargets(ctx context.Context, serverIDs []string, platformID string) (bool, error)
}

// StarterHost is what one User may do on one target Server through the User's own account: Host
// Access (Login), sudo and whether it needs a password, computed by the same rule the Directory
// projection uses, and whether the Server has the directory client installed, without which the
// host has no account for the User.
type StarterHost struct {
	Login           bool
	Sudo            bool
	Passwordless    bool
	DirectoryClient bool
}

// StarterAccount is the account a user suite run would use: whether the installation has a
// Directory, the User's username and uidNumber (0 when it has none, so it is not in the Directory),
// and the User's StarterHost on each Server asked about, by Server id.
type StarterAccount struct {
	DirectoryConfigured bool
	Username            string
	UIDNumber           int
	Hosts               map[string]StarterHost
}

// StarterAccess reads a User's account and access on target Servers, for accepting a user suite run
// as that User (implemented in the composition root over the access and software stores). Every
// Server in serverIDs has an entry in the result. A User that no longer exists is
// ErrRunForbidden; other errors are store failures.
type StarterAccess interface {
	StarterAccount(ctx context.Context, userID string, serverIDs []string) (StarterAccount, error)
}

// checkStarterLevel refuses a start below the suite's starter level: StarterOwner needs Pool operate
// on every target, StarterMember only the Pool read the route guard already checked.
func checkStarterLevel(ctx context.Context, suite validationdomain.Suite, starter Actor, serverIDs []string, platformID string) error {
	if starter.Admin || suite.RequiredLevel() == validationdomain.StarterMember {
		return nil
	}
	if starter.Targets == nil {
		return fmt.Errorf("%w: the caller's access to the targets is unknown", validationdomain.ErrRunForbidden)
	}
	operates, err := starter.Targets.OperatesTargets(ctx, serverIDs, platformID)
	if err != nil {
		return err
	}
	if !operates {
		return fmt.Errorf("%w: suite %s is started only by an owner of every target's Pool", validationdomain.ErrRunForbidden, suite.ID)
	}
	return nil
}

// starterRunAccount checks that the starter's own account can run a user suite on every target
// Server (validation.md §3 step 5) and returns the account the run records. The order follows the
// contract: no Directory or no uidNumber is a target not runnable (409); then, per target, no Host
// Access (403), no sudo for a sudo suite (403), sudo that needs a password (409), and no directory
// client (409). names maps Server ids onto names for the messages.
func starterRunAccount(account StarterAccount, suite validationdomain.Suite, serverIDs []string, names map[string]string) (validationdomain.RunAccount, error) {
	if !account.DirectoryConfigured {
		return validationdomain.RunAccount{}, fmt.Errorf("%w: a user suite runs as the account of the User who starts it, and this installation has no Directory to give the targets that account",
			validationdomain.ErrTargetNotRunnable)
	}
	if account.UIDNumber <= 0 {
		return validationdomain.RunAccount{}, fmt.Errorf("%w: %s has no uidNumber, so no target has an account for them",
			validationdomain.ErrTargetNotRunnable, account.Username)
	}
	for _, id := range serverIDs {
		host, name := account.Hosts[id], names[id]
		if name == "" {
			name = id
		}
		switch {
		case !host.Login:
			return validationdomain.RunAccount{}, fmt.Errorf("%w: %s may not log in to Server %s, so a user suite cannot run there as them",
				validationdomain.ErrRunForbidden, account.Username, name)
		case suite.Sudo && !host.Sudo:
			return validationdomain.RunAccount{}, fmt.Errorf("%w: suite %s runs through sudo, and %s has no sudo on Server %s",
				validationdomain.ErrRunForbidden, suite.ID, account.Username, name)
		case suite.Sudo && !host.Passwordless:
			return validationdomain.RunAccount{}, fmt.Errorf("%w: suite %s runs through sudo, and %s's sudo on Server %s needs a password, which swallow does not have",
				validationdomain.ErrTargetNotRunnable, suite.ID, account.Username, name)
		case !host.DirectoryClient:
			return validationdomain.RunAccount{}, fmt.Errorf("%w: Server %s has no directory client installed, so it has no account for %s",
				validationdomain.ErrTargetNotRunnable, name, account.Username)
		}
	}
	return validationdomain.RunAccount{Username: account.Username, UIDNumber: account.UIDNumber, Sudo: suite.Sudo}, nil
}
