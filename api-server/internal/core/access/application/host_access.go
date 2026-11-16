package application

import (
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

// HostAccess is what one User may do on one Server through the User's own account
// (identity-and-access.md §8): log in (Host Access), run sudo, and run sudo without a password.
// Passwordless is meaningful only with Sudo.
type HostAccess struct {
	Login        bool
	Sudo         bool
	Passwordless bool
}

// HostAccessInput is everything HostAccessFor combines for one User on one Server.
type HostAccessInput struct {
	// Admin is whether the User's role is admin.
	Admin bool
	// Membership is the User's membership in the Server's Pool, nil when it has none (or the Server is
	// in no Pool).
	Membership *accessdomain.Membership
	// Policy is the Sudo Policy of the Server's Pool (the zero value when it has none).
	Policy accessdomain.PoolPolicy
	// Limit is the Platform limit on the Server (accessdomain.NoLimit when no Platform limits it).
	Limit accessdomain.HostAccessLimit
	// Settings are the installation's access settings.
	Settings accessdomain.Settings
	// Server is the Server as the Directory names it.
	Server accessdomain.DirectoryServer
}

// HostAccessFor computes one User's access to one Server by the rule the Directory projection
// writes into the Directory (Project), which the directory client then enforces on the host:
//
//   - an admin logs in everywhere ("*") and has sudo everywhere ("ALL"), passwordless as the
//     installation's settings say;
//   - an owner of the Server's Pool logs in when the Server has a name and has sudo when it has a
//     name or an address (the sudo rule lists both), passwordless as the membership's resolved Sudo
//     Policy says;
//   - a member logs in when the Server has a name and the Platform limit lets members log in, and has
//     sudo when the resolved Sudo Policy gives it and the limit lets members have sudo;
//   - anyone else has nothing.
//
// It is the single rule both the Directory projection and the acceptance of a run that executes as
// a User (validation.md §3) read, so a check made before running as a User cannot drift from what
// the host would allow a login. It is a pure function.
func HostAccessFor(in HostAccessInput) HostAccess {
	named := len(in.Server.Names) > 0
	addressed := named || len(in.Server.Addresses) > 0
	if in.Admin {
		return HostAccess{Login: true, Sudo: true, Passwordless: in.Settings.AdminSudoPasswordless}
	}
	membership := in.Membership
	if membership == nil || in.Server.PoolID == "" || membership.PoolID != in.Server.PoolID {
		return HostAccess{}
	}
	owner := membership.Role == accessdomain.RoleOwner
	var access HostAccess
	access.Login = named && (owner || in.Limit.MemberLogin)
	effective := accessdomain.ResolveSudo(*membership, in.Policy)
	if effective.Sudo && addressed && (owner || in.Limit.MemberSudo) {
		access.Sudo, access.Passwordless = true, effective.Passwordless
	}
	return access
}
