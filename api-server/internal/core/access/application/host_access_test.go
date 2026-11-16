package application

import (
	"slices"
	"testing"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

// hostAccessCase is one User on one Server: the inputs both HostAccessFor and the Directory
// projection read, and the access each must give.
type hostAccessCase struct {
	name       string
	admin      bool
	membership *accessdomain.Membership
	policy     accessdomain.PoolPolicy
	limit      *accessdomain.HostAccessLimit
	settings   accessdomain.Settings
	server     accessdomain.DirectoryServer
	want       HostAccess
}

const (
	casePool   = "pool-1"
	caseServer = "s-1"
)

func caseMembership(role accessdomain.PoolRole, sudo, passwordless accessdomain.SudoSetting) *accessdomain.Membership {
	return &accessdomain.Membership{ID: "m-1", PoolID: casePool, UserID: "u-1", Role: role, Sudo: sudo, SudoPasswordless: passwordless}
}

var (
	namedServer   = accessdomain.DirectoryServer{ID: caseServer, PoolID: casePool, Names: []string{"gpu-1", "gpu-1.lab"}, Addresses: []string{"10.0.0.1"}}
	unnamedServer = accessdomain.DirectoryServer{ID: caseServer, PoolID: casePool, Addresses: []string{"10.0.0.1"}}
	otherPool     = accessdomain.DirectoryServer{ID: caseServer, PoolID: "pool-2", Names: []string{"gpu-1"}}
	loginOnly     = accessdomain.HostAccessLimit{MemberLogin: true}
	closed        = accessdomain.HostAccessLimit{}
)

// hostAccessCases is the one table both tests read, so the acceptance check and the Directory
// cannot drift apart unnoticed.
var hostAccessCases = []hostAccessCase{
	{name: "admin", admin: true, server: namedServer, want: HostAccess{Login: true, Sudo: true}},
	{name: "admin, passwordless setting", admin: true, settings: accessdomain.Settings{AdminSudoPasswordless: true}, server: unnamedServer,
		want: HostAccess{Login: true, Sudo: true, Passwordless: true}},
	{name: "owner", membership: caseMembership(accessdomain.RoleOwner, accessdomain.SudoInherit, accessdomain.SudoInherit), server: namedServer,
		want: HostAccess{Login: true, Sudo: true}},
	{name: "owner, passwordless Pool", membership: caseMembership(accessdomain.RoleOwner, accessdomain.SudoInherit, accessdomain.SudoInherit),
		policy: accessdomain.PoolPolicy{PoolID: casePool, SudoPasswordless: true}, server: namedServer, want: HostAccess{Login: true, Sudo: true, Passwordless: true}},
	{name: "owner, a Platform closes it to members", membership: caseMembership(accessdomain.RoleOwner, accessdomain.SudoInherit, accessdomain.SudoInherit),
		limit: &closed, server: namedServer, want: HostAccess{Login: true, Sudo: true}},
	{name: "owner, Server without a name", membership: caseMembership(accessdomain.RoleOwner, accessdomain.SudoInherit, accessdomain.SudoInherit),
		server: unnamedServer, want: HostAccess{Sudo: true}},
	{name: "member, default policy", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoInherit, accessdomain.SudoInherit),
		server: namedServer, want: HostAccess{Login: true}},
	{name: "member, Pool gives member sudo", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoInherit, accessdomain.SudoInherit),
		policy: accessdomain.PoolPolicy{PoolID: casePool, MemberSudo: true}, server: namedServer, want: HostAccess{Login: true, Sudo: true}},
	{name: "member, override on and passwordless", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoOn, accessdomain.SudoOn),
		server: namedServer, want: HostAccess{Login: true, Sudo: true, Passwordless: true}},
	{name: "member, override off over the Pool", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoOff, accessdomain.SudoInherit),
		policy: accessdomain.PoolPolicy{PoolID: casePool, MemberSudo: true}, server: namedServer, want: HostAccess{Login: true}},
	{name: "member, login node (no member sudo)", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoOn, accessdomain.SudoInherit),
		limit: &loginOnly, server: namedServer, want: HostAccess{Login: true}},
	{name: "member, node closed to members", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoOn, accessdomain.SudoInherit),
		limit: &closed, server: namedServer, want: HostAccess{}},
	{name: "member, Server without a name", membership: caseMembership(accessdomain.RoleMember, accessdomain.SudoOn, accessdomain.SudoInherit),
		server: unnamedServer, want: HostAccess{Sudo: true}},
	{name: "no membership", server: namedServer, want: HostAccess{}},
	{name: "membership of another Pool", membership: caseMembership(accessdomain.RoleOwner, accessdomain.SudoInherit, accessdomain.SudoInherit),
		server: otherPool, want: HostAccess{}},
}

func (tc hostAccessCase) input() HostAccessInput {
	limit := accessdomain.NoLimit
	if tc.limit != nil {
		limit = *tc.limit
	}
	return HostAccessInput{Admin: tc.admin, Membership: tc.membership, Policy: tc.policy, Limit: limit, Settings: tc.settings, Server: tc.server}
}

func TestHostAccessFor(t *testing.T) {
	for _, tc := range hostAccessCases {
		if got := HostAccessFor(tc.input()); got != tc.want {
			t.Errorf("%s: HostAccessFor() = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// The Directory the projection writes gives each case's User exactly the access HostAccessFor
// computes: a login where its account's host list names the Server (or is "*"), and sudo, with or
// without a password, where one of its sudo rules names the Server's name or address (or ALL).
func TestProjectGivesTheAccessHostAccessForComputes(t *testing.T) {
	for _, tc := range hostAccessCases {
		role := authdomain.RoleUser
		if tc.admin {
			role = authdomain.RoleAdmin
		}
		user := &authdomain.User{ID: "u-1", Username: "alice", Role: role, UIDNumber: 10005}
		in := ProjectionInput{
			Users:    []*authdomain.User{user},
			Policies: map[string]accessdomain.PoolPolicy{casePool: tc.policy},
			Settings: tc.settings,
			Servers:  []accessdomain.DirectoryServer{tc.server},
			Pools:    []accessdomain.DirectoryPool{{ID: casePool, Name: "lab"}, {ID: "pool-2", Name: "other"}},
			Limits:   map[string]accessdomain.HostAccessLimit{},
			Groups: map[string]accessdomain.GroupAllocation{
				groupKeyUsers: {Name: GroupUsers, GIDNumber: 10000}, groupKeyAdmins: {Name: GroupAdmins, GIDNumber: 10001},
				groupKeyPoolPrefix + casePool: {Name: "pool-lab", GIDNumber: 10002}, groupKeyOwnerPrefix + casePool: {Name: "pool-lab-owners", GIDNumber: 10003},
				groupKeyPoolPrefix + "pool-2": {Name: "pool-other", GIDNumber: 10004}, groupKeyOwnerPrefix + "pool-2": {Name: "pool-other-owners", GIDNumber: 10005},
			},
		}
		if tc.membership != nil {
			in.Memberships = []*accessdomain.Membership{tc.membership}
		}
		if tc.limit != nil {
			in.Limits[caseServer] = *tc.limit
		}
		content := Project(in)
		var got HostAccess
		hosts := append(slices.Clone(tc.server.Names), tc.server.Addresses...)
		for _, account := range content.Users {
			if account.Username == user.Username {
				got.Login = slices.Contains(account.Hosts, accessdomain.AnyHost) ||
					slices.ContainsFunc(tc.server.Names, func(name string) bool { return slices.Contains(account.Hosts, name) })
			}
		}
		for _, rule := range content.SudoRules {
			if !slices.Contains(rule.Users, user.Username) {
				continue
			}
			if slices.Contains(rule.Hosts, accessdomain.AllHosts) || slices.ContainsFunc(hosts, func(host string) bool { return slices.Contains(rule.Hosts, host) }) {
				got.Sudo, got.Passwordless = true, rule.Passwordless
			}
		}
		if got != tc.want {
			t.Errorf("%s: the Directory gives %+v, want %+v (HostAccessFor's)", tc.name, got, tc.want)
		}
	}
}
