package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// DirectoryUser is one account as the Directory holds it: the User's login name and POSIX
// identity, the password hash for password-protected sudo, its Access Keys as authorized_keys
// lines, and the host names its Host Access allows ("*" for every host, which only admins get).
type DirectoryUser struct {
	Username      string
	UIDNumber     int
	GIDNumber     int
	Home          string
	Shell         string
	PasswordHash  string
	SSHPublicKeys []string
	Hosts         []string
}

// DirectoryGroup is one POSIX group as the Directory holds it, with its members' login names.
type DirectoryGroup struct {
	Name      string
	GIDNumber int
	Members   []string
}

// SudoRule is one sudo rule as the Directory holds it: its Users may run any command as any user
// on its Hosts (host names and addresses, or AllHosts), without a password when Passwordless.
type SudoRule struct {
	Name         string
	Users        []string
	Hosts        []string
	Passwordless bool
}

// AllHosts is the Hosts value of a sudo rule that applies on every host.
const AllHosts = "ALL"

// AnyHost is the Hosts value of an account that may log in on every host.
const AnyHost = "*"

// DirectoryContent is the complete desired content of the Directory, computed from swallow's store
// on every pass (identity-and-access.md §7). The Directory controller hands all of it to
// Directory.Apply, which makes the Directory hold exactly this.
type DirectoryContent struct {
	Users     []DirectoryUser
	Groups    []DirectoryGroup
	SudoRules []SudoRule
}

// Normalize sorts every list in c, so equal contents compare and hash equal regardless of the order
// they were built in.
func (c *DirectoryContent) Normalize() {
	for i := range c.Users {
		slices.Sort(c.Users[i].SSHPublicKeys)
		c.Users[i].Hosts = sortedUnique(c.Users[i].Hosts)
	}
	slices.SortFunc(c.Users, func(a, b DirectoryUser) int { return compareStrings(a.Username, b.Username) })
	for i := range c.Groups {
		c.Groups[i].Members = sortedUnique(c.Groups[i].Members)
	}
	slices.SortFunc(c.Groups, func(a, b DirectoryGroup) int { return compareStrings(a.Name, b.Name) })
	for i := range c.SudoRules {
		c.SudoRules[i].Users = sortedUnique(c.SudoRules[i].Users)
		c.SudoRules[i].Hosts = sortedUnique(c.SudoRules[i].Hosts)
	}
	slices.SortFunc(c.SudoRules, func(a, b SudoRule) int { return compareStrings(a.Name, b.Name) })
}

// Hash is a stable digest of the normalized content. The controller skips a pass whose content
// hashes like the last applied one (a change feed event that changed nothing the Directory holds);
// it never stands in for comparing with the Directory itself at resync.
func (c DirectoryContent) Hash() string {
	c.Normalize()
	data, _ := json.Marshal(c) // A struct of strings, ints, bools, and slices always marshals.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HostCount is the number of distinct host names and addresses the content's Host Access and sudo
// rules name, for the Directory status.
func (c DirectoryContent) HostCount() int {
	hosts := map[string]bool{}
	for _, user := range c.Users {
		for _, host := range user.Hosts {
			if host != AnyHost {
				hosts[host] = true
			}
		}
	}
	return len(hosts)
}

// ApplyResult counts what one Apply changed in the Directory.
type ApplyResult struct {
	Added    int
	Modified int
	Deleted  int
}

// Directory is the account directory the Servers read their users from (glossary Directory; a
// Provider Interface). Apply makes the Directory hold exactly content: it adds what is missing,
// rewrites what differs, and deletes every account, group, and sudo rule swallow manages that
// content does not list. It must be idempotent, and safe to call while the Servers read.
//
// Errors: one wrapping ErrDirectoryUnreachable means the Directory could not be reached or did not
// answer (the controller retries with backoff); one wrapping ErrDirectoryRejected means the
// Directory refused a change (the controller stops and asks for attention until the content
// changes or a resync). The message must be safe to show a user: no credential, no password hash.
type Directory interface {
	Apply(ctx context.Context, content DirectoryContent) (ApplyResult, error)
}

var (
	// ErrDirectoryUnreachable classifies a transient Directory failure.
	ErrDirectoryUnreachable = errors.New("directory unreachable")
	// ErrDirectoryRejected classifies a Directory that refused a change.
	ErrDirectoryRejected = errors.New("directory rejected the change")
)

// DirectoryStatus is the Directory controller's status (identity-and-access.md §7.3): its
// conditions, the hash of the content it last applied, when it last applied and last checked the
// Directory, and the counts of what the content holds. Only the controller writes it.
type DirectoryStatus struct {
	Conditions    []convergence.Condition
	ContentHash   string
	LastAppliedAt *time.Time
	ObservedAt    *time.Time
	Users         int
	Groups        int
	SudoRules     int
	Hosts         int
}

// DirectoryStatusRepository persists the Directory status; Find before the first pass returns the
// zero status.
type DirectoryStatusRepository interface {
	Find(ctx context.Context) (DirectoryStatus, error)
	Save(ctx context.Context, status DirectoryStatus) error
}

// GroupAllocation is one POSIX group swallow allocated: the purpose it was allocated for (its key,
// for example "pool:<pool id>"), the name it got, and its gidNumber. Name and number never change
// after allocation, even when the Pool is renamed.
type GroupAllocation struct {
	Key       string
	Name      string
	GIDNumber int
}

// GroupAllocator allocates POSIX groups. Groups returns every allocation by key. Allocate records a
// new group for key with name and the next unused gidNumber; when key already has one it returns
// that one unchanged. Callers choose names that no other allocation uses.
type GroupAllocator interface {
	Groups(ctx context.Context) (map[string]GroupAllocation, error)
	Allocate(ctx context.Context, key, name string) (GroupAllocation, error)
}

// HostAccessLimit is a Platform's limit on a Pool's members on one of its nodes (glossary Host
// Access): whether members may log in, and whether they may have sudo. Owners and admins are never
// limited.
type HostAccessLimit struct {
	MemberLogin bool
	MemberSudo  bool
}

// NoLimit is the limit of a Server no Platform limits: members follow the Pool's rules.
var NoLimit = HostAccessLimit{MemberLogin: true, MemberSudo: true}

func sortedUnique(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
