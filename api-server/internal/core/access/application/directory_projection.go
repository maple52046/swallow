package application

import (
	"fmt"
	"regexp"
	"strings"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

// Group allocation keys (accessdomain.GroupAllocation.Key) and the names of the installation-wide
// groups (identity-and-access.md §3.2).
const (
	groupKeyUsers       = "users"
	groupKeyAdmins      = "admins"
	groupKeyPoolPrefix  = "pool:"
	groupKeyOwnerPrefix = "pool-owners:"

	// GroupUsers is every User's primary group. A shared primary group, instead of one per User,
	// keeps a User named "admin" from producing a group that Ubuntu's default sudoers grants sudo.
	GroupUsers = "swallow-users"
	// GroupAdmins holds every admin.
	GroupAdmins = "swallow-admins"

	// maxPoolSlug bounds the Pool part of a Pool's group name so "pool-<slug>-owners" stays within
	// the 32 characters groupadd and most tools accept.
	maxPoolSlug = 20

	userHome  = "/home/"
	userShell = "/bin/bash"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// poolGroupName derives a Pool's group name from its name at allocation time: "pool-" and the name
// lowercased, every run of other characters turned into "-", trimmed to maxPoolSlug. taken holds the
// names already allocated; a collision gets "-2", "-3", ….
func poolGroupName(poolName string, taken map[string]bool) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(poolName), "-"), "-")
	if slug == "" {
		slug = "pool"
	}
	if len(slug) > maxPoolSlug {
		slug = strings.TrimRight(slug[:maxPoolSlug], "-")
	}
	name := "pool-" + slug
	for n := 2; taken[name]; n++ {
		suffix := fmt.Sprintf("-%d", n)
		base := slug
		if len(base)+len(suffix) > maxPoolSlug {
			base = strings.TrimRight(base[:maxPoolSlug-len(suffix)], "-")
		}
		name = "pool-" + base + suffix
	}
	return name
}

// ProjectionInput is everything the Directory projection combines, read from the store in one pass.
// Groups must already hold an allocation for groupKeyUsers, groupKeyAdmins, and both keys of every
// Pool (the controller allocates the missing ones first).
type ProjectionInput struct {
	Users       []*authdomain.User
	Memberships []*accessdomain.Membership
	Policies    map[string]accessdomain.PoolPolicy
	Settings    accessdomain.Settings
	AccessKeys  map[string][]string
	Servers     []accessdomain.DirectoryServer
	Pools       []accessdomain.DirectoryPool
	Limits      map[string]accessdomain.HostAccessLimit
	Groups      map[string]accessdomain.GroupAllocation
}

// Project computes the Directory's complete content (identity-and-access.md §7.2, §8): one account
// per User that has a uidNumber, the installation-wide and per-Pool groups, and the sudo rules.
//
//   - Host Access: an admin may log in on every host ("*"); an owner on every Server of its Pools; a
//     member on its Pools' Servers that no Platform limit closes to members. A Server without a name
//     admits no Directory account by name.
//   - sudo: one rule for the admins on every host; per Pool, owners and the members whose resolved
//     Sudo Policy gives sudo, split by passwordless, each on the Pool's Servers (for members only
//     those whose Platform limit allows member sudo). Admins appear only in their own rule, and a
//     Server belongs to one Pool, so each User falls in one rule per host.
//
// It is a pure function of its input; the result is normalized.
func Project(in ProjectionInput) accessdomain.DirectoryContent {
	primary := in.Groups[groupKeyUsers]
	usersByID := make(map[string]*authdomain.User, len(in.Users))
	var content accessdomain.DirectoryContent
	var everyone, admins []string
	for _, user := range in.Users {
		if user.UIDNumber <= 0 {
			continue
		}
		usersByID[user.ID] = user
		everyone = append(everyone, user.Username)
		if user.Role == authdomain.RoleAdmin {
			admins = append(admins, user.Username)
		}
	}

	serversByPool := map[string][]accessdomain.DirectoryServer{}
	for _, server := range in.Servers {
		if server.PoolID != "" {
			serversByPool[server.PoolID] = append(serversByPool[server.PoolID], server)
		}
	}
	limit := func(serverID string) accessdomain.HostAccessLimit {
		if l, ok := in.Limits[serverID]; ok {
			return l
		}
		return accessdomain.NoLimit
	}

	hosts := map[string][]string{}
	for _, user := range in.Users {
		if user.Role == authdomain.RoleAdmin {
			hosts[user.ID] = []string{accessdomain.AnyHost}
		}
	}
	poolOwners := map[string][]string{}
	poolEveryone := map[string][]string{}
	type sudoKey struct {
		poolID       string
		owner        bool
		passwordless bool
	}
	sudoUsers := map[sudoKey][]string{}
	for _, membership := range in.Memberships {
		user, ok := usersByID[membership.UserID]
		if !ok {
			continue
		}
		poolEveryone[membership.PoolID] = append(poolEveryone[membership.PoolID], user.Username)
		owner := membership.Role == accessdomain.RoleOwner
		if owner {
			poolOwners[membership.PoolID] = append(poolOwners[membership.PoolID], user.Username)
		}
		if user.Role == authdomain.RoleAdmin {
			continue
		}
		for _, server := range serversByPool[membership.PoolID] {
			if owner || limit(server.ID).MemberLogin {
				hosts[user.ID] = append(hosts[user.ID], server.Names...)
			}
		}
		effective := accessdomain.ResolveSudo(*membership, in.Policies[membership.PoolID])
		if effective.Sudo {
			key := sudoKey{poolID: membership.PoolID, owner: owner, passwordless: effective.Passwordless}
			sudoUsers[key] = append(sudoUsers[key], user.Username)
		}
	}

	for _, user := range in.Users {
		if user.UIDNumber <= 0 {
			continue
		}
		content.Users = append(content.Users, accessdomain.DirectoryUser{
			Username: user.Username, UIDNumber: user.UIDNumber, GIDNumber: primary.GIDNumber,
			Home: userHome + user.Username, Shell: userShell, PasswordHash: string(user.PasswordHash),
			SSHPublicKeys: in.AccessKeys[user.ID], Hosts: hosts[user.ID],
		})
	}

	content.Groups = append(content.Groups,
		accessdomain.DirectoryGroup{Name: primary.Name, GIDNumber: primary.GIDNumber, Members: everyone},
		accessdomain.DirectoryGroup{Name: in.Groups[groupKeyAdmins].Name, GIDNumber: in.Groups[groupKeyAdmins].GIDNumber, Members: admins},
	)
	if len(admins) > 0 {
		content.SudoRules = append(content.SudoRules, accessdomain.SudoRule{
			Name: GroupAdmins, Users: admins, Hosts: []string{accessdomain.AllHosts}, Passwordless: in.Settings.AdminSudoPasswordless,
		})
	}
	for _, pool := range in.Pools {
		group, ok := in.Groups[groupKeyPoolPrefix+pool.ID]
		if !ok {
			continue
		}
		owners := in.Groups[groupKeyOwnerPrefix+pool.ID]
		content.Groups = append(content.Groups,
			accessdomain.DirectoryGroup{Name: group.Name, GIDNumber: group.GIDNumber, Members: poolEveryone[pool.ID]},
			accessdomain.DirectoryGroup{Name: owners.Name, GIDNumber: owners.GIDNumber, Members: poolOwners[pool.ID]},
		)
		var ownerHosts, memberHosts []string
		for _, server := range serversByPool[pool.ID] {
			names := append(append([]string(nil), server.Names...), server.Addresses...)
			ownerHosts = append(ownerHosts, names...)
			if limit(server.ID).MemberSudo {
				memberHosts = append(memberHosts, names...)
			}
		}
		for _, owner := range []bool{true, false} {
			for _, passwordless := range []bool{false, true} {
				users := sudoUsers[sudoKey{poolID: pool.ID, owner: owner, passwordless: passwordless}]
				ruleHosts := memberHosts
				if owner {
					ruleHosts = ownerHosts
				}
				if len(users) == 0 || len(ruleHosts) == 0 {
					continue
				}
				content.SudoRules = append(content.SudoRules, accessdomain.SudoRule{
					Name: sudoRuleName(group.Name, owner, passwordless), Users: users, Hosts: ruleHosts, Passwordless: passwordless,
				})
			}
		}
	}
	content.Normalize()
	return content
}

// sudoRuleName names a Pool's sudo rule: "<pool group>-<owner|member>-<passwd|nopasswd>".
func sudoRuleName(poolGroup string, owner, passwordless bool) string {
	role, auth := "member", "passwd"
	if owner {
		role = "owner"
	}
	if passwordless {
		auth = "nopasswd"
	}
	return poolGroup + "-" + role + "-" + auth
}
